package sml

import (
	"errors"
	"fmt"
)

// Type-length (TL) decoding for SML basic values. Every SML value starts
// with a TL byte: the high nibble carries the type (0 octet string, 4
// boolean, 5 signed integer, 6 unsigned integer, 7 list), the low nibble
// the total length including the TL byte itself. When bit 7 is set the
// length continues in the next byte's low nibble. For lists the length is
// the number of elements instead of a byte count. The single byte 0x01 is
// an empty octet string, which SML uses as the "optional field absent"
// marker.

const (
	typeOctet = 0x00
	typeBool  = 0x40
	typeInt   = 0x50
	typeUint  = 0x60
	typeList  = 0x70

	tlContinueBit = 0x80
	tlTypeMask    = 0x70
	tlLengthMask  = 0x0F
	nibbleBits    = 4
	byteBits      = 8

	maxValueBytes = 8
)

var errTruncated = errors.New("truncated SML value")

// value is one decoded SML basic value.
type value struct {
	typ    byte // typeOctet, typeBool, typeInt, typeUint, or typeList
	absent bool // true for the 0x01 "optional field absent" marker
	i      int64
	u      uint64
	octets []byte
	elems  int // element count when typ == typeList
	size   int // bytes consumed from the buffer, including TL bytes
}

// decodeTL reads the TL byte(s) at the start of buf. It returns the type,
// the length (total bytes including TL for scalar types, element count for
// lists), and the number of TL bytes consumed.
func decodeTL(buf []byte) (byte, int, int, error) {
	if len(buf) == 0 {
		return 0, 0, 0, errTruncated
	}
	typ := buf[0] & tlTypeMask
	length := int(buf[0] & tlLengthMask)
	tlLen := 1
	for buf[tlLen-1]&tlContinueBit != 0 {
		if tlLen >= len(buf) {
			return 0, 0, 0, errTruncated
		}
		next := buf[tlLen]
		if next&tlTypeMask != 0 {
			return 0, 0, 0, fmt.Errorf("invalid TL continuation byte 0x%02X", next)
		}
		length = length<<nibbleBits | int(next&tlLengthMask)
		tlLen++
	}
	return typ, length, tlLen, nil
}

// decodeValue decodes the SML basic value at the start of buf. For lists it
// decodes only the header; the caller walks the elements itself (see
// skipValue).
func decodeValue(buf []byte) (value, error) {
	typ, length, tlLen, err := decodeTL(buf)
	if err != nil {
		return value{}, err
	}
	if typ == typeList {
		return value{typ: typ, elems: length, size: tlLen}, nil
	}
	if length < tlLen || length > len(buf) {
		return value{}, errTruncated
	}
	data := buf[tlLen:length]
	v := value{typ: typ, size: length}
	switch typ {
	case typeOctet:
		v.octets = data
		v.absent = len(data) == 0
	case typeBool:
		if len(data) != 1 {
			return value{}, fmt.Errorf("boolean with %d data bytes", len(data))
		}
		v.u = uint64(data[0])
	case typeInt:
		if len(data) == 0 || len(data) > maxValueBytes {
			return value{}, fmt.Errorf("signed integer with %d data bytes", len(data))
		}
		i := int64(int8(data[0])) //nolint:gosec // G115: intentional sign extension from the first byte
		for _, b := range data[1:] {
			i = i<<byteBits | int64(b)
		}
		v.i = i
	case typeUint:
		if len(data) == 0 || len(data) > maxValueBytes {
			return value{}, fmt.Errorf("unsigned integer with %d data bytes", len(data))
		}
		var u uint64
		for _, b := range data {
			u = u<<byteBits | uint64(b)
		}
		v.u = u
	default:
		return value{}, fmt.Errorf("unknown SML type nibble 0x%02X", typ)
	}
	return v, nil
}

// skipValue returns the number of bytes the value at the start of buf
// occupies, descending into lists recursively.
func skipValue(buf []byte) (int, error) {
	v, err := decodeValue(buf)
	if err != nil {
		return 0, err
	}
	if v.typ != typeList {
		return v.size, nil
	}
	pos := v.size
	for range v.elems {
		if pos > len(buf) {
			return 0, errTruncated
		}
		n, elemErr := skipValue(buf[pos:])
		if elemErr != nil {
			return 0, elemErr
		}
		pos += n
	}
	return pos, nil
}
