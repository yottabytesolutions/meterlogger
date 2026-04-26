package protocol

import (
	"bytes"
	"testing"
)

func TestShortFrame_ToBytes(t *testing.T) {
	sf := &ShortFrameStruct{
		Control:  0x40,
		Address:  0x01,
		Checksum: 0x41,
	}
	got := sf.ToBytes()
	want := []byte{ShortFrameHeader, 0x40, 0x01, 0x41, StopByte}
	if !bytes.Equal(got, want) {
		t.Errorf("ShortFrame.ToBytes() = %v, want %v", got, want)
	}
}

func TestShortFrame_FrameType(t *testing.T) {
	sf := &ShortFrameStruct{}
	if sf.FrameType() != ShortFrame {
		t.Errorf("ShortFrame.FrameType() = %v, want ShortFrame", sf.FrameType())
	}
}

func TestControlFrame_ToBytes(t *testing.T) {
	cf := &ControlFrameStruct{
		Length:      3,
		Control:     0x43,
		Address:     0x01,
		ControlInfo: 0x52,
		Checksum:    0x96,
	}
	got := cf.ToBytes()
	want := []byte{ControlOrLongFrameHeader, 3, 3, ControlOrLongFrameHeader, 0x43, 0x01, 0x52, 0x96, StopByte}
	if !bytes.Equal(got, want) {
		t.Errorf("ControlFrame.ToBytes() = %v, want %v", got, want)
	}
}

func TestControlFrame_FrameType(t *testing.T) {
	cf := &ControlFrameStruct{}
	if cf.FrameType() != ControlFrame {
		t.Errorf("ControlFrame.FrameType() = %v, want ControlFrame", cf.FrameType())
	}
}

func TestLongFrame_ToBytes(t *testing.T) {
	lf := &LongFrameStruct{
		Length:      4,
		Control:     0x08,
		Address:     0x01,
		ControlInfo: 0x72,
		Data:        []byte{0xAB},
		Checksum:    0x7C,
	}
	got := lf.ToBytes()
	// header, length, length, header, control, address, controlinfo, data..., checksum, stop
	want := []byte{ControlOrLongFrameHeader, 4, 4, ControlOrLongFrameHeader, 0x08, 0x01, 0x72, 0xAB, 0x7C, StopByte}
	if !bytes.Equal(got, want) {
		t.Errorf("LongFrame.ToBytes() = %v, want %v", got, want)
	}
}

func TestLongFrame_FrameType(t *testing.T) {
	lf := &LongFrameStruct{}
	if lf.FrameType() != LongFrame {
		t.Errorf("LongFrame.FrameType() = %v, want LongFrame", lf.FrameType())
	}
}

func TestAckFrame_ToBytes(t *testing.T) {
	af := &AckFrameStruct{}
	got := af.ToBytes()
	if !bytes.Equal(got, AckFrameBytes) {
		t.Errorf("AckFrame.ToBytes() = %v, want %v", got, AckFrameBytes)
	}
}

func TestAckFrame_FrameType(t *testing.T) {
	af := &AckFrameStruct{}
	if af.FrameType() != AckFrame {
		t.Errorf("AckFrame.FrameType() = %v, want AckFrame", af.FrameType())
	}
}

func TestAckFrame_Validate(t *testing.T) {
	af := &AckFrameStruct{}
	if !af.Validate() {
		t.Error("AckFrame.Validate() should always return true")
	}
}

func TestAckFrame_Prepare(t *testing.T) {
	af := &AckFrameStruct{}
	result := af.Prepare()
	if result != af {
		t.Error("AckFrame.Prepare() should return itself")
	}
}

func TestShortFrame_Prepare(t *testing.T) {
	sf := &ShortFrameStruct{
		Control: 0x40,
		Address: 0x01,
	}
	result := sf.Prepare()
	if result == nil {
		t.Fatal("ShortFrame.Prepare() returned nil")
	}
	// After prepare, checksum should be set correctly
	if !sf.Validate() {
		t.Errorf("ShortFrame after Prepare() fails Validate()")
	}
}

func TestShortFrame_Validate(t *testing.T) {
	sf := &ShortFrameStruct{
		Control: 0x40,
		Address: 0x01,
	}
	sf.Prepare()
	if !sf.Validate() {
		t.Error("ShortFrame.Validate() should return true after Prepare()")
	}

	sf.Checksum = 0x00
	if sf.Validate() {
		t.Error("ShortFrame.Validate() should return false with wrong checksum")
	}
}

func TestControlFrame_Prepare(t *testing.T) {
	cf := &ControlFrameStruct{
		Control:     0x43,
		Address:     0x01,
		ControlInfo: 0x52,
	}
	cf.Prepare()
	if cf.Length != 3 {
		t.Errorf("ControlFrame.Prepare() Length = %d, want 3", cf.Length)
	}
	if !cf.Validate() {
		t.Error("ControlFrame.Validate() should return true after Prepare()")
	}
}

func TestControlFrame_Validate(t *testing.T) {
	cf := &ControlFrameStruct{
		Control:     0x43,
		Address:     0x01,
		ControlInfo: 0x52,
	}
	cf.Prepare()
	if !cf.Validate() {
		t.Error("ControlFrame.Validate() should return true after Prepare()")
	}

	cf.Checksum = 0x00
	if cf.Validate() {
		t.Error("ControlFrame.Validate() should return false with wrong checksum")
	}
}

