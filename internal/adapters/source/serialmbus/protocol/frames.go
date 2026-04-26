// Package protocol contains the M-Bus framing primitives used by the
// serialmbus reader: short and long frame builders, control codes, and the
// init handshake. It wraps gombus types so the reader can stay agnostic of
// the upstream library shape.
package protocol

import (
	"github.com/jonaz/gombus"
)

const (
	// frameHeaderByteCount is the number of header bytes (Control + Address + ControlInfo) in a frame.
	frameHeaderByteCount = 3
	// checksumMask masks the checksum to a single byte.
	checksumMask = 0xFF
)

func (sf *ShortFrameStruct) ToBytes() []byte {
	return []byte{ShortFrameHeader, sf.Control, sf.Address, sf.Checksum, StopByte}
}

func (sf *ShortFrameStruct) FrameType() FrameType {
	return ShortFrame
}

func (cf *ControlFrameStruct) ToBytes() []byte {
	return []byte{
		ControlOrLongFrameHeader, cf.Length, cf.Length, ControlOrLongFrameHeader, cf.Control, cf.Address,
		cf.ControlInfo, cf.Checksum, StopByte,
	}
}

func (cf *ControlFrameStruct) FrameType() FrameType {
	return ControlFrame
}

func (lf *LongFrameStruct) ToBytes() []byte {
	msg := []byte{
		ControlOrLongFrameHeader, lf.Length, lf.Length, ControlOrLongFrameHeader, lf.Control, lf.Address,
		lf.ControlInfo,
	}
	msg = append(msg, lf.Data...)
	msg = append(msg, lf.Checksum, StopByte)
	return msg
}

func (lf *LongFrameStruct) FrameType() FrameType {
	return LongFrame
}

func (scf *AckFrameStruct) ToBytes() []byte {
	return AckFrameBytes
}

func (scf *AckFrameStruct) FrameType() FrameType {
	return AckFrame
}

func (sf *ShortFrameStruct) Validate() bool {
	expectedChecksum := calculateChecksum(sf.Control, sf.Address)
	return expectedChecksum == sf.Checksum
}

func (cf *ControlFrameStruct) Validate() bool {
	expectedChecksum := calculateChecksum(cf.Control, cf.Address, cf.ControlInfo)
	return expectedChecksum == cf.Checksum
}

func (lf *LongFrameStruct) Validate() bool {
	input := append([]byte{lf.Control, lf.Address, lf.ControlInfo}, lf.Data...)
	expectedChecksum := calculateChecksum(input...)
	return expectedChecksum == lf.Checksum
}

func (scf *AckFrameStruct) Validate() bool {
	return true
}

func (sf *ShortFrameStruct) Prepare() Frame {
	sf.Checksum = calculateChecksum(sf.Control, sf.Address)
	return sf
}

func (cf *ControlFrameStruct) Prepare() Frame {
	cf.Length = frameHeaderByteCount // 1 byte for Control + 1 byte for Address + 1 byte for ControlInfo
	cf.Checksum = calculateChecksum(cf.Control, cf.Address, cf.ControlInfo)
	return cf
}

func (lf *LongFrameStruct) Prepare() Frame {
	//nolint:gosec // G115: data length fits in byte for valid MBus frames
	lf.Length = frameHeaderByteCount + byte(len(lf.Data))
	allBytes := append([]byte{lf.Control, lf.Address, lf.ControlInfo}, lf.Data...)
	lf.Checksum = calculateChecksum(allBytes...)
	return lf
}

func (scf *AckFrameStruct) Prepare() Frame {
	// Nothing to prepare for SingleCharacterFrame, it's always 0xE5.
	return scf
}

func calculateChecksum(data ...byte) byte {
	sum := uint16(0)
	for _, b := range data {
		sum += uint16(b)
	}
	return byte(sum & checksumMask)
}

func ParseUsingGombus(data []byte) (*gombus.DecodedFrame, error) {
	frame := gombus.LongFrame(data)
	decoded, err := frame.Decode()
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
