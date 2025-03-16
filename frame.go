package websocket

import (
	"encoding/binary"
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
	buf, err := c.readSkip(2)
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
		plBuf, err := c.readSkip(2)
		if err != nil {
			return nil, err
		}
		payloadLength = uint64(binary.BigEndian.Uint16(plBuf))
	case 127:
		plBuf, err := c.readSkip(8)
		if err != nil {
			return nil, err
		}
		payloadLength = uint64(binary.BigEndian.Uint16(plBuf))
	}

	var maskingKey []byte
	if mask {
		maskingKey, err = c.readSkip(4)
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
		PayloadLength: uint64(payloadLength),
		MaskingKey:    maskingKey,
	}, nil
}

func readToBool(byte, mask byte) bool {
	return byte&mask != 0
}
