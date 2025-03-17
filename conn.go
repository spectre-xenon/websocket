package websocket

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
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

func (c *Conn) handleSingleFrame(h *Headers, fragmented bool) (payload []byte, err error) {
	if fragmented {
		if h.Opcode != ContinuationFrame {
			_, err = c.sendControl(CloseFrame, CloseMistachedPayloadData, nil)
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				return payload, ErrUnexpectedClose
			}
			return payload, ErrBadMessage
		}

		payload, err = c.readPayload(h.PayloadLength)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}

		if h.Opcode == TextMessage {
			isValid := utf8.Valid(payload)
			if !isValid {
				_, err = c.sendControl(CloseFrame, CloseMistachedPayloadData, nil)
				if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
					return payload, ErrUnexpectedClose
				}
				return payload, ErrBadMessage
			}
		}

		return payload, nil
	}

	// intial frame or is a single frame.
	switch h.Opcode {
	case TextMessage:
		payload, err = c.readPayload(h.PayloadLength)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}

		// toggle mask
		toggleMask(payload, h.MaskingKey)

		isValid := utf8.Valid(payload)
		if !isValid {
			_, err = c.sendControl(CloseFrame, CloseMistachedPayloadData, nil)
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				return payload, ErrUnexpectedClose
			}
			return payload, ErrBadMessage
		}

	case BinaryMessage:
		payload, err = c.readPayload(h.PayloadLength)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}
		// toggle mask
		toggleMask(payload, h.MaskingKey)
	case CloseFrame:
		if h.PayloadLength < 2 {
			_, _ = c.sendControl(CloseFrame, CloseNormal, payload)
			return payload, ErrBadMessage
		}
		// read status code
		payload, err = c.readPayload(h.PayloadLength)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}
		// toggle mask
		toggleMask(payload, h.MaskingKey)

		statusCode := binary.BigEndian.Uint16(payload[0:2])
		// No need to handle ErrUnexpectedEOF
		if len(payload) <= 2 {
			payload = slices.Delete(payload, 0, 2)
		} else {
			payload = payload[2:]
		}
		_, _ = c.sendControl(CloseFrame, statusCode, payload)
		return payload, ErrNormalClose
	case PingFrame:
		payload, err = c.readPayload(h.PayloadLength)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}
		// toggle mask
		toggleMask(payload, h.MaskingKey)

		_, err = c.sendControl(PongFrame, 0, payload)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}
	case PongFrame:
		return
	default:
		_, err = c.sendControl(CloseFrame, CloseProtocolError, nil)
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return payload, ErrUnexpectedClose
		}
		return make([]byte, 0), ErrBadMessage
	}

	return
}

func (c *Conn) NextMessage() (Opcode, []byte, error) {
	// loop and ignore control message (eg. PING PONG)
	for {
		initialHeaders, err := c.parseFrameHeaders()
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return CloseFrame, nil, ErrUnexpectedClose
		}

		// Check reserved bits
		if initialHeaders.RSV1 || initialHeaders.RSV2 || initialHeaders.RSV3 {
			reason := "Reserved bits set"
			_, err := c.sendControl(CloseFrame, CloseProtocolError, []byte(reason))
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				return CloseFrame, nil, ErrUnexpectedClose
			}
			return CloseFrame, nil, ErrBadMessage
		}

		// Client messages must be masked
		if c.isServer && !initialHeaders.Mask {
			reason := "Received unmasked message"
			_, err := c.sendControl(CloseFrame, CloseProtocolError, []byte(reason))
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				return CloseFrame, nil, ErrUnexpectedClose
			}
			return CloseFrame, nil, ErrBadMessage
		}

		initialPayload, err := c.handleSingleFrame(initialHeaders, false)
		if err != nil {
			return CloseFrame, nil, err
		}

		// skip this frame if control frame
		if initialHeaders.Opcode == PingFrame || initialHeaders.Opcode == PongFrame {
			continue
		}

		// Single frame
		if initialHeaders.FIN {
			return initialHeaders.Opcode, initialPayload, nil
		}

		if initialHeaders.Opcode != TextMessage && initialHeaders.Opcode != BinaryMessage {
			return CloseFrame, nil, ErrUnexpectedClose
		}

		// Fragmented frames
		for {
			nextHeaders, err := c.parseFrameHeaders()
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				return CloseFrame, nil, ErrUnexpectedClose
			}

			nextPayload, err := c.handleSingleFrame(nextHeaders, true)
			if err != nil {
				return CloseFrame, nil, err
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

	fmt.Printf("control payload: %v", payload)

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

// Close writes the websocket close frame,
// flushes the buffer and closes the underlying connections.
func (c *Conn) Close() {
	// TODO: write Close frame

	c.netConn.Close()
}
