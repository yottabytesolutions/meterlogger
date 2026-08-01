package sml

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// chunkReader returns at most one byte per Read call, exercising frames
// split across reads.
type chunkReader struct{ data []byte }

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	p[0] = c.data[0]
	c.data = c.data[1:]
	return 1, nil
}

func scanOneFrame(t *testing.T, src io.Reader) []byte {
	t.Helper()
	s := newFrameScanner(src, testLogger())
	frame, err := s.nextFrame(context.Background())
	if err != nil {
		t.Fatalf("nextFrame: %v", err)
	}
	return frame
}

func TestScannerFindsFrame(t *testing.T) {
	payload := mustHex(t, "76050000000162006200726500000101")
	frame := buildFrame(t, payload, crcVariantX25)

	tests := []struct {
		name   string
		stream []byte
	}{
		{"bare frame", frame},
		{"garbage prefix", append(mustHex(t, "00FF421B1B0101ABCD"), frame...)},
		{"partial start then frame", append(mustHex(t, "1B1B1B1B010102"), frame...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanOneFrame(t, bytes.NewReader(tt.stream))
			if !bytes.Equal(got, frame) {
				t.Errorf("frame mismatch:\ngot  % X\nwant % X", got, frame)
			}
		})
	}
}

func TestScannerSplitReads(t *testing.T) {
	payload := mustHex(t, "76050000000162006200726500000101")
	frame := buildFrame(t, payload, crcVariantX25)
	got := scanOneFrame(t, &chunkReader{data: append(mustHex(t, "AB"), frame...)})
	if !bytes.Equal(got, frame) {
		t.Errorf("frame mismatch across split reads")
	}
}

func TestScannerEscapedPayload(t *testing.T) {
	// Payload containing a literal escape sequence; buildFrame doubles it.
	payload := append(mustHex(t, "62011B1B1B1B6202"), 0x00, 0x00) // pad-friendly content
	frame := buildFrame(t, payload, crcVariantX25)
	got := scanOneFrame(t, bytes.NewReader(frame))
	if !bytes.Equal(got, frame) {
		t.Errorf("escaped frame mismatch:\ngot  % X\nwant % X", got, frame)
	}
	unescaped, variant, err := validateFrame(got)
	if err != nil {
		t.Fatalf("validateFrame: %v", err)
	}
	if variant != crcVariantX25 {
		t.Errorf("variant = %q, want x25", variant)
	}
	if !bytes.Equal(unescaped, payload) {
		t.Errorf("payload mismatch:\ngot  % X\nwant % X", unescaped, payload)
	}
}

func TestScannerResyncOnNestedStart(t *testing.T) {
	payload := mustHex(t, "62016202")
	frame := buildFrame(t, payload, crcVariantX25)
	// A truncated frame (start plus some payload, no end) directly followed
	// by a complete frame: the scanner restarts on the nested start.
	stream := append(mustHex(t, "1B1B1B1B01010101620362"), frame...)
	got := scanOneFrame(t, bytes.NewReader(stream))
	if !bytes.Equal(got, frame) {
		t.Errorf("expected the complete frame after resync")
	}
}

func TestScannerResyncOnUnknownEscape(t *testing.T) {
	payload := mustHex(t, "62016202")
	frame := buildFrame(t, payload, crcVariantX25)
	// An escape followed by garbage aborts the current frame.
	stream := append(mustHex(t, "1B1B1B1B01010101"+"6203"+"1B1B1B1B"+"DEADBEEF"), frame...)
	got := scanOneFrame(t, bytes.NewReader(stream))
	if !bytes.Equal(got, frame) {
		t.Errorf("expected the complete frame after resync")
	}
}

func TestScannerSizeCap(t *testing.T) {
	payload := mustHex(t, "62016202")
	frame := buildFrame(t, payload, crcVariantX25)
	// A start sequence followed by more than maxFrameBytes of payload
	// without an end: the scanner drops it and finds the next frame.
	oversized := make([]byte, 0, maxFrameBytes+len(frame)+64)
	oversized = append(oversized, escSeq...)
	oversized = append(oversized, startMsg...)
	oversized = append(oversized, bytes.Repeat([]byte{0x62}, maxFrameBytes+16)...)
	oversized = append(oversized, frame...)
	got := scanOneFrame(t, bytes.NewReader(oversized))
	if !bytes.Equal(got, frame) {
		t.Errorf("expected the frame after the oversized partial")
	}
}

func TestScannerEOF(t *testing.T) {
	s := newFrameScanner(bytes.NewReader(mustHex(t, "1B1B1B1B01010101FFFF")), testLogger())
	_, err := s.nextFrame(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected wrapped io.EOF, got %v", err)
	}
}

func TestScannerContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newFrameScanner(bytes.NewReader(nil), testLogger())
	if _, err := s.nextFrame(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestValidateFrame(t *testing.T) {
	payload := mustHex(t, "76050000000162006200726500000101")
	tests := []struct {
		name    string
		variant string
	}{
		{"x25", crcVariantX25},
		{"kermit", crcVariantKermit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := buildFrame(t, payload, tt.variant)
			got, variant, err := validateFrame(frame)
			if err != nil {
				t.Fatalf("validateFrame: %v", err)
			}
			if variant != tt.variant {
				t.Errorf("variant = %q, want %q", variant, tt.variant)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("payload mismatch: fill bytes not stripped?")
			}
		})
	}
}

func TestValidateFrameErrors(t *testing.T) {
	payload := mustHex(t, "76050000000162006200726500000101")
	good := buildFrame(t, payload, crcVariantX25)

	corrupted := append([]byte{}, good...)
	corrupted[10] ^= 0xFF

	badFill := append([]byte{}, good...)
	badFill[len(badFill)-3] = maxFillBytes + 1
	// Recompute the CRC so only the fill count is invalid.
	crc := swap16(crc16X25(badFill[:len(badFill)-2]))
	badFill[len(badFill)-2], badFill[len(badFill)-1] = byte(crc>>8), byte(crc&0xFF)

	tests := []struct {
		name  string
		frame []byte
	}{
		{"too short", good[:8]},
		{"corrupted payload", corrupted},
		{"invalid fill count", badFill},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := validateFrame(tt.frame); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}
