// Package util provides low-level helpers for the M-Bus reader: hex stream
// parsing for canned test responses, byte-level frame trimming, and the
// retry shaping primitives that the reader uses for transient port errors.
package util

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	bcd "github.com/johnsonjh/gobcd"
)

const checksumModulus = 256

func ComputeChecksum(data []byte) byte {
	sum := 0
	for _, v := range data {
		sum += int(v)
	}
	return byte(sum % checksumModulus)
}

func ComputeLRC(data []byte) byte {
	var lrc byte
	for _, v := range data {
		lrc ^= v
	}
	return lrc
}

func FormatBytesSlice(slice []byte) string {
	if len(slice) == 0 {
		return "[]"
	}

	var sb strings.Builder
	sb.WriteString("[ ")
	for _, b := range slice {
		fmt.Fprintf(&sb, "%02X ", b)
	}
	sb.WriteString("]")
	return sb.String()
}

func reverseSlice(slice []byte) []byte {
	reversedSlice := make([]byte, len(slice))
	for i, b := range slice {
		reversedSlice[len(slice)-1-i] = b
	}
	return reversedSlice
}

func BcdToDec(rawbytes []byte) int64 {
	return int64(bcd.ToUint32(reverseSlice(rawbytes)))
}

func ReadHexBytesFromFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	reader := bufio.NewReader(file)
	hexStr, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	return ParseHexBytes(hexStr)
}

func ParseHexBytes(hexStr string) ([]byte, error) {
	hexStr = strings.TrimSpace(hexStr)
	hexValues := strings.Split(hexStr, " ")

	var result []byte
	for _, hexVal := range hexValues {
		decoded, err := hex.DecodeString(hexVal)
		if err != nil {
			return nil, err
		}
		result = append(result, decoded[0])
	}

	return result, nil
}

const (
	asciiLetterOffset = 64
	base32            = 32
)

func ManufacturerIDToASCII(id uint16) string {
	// 1st letter
	firstLetterASCII := byte(id / (base32 * base32)) //nolint:gosec // G115: value bounded by protocol; fits in byte
	firstLetter := firstLetterASCII + asciiLetterOffset

	// 2nd letter
	id %= base32 * base32
	secondLetterASCII := byte(id / base32)
	secondLetter := secondLetterASCII + asciiLetterOffset

	// 3rd letter
	thirdLetterASCII := byte(id % base32)
	thirdLetter := thirdLetterASCII + asciiLetterOffset

	return string([]byte{firstLetter, secondLetter, thirdLetter})
}

const bytesForUint16 = 2

func BytesToUint16(b []byte) uint16 {
	if len(b) != bytesForUint16 {
		panic("input slice should have exactly 2 bytes")
	}
	return uint16(b[1])<<8 | uint16(b[0])
}
