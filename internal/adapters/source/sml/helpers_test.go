package sml

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// Test helpers: a hex literal decoder and a minimal SML transport frame
// encoder used to synthesize frames without a real meter.

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "\n", ""))
	if err != nil {
		t.Fatalf("bad hex literal %q: %v", s, err)
	}
	return b
}

// buildFrame wraps payload in a transport frame: escaping, fill bytes, end
// escape, and the CRC of the requested variant.
func buildFrame(t *testing.T, payload []byte, variant string) []byte {
	t.Helper()
	escaped := bytes.ReplaceAll(payload, escSeq, append(append([]byte{}, escSeq...), escSeq...))
	fill := (4 - len(escaped)%4) % 4
	frame := make([]byte, 0, len(escaped)+frameOverhead+fill)
	frame = append(frame, escSeq...)
	frame = append(frame, startMsg...)
	frame = append(frame, escaped...)
	frame = append(frame, make([]byte, fill)...)
	frame = append(frame, escSeq...)
	frame = append(frame, endMsgByte, byte(fill))
	var crc uint16
	switch variant {
	case crcVariantX25:
		crc = swap16(crc16X25(frame))
	case crcVariantKermit:
		crc = swap16(crc16Kermit(frame))
	default:
		t.Fatalf("unknown CRC variant %q", variant)
	}
	return append(frame, byte(crc>>8), byte(crc))
}
