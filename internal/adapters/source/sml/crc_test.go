package sml

import "testing"

// Check values from the CRC catalogue: CRC-16/X-25 over "123456789" is
// 0x906E, CRC-16/Kermit over the same input is 0x2189.
func TestCRC16CheckValues(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]byte) uint16
		want uint16
	}{
		{"x25", crc16X25, 0x906E},
		{"kermit", crc16Kermit, 0x2189},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn([]byte("123456789")); got != tt.want {
				t.Errorf("got 0x%04X, want 0x%04X", got, tt.want)
			}
		})
	}
}

func TestSwap16(t *testing.T) {
	if got := swap16(0x906E); got != 0x6E90 {
		t.Errorf("swap16(0x906E) = 0x%04X, want 0x6E90", got)
	}
}

// TestCRC16RealFrame pins the CRC over a real frame prefix: the hardcoded
// import-total frame used in the parser tests, up to and including the
// fill count byte.
func TestCRC16RealFrame(t *testing.T) {
	data := mustHex(t,
		"1B1B1B1B01010101"+
			"77070100010800FF650001018001621E52FF590000000001C90CA700"+
			"1B1B1B1B1A01")
	if got := crc16X25(data); got != 0x56FA {
		t.Errorf("x25 = 0x%04X, want 0x56FA", got)
	}
	if got := crc16Kermit(data); got != 0x3578 {
		t.Errorf("kermit = 0x%04X, want 0x3578", got)
	}
}
