package gridmeter

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// DLMS general-glo-ciphering frame layout as pushed by Luxembourgish Smarty
// and Austrian Sagemcom T210-D meters:
//
//	offset 0        0xDB tag
//	offset 1        system title length (always 8)
//	offset 2..9     system title
//	offset 10..     length field: one byte below 0x80, or 0x81/0x82 prefix
//	then            0x30 security byte, 4-byte big-endian frame counter,
//	                ciphertext, 12-byte GCM tag
const (
	dlmsStartByte     = 0xDB
	dlmsSecurityByte  = 0x30
	dlmsSystemTitleLn = 8
	dlmsFrameCountLn  = 4
	dlmsGCMTagLn      = 12
	dlmsKeyLn         = 16
	// dlmsMinBodyLen is the smallest valid length field value: security byte,
	// frame counter and GCM tag with an empty ciphertext.
	dlmsMinBodyLen = 1 + dlmsFrameCountLn + dlmsGCMTagLn

	dlmsLengthOneByte  = 0x81
	dlmsLengthTwoBytes = 0x82
	dlmsShortFormMax   = 0x80
	// dlmsHeaderLen is the 0xDB tag plus the system title length byte.
	dlmsHeaderLen = 2
	// dlmsLengthWidth is the size of the two-byte long-form length field.
	dlmsLengthWidth = 2
)

// dlmsDecryptor decrypts DLMS general-glo-ciphering frames (AES-128-GCM)
// into plaintext DSMR telegrams.
type dlmsDecryptor struct {
	aead    cipher.AEAD
	authKey []byte
}

// newDLMSDecryptor builds a decryptor from the hex-encoded 128-bit
// decryption key (GUEK) and authentication key (GAK/AK).
func newDLMSDecryptor(decryptionKeyHex, authenticationKeyHex string) (*dlmsDecryptor, error) {
	key, err := decodeDLMSKey("decryption key", decryptionKeyHex)
	if err != nil {
		return nil, err
	}
	authKey, err := decodeDLMSKey("authentication key", authenticationKeyHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init AES cipher: %w", err)
	}
	aead, err := cipher.NewGCMWithTagSize(block, dlmsGCMTagLn)
	if err != nil {
		return nil, fmt.Errorf("init GCM: %w", err)
	}
	return &dlmsDecryptor{aead: aead, authKey: authKey}, nil
}

func decodeDLMSKey(name, hexKey string) ([]byte, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if len(key) != dlmsKeyLn {
		return nil, fmt.Errorf("%s must be %d bytes (32 hex characters), got %d bytes", name, dlmsKeyLn, len(key))
	}
	return key, nil
}

// readFrame consumes one encrypted frame from r, starting at the 0xDB tag,
// and returns the decrypted plaintext telegram. Any framing or decryption
// problem returns an error; the caller resynchronises on the next start
// marker.
func (d *dlmsDecryptor) readFrame(r *bufio.Reader) (string, error) {
	header := make([]byte, dlmsHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", fmt.Errorf("read frame header: %w", err)
	}
	if header[0] != dlmsStartByte {
		return "", fmt.Errorf("expected frame tag 0xDB, got 0x%02X", header[0])
	}
	if header[1] != dlmsSystemTitleLn {
		return "", fmt.Errorf("unexpected system title length %d", header[1])
	}
	systemTitle := make([]byte, dlmsSystemTitleLn)
	if _, err := io.ReadFull(r, systemTitle); err != nil {
		return "", fmt.Errorf("read system title: %w", err)
	}
	length, err := readDLMSLength(r)
	if err != nil {
		return "", err
	}
	if length < dlmsMinBodyLen || length > maxTelegramBytes {
		return "", fmt.Errorf("frame length %d out of range", length)
	}
	body := make([]byte, length)
	if _, err = io.ReadFull(r, body); err != nil {
		return "", fmt.Errorf("read frame body: %w", err)
	}
	if body[0] != dlmsSecurityByte {
		return "", fmt.Errorf("unexpected security byte 0x%02X", body[0])
	}

	// IV is the system title followed by the frame counter; AAD is the
	// security byte followed by the authentication key.
	iv := append(append(make([]byte, 0, dlmsSystemTitleLn+dlmsFrameCountLn), systemTitle...),
		body[1:1+dlmsFrameCountLn]...)
	aad := append([]byte{dlmsSecurityByte}, d.authKey...)
	plain, err := d.aead.Open(nil, iv, body[1+dlmsFrameCountLn:], aad)
	if err != nil {
		return "", fmt.Errorf("decrypt frame (check the decryption and authentication keys): %w", err)
	}
	return string(plain), nil
}

// readDLMSLength reads a BER-style length: a single byte below 0x80, or an
// 0x81/0x82 prefix followed by one or two big-endian length bytes.
func readDLMSLength(r *bufio.Reader) (int, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("read length: %w", err)
	}
	switch {
	case first < dlmsShortFormMax:
		return int(first), nil
	case first == dlmsLengthOneByte:
		b, readErr := r.ReadByte()
		if readErr != nil {
			return 0, fmt.Errorf("read length: %w", readErr)
		}
		return int(b), nil
	case first == dlmsLengthTwoBytes:
		buf := make([]byte, dlmsLengthWidth)
		if _, readErr := io.ReadFull(r, buf); readErr != nil {
			return 0, fmt.Errorf("read length: %w", readErr)
		}
		return int(binary.BigEndian.Uint16(buf)), nil
	default:
		return 0, fmt.Errorf("unsupported length form 0x%02X", first)
	}
}

// errEncryptedWithoutKey is returned when an encrypted frame arrives but no
// decryption key is configured. This is not recoverable: the stream will
// never contain plaintext telegrams.
var errEncryptedWithoutKey = errors.New(
	"meter sends encrypted DLMS telegrams but Grid.DecryptionKey is not configured; " +
		"request the decryption key from your grid operator and set it in the configuration",
)
