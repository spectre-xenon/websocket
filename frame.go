package websocket

import (
	"encoding/binary"
	"math"
)

type Opcode byte

// Opcode frame header
const (
	// Denotes a continuation frame.
	ContinuationFrame Opcode = iota
	// Denotes a text frame.
	TextMessage
	// Denotes a binary frame.
	BinaryMessage

	/*
		RM3-RM7 are reserved for non control frames.
	*/
	RM3
	RM4
	RM5
	RM6
	RM7

	// Denotes a close frame.
	CloseFrame
	// Denotes a ping frame.
	PingFrame
	// Denotes a pong frame.
	PongFrame

	/*
		RM11-RM15 are reserved for further control frames
		yet to be defined.
	*/
	RM11
	RM12
	RM13
	RM14
	RM15
)

// Helper masks for getting header bits
const (
	// Byte: 0; bit: 10000000
	finMask = 1 << 7 // 10000000

	// Byte: 0; bit: 01000000
	rsv1Mask = 1 << 6
	// Byte: 0; bit: 00100000
	rsv2Mask = 1 << 5
	// Byte: 0; bit: 00010000
	rsv3Mask = 1 << 4

	// Byte: 0; bit: 00001111
	opcodeMask = 0x0F

	// Byte: 1; bit: 10000000
	maskMask = 1 << 7

	// Byte: 1; bit: 01111111
	payloadLengthMask = 0x7F
)

const (
	CloseNormal = 1000 + iota
	CloseGoingAway
	CloseProtocolError
	CloseUnknownData
	CloseReserved
	CloseNoStatus
	CloseAbnormal
	CloseMistachedPayloadData
	ClosePolicyViolation
	CloseFrameTooBig
	CloseRequiredExtension
	CloseInternalServerErr
	CloseFailedTLS = 1015
)

const (
	maxControlFramePayloadSize = 125
)

var validCloseFrameCodes = map[int]bool{
	CloseNormal:               true,
	CloseGoingAway:            true,
	CloseProtocolError:        true,
	CloseUnknownData:          true,
	CloseReserved:             true,
	CloseNoStatus:             false,
	CloseAbnormal:             false,
	CloseMistachedPayloadData: true,
	ClosePolicyViolation:      true,
	CloseFrameTooBig:          true,
	CloseRequiredExtension:    true,
	CloseFailedTLS:            false,
}

type Headers struct {
	// FIN indicates that this is the final fragment in a message.
	// The first fragment MAY also be the final fragment.
	FIN bool

	// RSV1-RSV3 are reserved bits for extensions,
	// MUST be 0 unless an extension is configured.
	RSV1, RSV2, RSV3 bool

	// Opcode is the code denoting the
	// interpretation of the "Payload data"
	Opcode Opcode

	// Mask Defines whether the "Payload data" is masked.
	Mask bool

	// "Payload data" length header
	PayloadLength uint64

	// MaskingKey is a 32-bit value present if Mask header is set to 1.
	// Used to mask and unmask the "Payload data"
	MaskingKey []byte
}

func (c *Conn) parseFrameHeaders() (*Headers, error) {
	buf, err := c.peekDiscard(2)
	if err != nil {
		return nil, err
	}

	fin := readToBool(buf[0], finMask)
	rsv1 := readToBool(buf[0], rsv1Mask)
	rsv2 := readToBool(buf[0], rsv2Mask)
	rsv3 := readToBool(buf[0], rsv3Mask)

	opcode := Opcode(buf[0] & opcodeMask)

	mask := readToBool(buf[1], maskMask)

	payloadLength := uint64(buf[1] & payloadLengthMask)

	switch payloadLength {
	case 126:
		plBuf, err := c.peekDiscard(2)
		if err != nil {
			return nil, err
		}
		payloadLength = uint64(binary.BigEndian.Uint16(plBuf))
	case 127:
		plBuf, err := c.peekDiscard(8)
		if err != nil {
			return nil, err
		}
		payloadLength = binary.BigEndian.Uint64(plBuf)
	}

	var maskingKey []byte
	if mask {
		maskingKey, err = c.peekDiscard(4)
		if err != nil {
			return nil, err
		}
	}

	return &Headers{
		FIN:           fin,
		RSV1:          rsv1,
		RSV2:          rsv2,
		RSV3:          rsv3,
		Opcode:        opcode,
		Mask:          mask,
		PayloadLength: payloadLength,
		MaskingKey:    maskingKey,
	}, nil
}

func makeFrameHeadersBuf(h *Headers) []byte {
	buf := make([]byte, 0)

	// Intialize as 0 and apply masks
	var byte0 byte = 0
	if h.FIN {
		byte0 |= finMask
	}
	if h.RSV1 {
		byte0 |= rsv1Mask
	}
	if h.RSV2 {
		byte0 |= rsv2Mask
	}
	if h.RSV3 {
		byte0 |= rsv3Mask
	}
	byte0 |= byte(h.Opcode)
	// Append first byte
	buf = append(buf, byte0)

	// Initialize the second byte
	var byte1 byte = 0
	if h.Mask {
		byte1 |= maskMask
	}

	// Add PayloadLength bytes
	pl := h.PayloadLength
	switch {
	case pl <= 125:
		byte1 |= byte(pl)
		// Append second byte
		buf = append(buf, byte1)
	case pl <= math.MaxUint16:
		// Create Uint16 bytes from number as Network bytes order
		byte1 |= 126
		plBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(plBytes, uint16(pl))
		// Append second bytes
		buf = append(buf, byte1)
		buf = append(buf, plBytes...)
	default:
		// Number is Uint64
		byte1 |= 127
		plBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(plBytes, pl)
		// Append second bytes
		buf = append(buf, byte1)
		buf = append(buf, plBytes...)
	}

	if h.Mask {
		buf = append(buf, h.MaskingKey...)
	}

	return buf
}

func readToBool(byte, mask byte) bool {
	return byte&mask != 0
}
