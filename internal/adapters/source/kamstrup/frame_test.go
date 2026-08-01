package kamstrup

import (
	"bytes"
	"errors"
	"testing"
)

// TestCRC16CheckValue pins the CRC to the published CRC-16/XMODEM check
// value, independently of any frame built in this package.
func TestCRC16CheckValue(t *testing.T) {
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Fatalf("crc16(123456789) = 0x%04X, want 0x31C3", got)
	}
}

// TestEncodeFrame_KnownVectors pins the encoder to full frames with CRC bytes
// precomputed by an independent implementation, so the CRC and stuffing
// cannot drift together with the decoder.
func TestEncodeFrame_KnownVectors(t *testing.T) {
	tests := []struct {
		name    string
		start   byte
		payload []byte
		want    []byte
	}{
		{
			name:    "GetSerialNo request",
			start:   startRequest,
			payload: []byte{0x3F, 0x02},
			want:    []byte{0x80, 0x3F, 0x02, 0x35, 0xE9, 0x0D},
		},
		{
			name:    "GetRegister 60 request",
			start:   startRequest,
			payload: []byte{0x3F, 0x10, 0x01, 0x00, 0x3C},
			want:    []byte{0x80, 0x3F, 0x10, 0x01, 0x00, 0x3C, 0xB2, 0x5F, 0x0D},
		},
		{
			name:    "GetSerialNo response",
			start:   startResponse,
			payload: []byte{0x3F, 0x02, 0x00, 0xBC, 0x61, 0x4E},
			want:    []byte{0x40, 0x3F, 0x02, 0x00, 0xBC, 0x61, 0x4E, 0xB4, 0x83, 0x0D},
		},
		{
			// The 0x40 mantissa byte must be stuffed as 0x1B 0xBF.
			name:  "register 60 response with stuffed body byte",
			start: startResponse,
			payload: []byte{
				0x3F, 0x10, 0x00, 0x3C, 0x08, 0x04, 0x42, 0x00, 0x01, 0xE2, 0x40,
			},
			want: []byte{
				0x40, 0x3F, 0x10, 0x00, 0x3C, 0x08, 0x04, 0x42, 0x00, 0x01, 0xE2,
				0x1B, 0xBF, 0x7E, 0x4A, 0x0D,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeFrame(tt.start, tt.payload)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("encodeFrame() = % X, want % X", got, tt.want)
			}
			back, err := decodeFrame(got, tt.start)
			if err != nil {
				t.Fatalf("decodeFrame() error = %v", err)
			}
			if !bytes.Equal(back, tt.payload) {
				t.Errorf("decodeFrame() = % X, want % X", back, tt.payload)
			}
		})
	}
}

// TestStuff_EveryEscapedByte checks each reserved byte individually.
func TestStuff_EveryEscapedByte(t *testing.T) {
	tests := []struct {
		in   byte
		want []byte
	}{
		{0x06, []byte{0x1B, 0xF9}},
		{0x0D, []byte{0x1B, 0xF2}},
		{0x1B, []byte{0x1B, 0xE4}},
		{0x40, []byte{0x1B, 0xBF}},
		{0x80, []byte{0x1B, 0x7F}},
	}
	for _, tt := range tests {
		got := stuff([]byte{tt.in})
		if !bytes.Equal(got, tt.want) {
			t.Errorf("stuff(0x%02X) = % X, want % X", tt.in, got, tt.want)
		}
		back, err := unstuff(got)
		if err != nil {
			t.Fatalf("unstuff(% X) error = %v", got, err)
		}
		if !bytes.Equal(back, []byte{tt.in}) {
			t.Errorf("unstuff(% X) = % X, want %02X", got, back, tt.in)
		}
	}
}

func TestStuff_PassesPlainBytes(t *testing.T) {
	in := []byte{0x00, 0x3F, 0x10, 0xFF, 0x41, 0x7F}
	if got := stuff(in); !bytes.Equal(got, in) {
		t.Errorf("stuff(% X) = % X, want unchanged", in, got)
	}
}

func TestDecodeFrame_Errors(t *testing.T) {
	valid := encodeFrame(startResponse, []byte{0x3F, 0x02, 0x00, 0xBC, 0x61, 0x4E})

	corruptCRC := bytes.Clone(valid)
	corruptCRC[2] ^= 0x01

	noTerminator := bytes.Clone(valid[:len(valid)-1])

	trailingEscape := []byte{0x40, 0x3F, 0x1B, 0x0D}

	tests := []struct {
		name    string
		frame   []byte
		start   byte
		wantErr error
	}{
		{"crc mismatch", corruptCRC, startResponse, errCRC},
		{"missing terminator", noTerminator, startResponse, errTruncatedFrame},
		{"too short", []byte{0x40, 0x0D}, startResponse, errTruncatedFrame},
		{"escape at end of body", trailingEscape, startResponse, errTruncatedFrame},
		{"empty", nil, startResponse, errTruncatedFrame},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeFrame(tt.frame, tt.start)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("decodeFrame() error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	t.Run("wrong start byte", func(t *testing.T) {
		if _, err := decodeFrame(valid, startRequest); err == nil {
			t.Error("decodeFrame() with wrong start byte succeeded, want error")
		}
	})
}

// TestFrameRoundTrip covers payloads with reserved bytes in every position,
// including CRC bytes that themselves need stuffing.
func TestFrameRoundTrip(t *testing.T) {
	payloads := [][]byte{
		{0x3F, 0x01},
		{0x3F, 0x10, 0x01, 0x03, 0xEC},
		{0x06, 0x0D, 0x1B, 0x40, 0x80},
		{0x00},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
	}
	for _, p := range payloads {
		frame := encodeFrame(startRequest, p)
		got, err := decodeFrame(frame, startRequest)
		if err != nil {
			t.Fatalf("decodeFrame(encodeFrame(% X)) error = %v", p, err)
		}
		if !bytes.Equal(got, p) {
			t.Errorf("round trip of % X = % X", p, got)
		}
	}
}