func TestLongFrame_Prepare(t *testing.T) {
	lf := &LongFrameStruct{
		Control:     0x08,
		Address:     0x01,
		ControlInfo: 0x72,
		Data:        []byte{0xAB, 0xCD},
	}
	lf.Prepare()
	// Length = 3 + len(data) = 3 + 2 = 5
	if lf.Length != 5 {
		t.Errorf("LongFrame.Prepare() Length = %d, want 5", lf.Length)
	}
	if !lf.Validate() {
		t.Errorf("LongFrame.Validate() should return true after Prepare()")
	}
}

func TestLongFrame_Validate(t *testing.T) {
	lf := &LongFrameStruct{
		Control:     0x08,
		Address:     0x01,
		ControlInfo: 0x72,
		Data:        []byte{0xAB},
	}
	lf.Prepare()
	if !lf.Validate() {
		t.Error("LongFrame.Validate() should return true after Prepare()")
	}

	lf.Checksum = 0x00
	if lf.Validate() {
		t.Error("LongFrame.Validate() should return false with wrong checksum")
	}
}

func TestCalculateChecksum(t *testing.T) {
	// Test via ShortFrame's Prepare/Validate cycle
	sf := &ShortFrameStruct{Control: 0x40, Address: 0xFE}
	sf.Prepare()
	expectedChecksum := byte((0x40 + 0xFE) & 0xFF)
	if sf.Checksum != expectedChecksum {
		t.Errorf("calculateChecksum via ShortFrame = %02X, want %02X", sf.Checksum, expectedChecksum)
	}
}

func TestParseUsingGombus_ValidFrame(t *testing.T) {
	// Valid long frame from testresponse/response.hex
	data := []byte{
		0x68, 0xC7, 0xC7, 0x68, 0x08, 0x01, 0x72, 0x02, 0x75, 0x92, 0x72,
		0x2D, 0x2C, 0x34, 0x0C, 0x53, 0x00, 0x00, 0x00,
		0x04, 0x0E, 0xE0, 0x01, 0x00, 0x00,
		0x04, 0xFF, 0x07, 0xBA, 0x0B, 0x00, 0x00,
		0x04, 0xFF, 0x08, 0x24, 0x07, 0x00, 0x00,
		0x04, 0x13, 0x91, 0x12, 0x00, 0x00,
		0x84, 0x40, 0x14, 0x00, 0x00, 0x00, 0x00,
		0x84, 0x80, 0x40, 0x14, 0x00, 0x00, 0x00, 0x00,
		0x04, 0x22, 0xF0, 0x0B, 0x00, 0x00,
		0x34, 0x22, 0x00, 0x00, 0x00, 0x00,
		0x02, 0x59, 0x50, 0x15, 0x02, 0x5D, 0xFF, 0x13, 0x02, 0x61, 0x51, 0x01,
		0x04, 0x2D, 0x00, 0x00, 0x00, 0x00,
		0x14, 0x2D, 0x28, 0x00, 0x00, 0x00,
		0x04, 0x3B, 0x00, 0x00, 0x00, 0x00,
		0x14, 0x3B, 0x5D, 0x00, 0x00, 0x00,
		0x04, 0xFF, 0x22, 0x00, 0x00, 0x00, 0x00,
		0x04, 0x6D, 0x3B, 0x2A, 0xF6, 0x27,
		0x44, 0x0E, 0xF4, 0x00, 0x00, 0x00,
		0x44, 0xFF, 0x07, 0x0C, 0x06, 0x00, 0x00,
		0x44, 0xFF, 0x08, 0xB5, 0x03, 0x00, 0x00,
		0x44, 0x13, 0x9E, 0x09, 0x00, 0x00,
		0xC4, 0x40, 0x14, 0x00, 0x00, 0x00, 0x00,
		0xC4, 0x80, 0x40, 0x14, 0x00, 0x00, 0x00, 0x00,
		0x54, 0x2D, 0x25, 0x00, 0x00, 0x00,
		0x54, 0x3B, 0x5D, 0x00, 0x00, 0x00,
		0x42, 0x6C, 0xE1, 0x27,
		0x02, 0xFF, 0x1A, 0x01, 0x1A,
		0x0C, 0x78, 0x02, 0x75, 0x92, 0x72,
		0x04, 0xFF, 0x16, 0x86, 0x0B, 0x20, 0x00,
		0x04, 0xFF, 0x17, 0xC9, 0xFF, 0x0E, 0x01,
		0x49, 0x16,
	}
	decoded, err := ParseUsingGombus(data)
	if err != nil {
		t.Fatalf("ParseUsingGombus() error: %v", err)
	}
	if decoded == nil {
		t.Fatal("ParseUsingGombus() returned nil")
	}
	if len(decoded.DataRecords) == 0 {
		t.Error("ParseUsingGombus() returned no data records")
	}
}

func TestParseUsingGombus_InvalidFrame(t *testing.T) {
	// Invalid frame - wrong CI byte
	data := []byte{0x68, 0x03, 0x03, 0x68, 0x08, 0x01, 0x00, 0x09, 0x16}
	_, err := ParseUsingGombus(data)
	if err == nil {
		t.Error("ParseUsingGombus() should return error for invalid frame")
	}
}

func TestFrameTypeConstants(t *testing.T) {
	if ShortFrame != 0 {
		t.Errorf("ShortFrame constant = %d, want 0", ShortFrame)
	}
	if ControlFrame != 1 {
		t.Errorf("ControlFrame constant = %d, want 1", ControlFrame)
	}
	if LongFrame != 2 {
		t.Errorf("LongFrame constant = %d, want 2", LongFrame)
	}
	if AckFrame != 3 {
		t.Errorf("AckFrame constant = %d, want 3", AckFrame)
	}
}
