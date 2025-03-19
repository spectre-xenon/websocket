package websocket

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"slices"
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

func (c *Conn) peekDiscard(n int) ([]byte, error) {
	buf, err := c.br.Peek(n)
	if err != nil {
		return buf, err
	}
	// Guaranteed to succeed, cause of check above
	c.br.Discard(len(buf))
	return buf, err
}

func (c *Conn) readPayload(pl uint64) ([]byte, error) {
	if pl == 0 {
		return make([]byte, 0), nil
	}

	payload := make([]byte, pl)
	_, err := io.ReadFull(c.br, payload)
	if err != nil {
		return payload, err
	}

	return payload, nil
}

func (c *Conn) discardRemaining(n int64) (int64, error) {
	nn, err := io.CopyN(io.Discard, c.br, n)
	return nn, err
}

func isEOF(err error) bool {
	return err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF)
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

func (c *Conn) handleTextMessage(h *Headers) (payload []byte, err error) {
	payload, err = c.readPayload(h.PayloadLength)
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
	return
}

func (c *Conn) handleBinaryMessage(h *Headers) (payload []byte, err error) {
	payload, err = c.readPayload(h.PayloadLength)
	if err != nil {
		return payload, err
	}
	// toggle mask if we're a server
	if c.isServer {
		toggleMask(payload, h.MaskingKey)
	}
	return
}

func (c *Conn) handleCloseFrame(h *Headers) (payload []byte, err error) {
	// Message must contain a status code
	if h.PayloadLength < 2 {
		return payload, ErrBadMessage
	}

	// read status code
	payload, err = c.readPayload(h.PayloadLength)
	if err != nil {
		return payload, err
	}
	// toggle mask if we're a server
	if c.isServer {
		toggleMask(payload, h.MaskingKey)
	}

	// parse status code
	statusCode := binary.BigEndian.Uint16(payload[0:2])
	// No need to handle ErrUnexpectedEOF
	if len(payload) <= 2 {
		payload = slices.Delete(payload, 0, 2)
	} else {
		payload = payload[2:]
	}

	// we don't care if sending the control fails here
	_, _ = c.sendControl(CloseFrame, statusCode, payload)
	return payload, ErrNormalClose
}

func (c *Conn) handlePingFrame(h *Headers) (payload []byte, err error) {
	payload, err = c.readPayload(h.PayloadLength)
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

	return
}

func (c *Conn) handleSingleFrame(h *Headers) (payload []byte, err error) {
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
		return
	default:
		// Unhandled Opcode
		return payload, ErrBadMessage
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
		// Single frame
		if initialHeaders.FIN {
			return initialHeaders.Opcode, initialPayload, nil
		}
		// illegal ContinuationFrame
		if initialHeaders.Opcode == ContinuationFrame {
			return c.closeWithErr(CloseProtocolError)
		}

		// Fragmented frames
		for {
			nextHeaders, err := c.parseFrameHeaders()
			if isEOF(err) {
				return CloseFrame, nil, ErrUnexpectedClose
			}

			// skip this frame if control frame
			if nextHeaders.Opcode == PingFrame || nextHeaders.Opcode == PongFrame {
				continue
			}
			// illegal text/binary frame
			if nextHeaders.Opcode != ContinuationFrame {
				return c.closeWithErr(CloseProtocolError)
			}

			nextPayload, err := c.handleSingleFrame(nextHeaders)
			if err != nil {
				return c.handleSingleMessageErr(err)
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
	var payload []byte
	if mt == CloseFrame {
		statusBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(statusBuf, uint16(status))
		payload = append(payload, statusBuf...)
	}
	// append reason
	payload = append(payload, reason...)

	// Mask if we're a client
	if !c.isServer && len(payload) > 0 {
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
	// close tcp connection
	c.Close()
	return CloseFrame, nil, ErrBadMessage
}

// Close writes the websocket close frame,
// flushes the buffer and closes the underlying connections.
func (c *Conn) Close() {
	// TODO: write Close frame

	if c.netConn != nil {
		c.netConn.Close()
	}
}
