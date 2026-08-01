package sml

// CRC-16 for SML transport frames. The frame checksum is the reflected
// CRC-16 with polynomial 0x8408 (0x1021 reversed). Standards-compliant
// meters use the X-25 parameters (init 0xFFFF, final XOR 0xFFFF, check
// value 0x906E for "123456789"). The Holley DTZ541 firmware instead uses
// the Kermit parameters (init 0x0000, no final XOR, check value 0x2189).
// In both cases libsml-style firmware writes the result byte-swapped:
// the low byte of the computed value comes first on the wire.

const (
	crcPolyReflected = 0x8408
	crcInitX25       = 0xFFFF
	crcInitKermit    = 0x0000
	crcFinalXorX25   = 0xFFFF
)

// crc16Reflected computes the reflected CRC-16 over data with the given
// initial value. No final XOR is applied; callers apply their variant's
// final XOR themselves.
func crc16Reflected(data []byte, initial uint16) uint16 {
	crc := initial
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&0x0001 != 0 {
				crc = (crc >> 1) ^ crcPolyReflected
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// crc16X25 computes the CRC-16/X-25 value (before the wire byte swap).
func crc16X25(data []byte) uint16 {
	return crc16Reflected(data, crcInitX25) ^ crcFinalXorX25
}

// crc16Kermit computes the CRC-16/Kermit value (before the wire byte swap).
func crc16Kermit(data []byte) uint16 {
	return crc16Reflected(data, crcInitKermit)
}

// swap16 swaps the bytes of a CRC value into the order SML frames carry it.
func swap16(v uint16) uint16 {
	return v>>8 | v<<8
}
