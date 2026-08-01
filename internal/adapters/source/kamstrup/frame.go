package kamstrup

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// KMP frame delimiters and the byte-stuffing escape.
const (
	startRequest  = 0x80 // first byte of a frame sent to the meter
	startResponse = 0x40 // first byte of a frame sent by the meter
	frameEnd      = 0x0D
	escapeByte    = 0x1B
	ackByte       = 0x06 // reserved on the wire, must be stuffed too
	escapeXor     = 0xFF // an escaped byte is sent as escapeByte, byte^escapeXor
)

const (
	crcPolynomial = 0x1021
	crcLen        = 2 // CRC bytes appended to a payload
	bitsPerByte   = 8
)

// maxFrameLen bounds a stuffed response frame. The largest response we ask
// for is a handful of register blocks; 256 bytes is far above any legal size.
const maxFrameLen = 256

var (
	errCRC            = errors.New("crc mismatch")
	errTruncatedFrame = errors.New("truncated frame")
)

// crc16 computes the CCITT CRC (polynomial 0x1021, initial value 0x0000,
// no reflection, no final xor) over data. Appending the result big endian to
// data makes the CRC of the whole checksum to zero, which is how received
// frames are verified.
func crc16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << bitsPerByte
		for range bitsPerByte {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ crcPolynomial
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// needsEscape reports whether b collides with a frame delimiter or the escape
// itself and must be stuffed on the wire.
func needsEscape(b byte) bool {
	switch b {
	case ackByte, frameEnd, escapeByte, startResponse, startRequest:
		return true
	}
	return false
}

// stuff escapes every reserved byte in body as 0x1B followed by the byte
// xor 0xFF. Applied after the CRC is appended, per the KMP protocol.
func stuff(body []byte) []byte {
	out := make([]byte, 0, len(body))
	for _, b := range body {
		if needsEscape(b) {
			out = append(out, escapeByte, b^escapeXor)
			continue
		}
		out = append(out, b)
	}
	return out
}

// unstuff reverses stuff. It fails on a trailing escape byte.
func unstuff(body []byte) ([]byte, error) {
	out := make([]byte, 0, len(body))
	for i := 0; i < len(body); i++ {
		b := body[i]
		if b != escapeByte {
			out = append(out, b)
			continue
		}
		i++
		if i >= len(body) {
			return nil, fmt.Errorf("%w: escape byte at end of frame", errTruncatedFrame)
		}
		out = append(out, body[i]^escapeXor)
	}
	return out, nil
}

// encodeFrame builds a complete wire frame: start byte, byte-stuffed payload
// plus CRC, and the 0x0D terminator.
func encodeFrame(start byte, payload []byte) []byte {
	body := make([]byte, 0, len(payload)+crcLen)
	body = append(body, payload...)
	body = binary.BigEndian.AppendUint16(body, crc16(payload))

	frame := make([]byte, 0, len(body)+crcLen)
	frame = append(frame, start)
	frame = append(frame, stuff(body)...)
	frame = append(frame, frameEnd)
	return frame
}

// decodeFrame validates and unwraps a complete wire frame, returning the
// payload without the CRC. The frame must begin with the expected start byte
// and end with 0x0D.
func decodeFrame(frame []byte, start byte) ([]byte, error) {
	const minFrameLen = 4 // start + at least the two CRC bytes + end
	if len(frame) < minFrameLen {
		return nil, fmt.Errorf("%w: %d bytes", errTruncatedFrame, len(frame))
	}
	if frame[0] != start {
		return nil, fmt.Errorf("unexpected start byte 0x%02X, want 0x%02X", frame[0], start)
	}
	if frame[len(frame)-1] != frameEnd {
		return nil, fmt.Errorf("%w: missing 0x0D terminator", errTruncatedFrame)
	}

	body, err := unstuff(frame[1 : len(frame)-1])
	if err != nil {
		return nil, err
	}
	if len(body) < crcLen {
		return nil, fmt.Errorf("%w: no room for crc", errTruncatedFrame)
	}
	if crc16(body) != 0 {
		return nil, errCRC
	}
	return body[:len(body)-crcLen], nil
}
