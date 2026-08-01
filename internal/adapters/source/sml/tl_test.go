package sml

import (
	"bytes"
	"errors"
	"testing"
)

type decodeValueCase struct {
	name       string
	in         string
	wantTyp    byte
	wantAbsent bool
	wantInt    int64
	wantUint   uint64
	wantOctets string
	wantSize   int
}

func checkDecodedValue(t *testing.T, v value, tt decodeValueCase) {
	t.Helper()
	if v.typ != tt.wantTyp {
		t.Errorf("typ = 0x%02X, want 0x%02X", v.typ, tt.wantTyp)
	}
	if v.absent != tt.wantAbsent {
		t.Errorf("absent = %v, want %v", v.absent, tt.wantAbsent)
	}
	if v.i != tt.wantInt {
		t.Errorf("i = %d, want %d", v.i, tt.wantInt)
	}
	if v.u != tt.wantUint {
		t.Errorf("u = %d, want %d", v.u, tt.wantUint)
	}
	if !bytes.Equal(v.octets, []byte(tt.wantOctets)) {
		t.Errorf("octets = %q, want %q", v.octets, tt.wantOctets)
	}
	if v.size != tt.wantSize {
		t.Errorf("size = %d, want %d", v.size, tt.wantSize)
	}
}

func TestDecodeValue(t *testing.T) {
	tests := []decodeValueCase{
		{name: "absent marker", in: "01", wantTyp: typeOctet, wantAbsent: true, wantSize: 1},
		{name: "int8 negative", in: "52FF", wantTyp: typeInt, wantInt: -1, wantSize: 2},
		{name: "int8 positive", in: "527F", wantTyp: typeInt, wantInt: 127, wantSize: 2},
		{name: "int16 negative", in: "53FFFE", wantTyp: typeInt, wantInt: -2, wantSize: 3},
		{name: "int16 positive", in: "530102", wantTyp: typeInt, wantInt: 258, wantSize: 3},
		{name: "int24", in: "54800000", wantTyp: typeInt, wantInt: -8388608, wantSize: 4},
		{name: "int32 negative", in: "55FFFFFFD6", wantTyp: typeInt, wantInt: -42, wantSize: 5},
		{name: "int64", in: "590000000001C90CA7", wantTyp: typeInt, wantInt: 29953191, wantSize: 9},
		{name: "int64 negative", in: "59FFFFFFFFFFFFFFFF", wantTyp: typeInt, wantInt: -1, wantSize: 9},
		{name: "uint8", in: "621E", wantTyp: typeUint, wantUint: 30, wantSize: 2},
		{name: "uint16", in: "630102", wantTyp: typeUint, wantUint: 258, wantSize: 3},
		{name: "uint32", in: "6500010180", wantTyp: typeUint, wantUint: 65920, wantSize: 5},
		{name: "uint64", in: "69FFFFFFFFFFFFFFFF", wantTyp: typeUint, wantUint: ^uint64(0), wantSize: 9},
		{name: "bool", in: "4201", wantTyp: typeBool, wantUint: 1, wantSize: 2},
		{name: "octet string", in: "0748414C4C4F21", wantTyp: typeOctet, wantOctets: "HALLO!", wantSize: 7},
		{
			name: "octet string multi-byte TL", in: "8102" + "30313233343536373839414243444546",
			wantTyp: typeOctet, wantOctets: "0123456789ABCDEF", wantSize: 18,
		},
		{name: "list header", in: "77", wantTyp: typeList, wantSize: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := decodeValue(mustHex(t, tt.in))
			if err != nil {
				t.Fatalf("decodeValue: %v", err)
			}
			checkDecodedValue(t, v, tt)
		})
	}
}

func TestDecodeValueErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty buffer", ""},
		{"truncated int", "5901020304"},
		{"truncated TL continuation", "81"},
		{"invalid TL continuation type", "8152"},
		{"int too wide", "5A000000000000000000"},
		{"bool wrong width", "430101"},
		{"length shorter than TL", "50"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeValue(mustHex(t, tt.in)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestSkipValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"absent", "01FF", 1},
		{"uint8", "621EFF", 2},
		{"sml time list", "726201650000000AFF", 8},
		{"nested list", "727201620101", 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := skipValue(mustHex(t, tt.in))
			if err != nil {
				t.Fatalf("skipValue: %v", err)
			}
			if n != tt.want {
				t.Errorf("n = %d, want %d", n, tt.want)
			}
		})
	}
}

func TestSkipValueTruncatedList(t *testing.T) {
	if _, err := skipValue(mustHex(t, "726201")); !errors.Is(err, errTruncated) {
		t.Errorf("expected errTruncated, got %v", err)
	}
}
