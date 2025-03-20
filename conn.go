package websocket

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"unicode/utf8"
)

type Conn struct {
	netConn  net.Conn
	isServer bool

	br *bufio.Reader
	bw *bufio.Writer

	subprotocol string
}

var (
	ErrInvalidMessageType = errors.New("websocket: Specified message must be TextMessage or BinaryMessage")
	ErrBadMessage         = errors.New("websocket: Received a message that violates the websocket protocol")
	ErrUtf8               = errors.New("websocket: Received a TextMessage that contains invalid utf-8")
	ErrNormalClose        = errors.New("websocket: Peer disconnected normally")
	ErrUnexpectedClose    = errors.New("websocket: Peer disconnected unexpectedly")
)

func (c *Conn) read(n uint64) ([]byte, error) {
	if n == 0 {
		return make([]byte, 0), nil
	}

	buf := make([]byte, n)
	if _, err := io.ReadFull(c.br, buf); err != nil {
		return buf, err
	}

	return buf, nil
}

func (c *Conn) discardRemaining(n int64) (int64, error) {
	nn, err := io.CopyN(io.Discard, c.br, n)
	return nn, err
}

func isEOF(err error) bool {
	return err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF)
}

func (c *Conn) handleTextMessage(h *Headers) ([]byte, error) {
	payload, err := c.read(h.PayloadLength)
	if err != nil {
		return payload, err
	}
	// toggle mask if we're a server
	if c.isServer {
		toggleMask(payload, h.MaskingKey)
	}
	// check if valid utf-8 payload
	if !utf8.Valid(payload) {
		return payload, ErrUtf8
	}
	return payload, nil
}

func (c *Conn) handleBinaryMessage(h *Headers) ([]byte, error) {
	payload, err := c.read(h.PayloadLength)
	if err != nil {
		return payload, err
	}
	// toggle mask if we're a server
	if c.isServer {
		toggleMask(payload, h.MaskingKey)
	}
	return payload, nil
}

func (c *Conn) handleCloseFrame(h *Headers) ([]byte, error) {
	// If no payload then it's a Close with no status or reason
	if h.PayloadLength == 0 {
		_, _ = c.sendControl(CloseFrame, CloseNormal, nil)
		return nil, ErrNormalClose
	}
	// payload length must be atleast 2 and not bigger than 125 (status code)
	if h.PayloadLength < 2 || h.PayloadLength > maxControlFramePayloadSize {
		return nil, ErrBadMessage
	}

	// read status code
	payload, err := c.read(h.PayloadLength)
	if err != nil {
		return payload, err
	}
	// toggle mask if we're a server
	if c.isServer {
		toggleMask(payload, h.MaskingKey)
	}

	// parse status code
	statusCode := binary.BigEndian.Uint16(payload[0:2])
	// check for valid status codes
	if !validCloseFrameCodes[statusCode] &&
		(statusCode < minNonCloseStatusCode || statusCode > maxNonCloseStatusCode) {
		return payload, ErrBadMessage
	}

	// handle extra reason payload
	if len(payload) <= 2 {
		payload = make([]byte, 0)
	} else {
		payload = payload[2:]
	}
	// Verify valid utf-8
	if len(payload) > 0 && !utf8.Valid(payload) {
		return payload, ErrBadMessage
	}

	// we don't care if sending the control fails here
	_, _ = c.sendControl(CloseFrame, statusCode, payload)
	return payload, ErrNormalClose
}

func (c *Conn) handlePingFrame(h *Headers) ([]byte, error) {
	// payload length must not be bigger than 125
	if h.PayloadLength > maxControlFramePayloadSize {
		return nil, ErrBadMessage
	}
	// ping shouldn't be fragmented
	if !h.FIN {
		return nil, ErrBadMessage
	}

	payload, err := c.read(h.PayloadLength)
	if err != nil {
		return payload, err
	}
	// toggle mask
	if c.isServer {
		toggleMask(payload, h.MaskingKey)
	}

	_, err = c.sendControl(PongFrame, 0, payload)
	if err != nil {
		return payload, err
	}

	return payload, nil
}

func (c *Conn) handlePongFrame(h *Headers) ([]byte, error) {
	// payload length must not be bigger than 125
	if h.PayloadLength > maxControlFramePayloadSize {
		return nil, ErrBadMessage
	}
	// pong shouldn't be fragmented
	if !h.FIN {
		return nil, ErrBadMessage
	}

	payload, err := c.read(h.PayloadLength)
	if err != nil {
		return payload, err
	}

	return payload, nil
}

func (c *Conn) handleSingleFrame(h *Headers) ([]byte, error) {
	switch h.Opcode {
	case TextMessage:
		return c.handleTextMessage(h)
	case BinaryMessage:
		return c.handleBinaryMessage(h)
	case ContinuationFrame:
		// Same as binary message handling
		return c.handleBinaryMessage(h)
	case CloseFrame:
		return c.handleCloseFrame(h)
	case PingFrame:
		return c.handlePingFrame(h)
	case PongFrame:
		return c.handlePongFrame(h)
	default:
		// Unhandled Opcode
		return nil, ErrBadMessage
	}
}

