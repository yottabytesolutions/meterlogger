package util

import (
	"strings"
	"testing"
)

const (
	testCaseSingleByte    = "single byte"
	testCaseMultipleBytes = "multiple bytes"
)

func TestBcdToDec(t *testing.T) {
	type args struct {
		bcd []byte
	}
	tests := []struct {
		name string
		args args
		want int64
	}{
		{
			name: "Nothing",
			args: args{
				bcd: []byte{},
			},
			want: 0,
		},
		{
			name: "Simple",
			args: args{
				bcd: []byte{0x02, 0x75, 0x92, 0x72},
			},
			want: 72927502,
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				if got := BcdToDec(tt.args.bcd); got != tt.want {
					t.Errorf("BcdToDec() = %v, want %v", got, tt.want)
				}
			},
		)
	}
}

func TestComputeChecksum(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want byte
	}{
		{
			name: "empty",
			data: []byte{},
			want: 0,
		},
		{
			name: testCaseSingleByte,
			data: []byte{0x10},
			want: 0x10,
		},
		{
			name: "overflow wraps at 256",
			data: []byte{0xFF, 0x01},
			want: 0x00,
		},
		{
			name: testCaseMultipleBytes,
			data: []byte{0x40, 0x30, 0x05},
			want: byte((0x40 + 0x30 + 0x05) % 256),
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got := ComputeChecksum(tt.data)
				if got != tt.want {
					t.Errorf("ComputeChecksum() = %02X, want %02X", got, tt.want)
				}
			},
		)
	}
}

func TestComputeLRC(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want byte
	}{
		{
			name: "empty",
			data: []byte{},
			want: 0,
		},
		{
			name: testCaseSingleByte,
			data: []byte{0xAB},
			want: 0xAB,
		},
		{
			name: "XOR of same bytes cancels out",
			data: []byte{0xAB, 0xAB},
			want: 0x00,
		},
		{
			name: testCaseMultipleBytes,
			data: []byte{0x01, 0x02, 0x04},
			want: 0x07,
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got := ComputeLRC(tt.data)
				if got != tt.want {
					t.Errorf("ComputeLRC() = %02X, want %02X", got, tt.want)
				}
			},
		)
	}
}

func TestFormatBytesSlice(t *testing.T) {
	tests := []struct {
		name  string
		slice []byte
		want  string
	}{
		{
			name:  "empty slice",
			slice: []byte{},
			want:  "[]",
		},
		{
			name:  testCaseSingleByte,
			slice: []byte{0x0A},
			want:  "[ 0A ]",
		},
		{
			name:  testCaseMultipleBytes,
			slice: []byte{0x10, 0xFF, 0x00},
			want:  "[ 10 FF 00 ]",
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got := FormatBytesSlice(tt.slice)
				if got != tt.want {
					t.Errorf("FormatBytesSlice() = %q, want %q", got, tt.want)
				}
			},
		)
	}
}

func TestFormatBytesSlice_ContainsBytes(t *testing.T) {
	result := FormatBytesSlice([]byte{0xDE, 0xAD})
	if !strings.Contains(result, "DE") || !strings.Contains(result, "AD") {
		t.Errorf("FormatBytesSlice result %q missing expected bytes", result)
	}
}

func TestParseHexBytes(t *testing.T) {
	tests := []struct {
		name    string
		hexStr  string
		want    []byte
		wantErr bool
	}{
		{
			name:    "simple hex bytes",
			hexStr:  "10 FF 00",
			want:    []byte{0x10, 0xFF, 0x00},
			wantErr: false,
		},
		{
			name:    "with newline trimmed",
			hexStr:  "AB CD\n",
			want:    []byte{0xAB, 0xCD},
			wantErr: false,
		},
		{
			name:    "invalid hex",
			hexStr:  "ZZ",
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got, err := ParseHexBytes(tt.hexStr)
				if (err != nil) != tt.wantErr {
					t.Errorf("ParseHexBytes() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if len(got) != len(tt.want) {
					t.Errorf("ParseHexBytes() len = %d, want %d", len(got), len(tt.want))
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("ParseHexBytes()[%d] = %02X, want %02X", i, got[i], tt.want[i])
					}
				}
			},
		)
	}
}

func TestManufacturerIDToASCII(t *testing.T) {
	tests := []struct {
		name string
		id   uint16
		want string
	}{
		{
			name: "ABC",
			// A=1, B=2, C=3 → 1*1024 + 2*32 + 3 = 1024+64+3=1091
			id:   1091,
			want: "ABC",
		},
		{
			name: "ELS",
			// E=5, L=12, S=19 → 5*1024 + 12*32 + 19 = 5120+384+19=5523
			id:   5523,
			want: "ELS",
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got := ManufacturerIDToASCII(tt.id)
				if got != tt.want {
					t.Errorf("ManufacturerIDToASCII(%d) = %q, want %q", tt.id, got, tt.want)
				}
			},
		)
	}
}

func TestBytesToUint16(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  uint16
	}{
		{
			name:  "little endian low byte",
			input: []byte{0x01, 0x00},
			want:  0x0001,
		},
		{
			name:  "little endian high byte",
			input: []byte{0x00, 0x01},
			want:  0x0100,
		},
		{
			name:  "both bytes set",
			input: []byte{0x34, 0x12},
			want:  0x1234,
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got := BytesToUint16(tt.input)
				if got != tt.want {
					t.Errorf("BytesToUint16() = %04X, want %04X", got, tt.want)
				}
			},
		)
	}
}

func TestBytesToUint16_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("BytesToUint16 with wrong length did not panic")
		}
	}()
	BytesToUint16([]byte{0x01})
}

func TestReadHexBytesFromFile_NonExistent(t *testing.T) {
	_, err := ReadHexBytesFromFile("/nonexistent/file.hex")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestReadHexBytesFromFile_ValidFile(t *testing.T) {
	// Use the testresponse/response.hex file (first line of hex data)
	bytes, err := ReadHexBytesFromFile("../../../../../testresponse/response.hex")
	if err != nil {
		t.Fatalf("ReadHexBytesFromFile() error: %v", err)
	}
	if len(bytes) == 0 {
		t.Error("ReadHexBytesFromFile() returned empty bytes")
	}
	// First byte of response.hex is 0x68
	if bytes[0] != 0x68 {
		t.Errorf("first byte = %02X, want 0x68", bytes[0])
	}
}