func (c *Conn) handleSingleMessageErr(err error) (Opcode, []byte, error) {
	switch {
	case isEOF(err):
		return CloseFrame, nil, ErrUnexpectedClose
	case errors.Is(err, ErrUtf8):
		return c.closeWithErr(CloseMistachedPayloadData)
	case errors.Is(err, ErrBadMessage):
		return c.closeWithErr(CloseProtocolError)
	default:
		return CloseFrame, nil, err
	}
}

func (c *Conn) NextMessage() (Opcode, []byte, error) {
	// loop and ignore control message (eg. PING PONG)
	for {
		initialHeaders, err := c.parseFrameHeaders()
		if isEOF(err) {
			return CloseFrame, nil, ErrUnexpectedClose
		}

		// Check reserved bits
		if initialHeaders.RSV1 || initialHeaders.RSV2 || initialHeaders.RSV3 {
			return c.closeWithErr(CloseProtocolError)
		}

		// Client messages must be masked
		if initialHeaders.Mask != c.isServer {
			return c.closeWithErr(CloseProtocolError)
		}

		// initial message payload
		initialPayload, err := c.handleSingleFrame(initialHeaders)
		if err != nil {
			return c.handleSingleMessageErr(err)
		}

		// skip this frame if control frame
		if initialHeaders.Opcode == PingFrame || initialHeaders.Opcode == PongFrame {
			continue
		}

		if initialHeaders.Opcode == ContinuationFrame {
			return c.closeWithErr(CloseProtocolError)
		}
		// Single frame
		if initialHeaders.FIN {
			return initialHeaders.Opcode, initialPayload, nil
		}

		// Fragmented frames
		for {
			nextHeaders, err := c.parseFrameHeaders()
			if isEOF(err) {
				return CloseFrame, nil, ErrUnexpectedClose
			}

			// illegal ContinuationFrame
			if nextHeaders.Opcode != ContinuationFrame && nextHeaders.Opcode != CloseFrame && nextHeaders.Opcode != PingFrame && nextHeaders.Opcode != PongFrame {
				return c.closeWithErr(CloseProtocolError)
			}

			nextPayload, err := c.handleSingleFrame(nextHeaders)
			if err != nil {
				return c.handleSingleMessageErr(err)
			}

			// skip this frame if control frame
			if nextHeaders.Opcode == PingFrame || nextHeaders.Opcode == PongFrame {
				continue
			}

			if initialHeaders.Opcode == TextMessage && !utf8.Valid(nextPayload) {
				return c.closeWithErr(CloseMistachedPayloadData)
			}

			// append data
			initialPayload = append(initialPayload, nextPayload...)

			if nextHeaders.FIN {
				break
			}
		}

		return initialHeaders.Opcode, initialPayload, nil
	}
}

func (c *Conn) SendMessage(payload []byte, mt Opcode) (int, error) {
	if mt != TextMessage && mt != BinaryMessage {
		return 0, ErrInvalidMessageType
	}

	buf := makeFrameHeadersBuf(&Headers{
		FIN:           true,
		Opcode:        mt,
		PayloadLength: uint64(len(payload)),
		Mask:          !c.isServer,
	})

	buf = append(buf, payload...)

	n, err := c.netConn.Write(buf)
	if err != nil {
		return n, err
	}

	return n, nil
}

func (c *Conn) sendControl(mt Opcode, status uint16, reason []byte) (int, error) {
	headers := &Headers{
		FIN:    true,
		Opcode: mt,
		Mask:   !c.isServer,
	}

	// encode status code
	payload := make([]byte, 0)
	if mt == CloseFrame {
		statusBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(statusBuf, uint16(status))
		payload = append(payload, statusBuf...)
	}
	// append reason
	if len(reason) > 0 {
		payload = append(payload, reason...)
	}

	// Mask if we're a client
	if !c.isServer {
		maskingKey := makeMaskingKey()
		headers.MaskingKey = maskingKey
		toggleMask(payload, maskingKey)
	}

	// set PayloadLength
	headers.PayloadLength = uint64(len(payload))

	// make initial buf with headers
	buf := makeFrameHeadersBuf(headers)
	// append payload
	buf = append(buf, payload...)

	// write control
	n, err := c.netConn.Write(buf)

	return n, err
}

func (c *Conn) closeWithErr(code uint16) (Opcode, []byte, error) {
	_, err := c.sendControl(CloseFrame, code, nil)
	if isEOF(err) {
		return CloseFrame, nil, ErrUnexpectedClose
	}
	return CloseFrame, nil, ErrBadMessage
}

// Close writes the websocket close frame,
// flushes the buffer and closes the underlying connections.
func (c *Conn) Close() {
	c.netConn.Close()
}
