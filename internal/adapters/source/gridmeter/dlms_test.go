package gridmeter

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// smartyKey is the decryption key for the public NEXXTLAB go-smarty-reader
// test vector. It works only for that captured frame, not for any real meter.
const smartyKey = "D491470F47126332B07D1923B3504188" // gitleaks:allow published NEXXTLAB test vector key

// luxembourgAuthKey is the fixed authentication key shared by all
// Luxembourgish Smarty meters.
const luxembourgAuthKey = "00112233445566778899AABBCCDDEEFF" // gitleaks:allow public spec constant

// smartyFrameHex is the real 647-byte encrypted DLMS frame from the
// go-smarty-reader test suite: system title 5341476770 01BD54, two-byte
// length form (0x82 0x027A), frame counter 0x0005A8E3.
const smartyFrameHex = "db08534147677001bd5482027a300005a8e3806ee6e639274c7bc57095f872b0" +
	"8dde621fb74ee81e5ebe342c93d8e73781fb2a1eb8710074a54fc57aa7d1d992" +
	"36c42e2ec01a0324efc7f02e3ba2fa43196ca603838ab8322dffa83f7e83932e" +
	"6007406b112b41745f38805646d53fea7a029fa79c36d9d777da8b5a03ed3ec6" +
	"e485dec0c98d7d53c0bd175b228e01f466d9843777808503d630158c8236d868" +
	"088eb13977bdfc474367e557136feb6d537f18304484a014b49099a63c18d430" +
	"3a4c53853a0ca8c5b0e3e805bfd8425410fab522b35a8f258b57bb1e97e2489b" +
	"6f37071753affbb4eebca502eea592b88085ae155bcea8f732d6b0115bb9ced7" +
	"6be5b893b2a7efc8163c171e77c3d29a72b7478ed6dfe2c48bf3f9d8f895ca1c" +
	"b42eaac4cb21d6eab51e77e4d6029d788427dd5bfc46bdd3e8a32dbb6f93b484" +
	"2b073e9b6fe6e5dfc058b5f454aa3ef162bdf93cb1e0c452dbb2be4cb4c67d16" +
	"9e2a30618fa2165441b6d3c22f1c3627e35fffcf9f1947d7a6ae942fc21e246f" +
	"0efc456a7889c961c93ea089eef6d1a44056baaab352cbea7d7e206757af0d42" +
	"7064b058d57235a78f8db91ce4d60a3c6babd99a61169b172f2414f565bde023" +
	"9654eac77552fbcc2a2fa102d7cd003ddbeb9c1f811dbc695376336e2e9d6cfc" +
	"73fcb4a29446bd7ce3170e5856766b25e82049ee5508ec1816685f1dcec23819" +
	"1216dffba0647d32a1559d99640a15f24ab02172db3d564454e7a0346c099399" +
	"b8770a9bdaa619c7c5230526d6b80499a0ee0cd4fc46a677af68ef4dee9e76b7" +
	"8ac8a04f431db3986fcfd45ee1f10799af0b794a4103735d6dda3829173f80f1" +
	"38a5301afdf1db8774f0c40e6e71264b334a943fc10552b0752f3a0d7fd80458" +
	"595408f2850f17"

func smartyFrame(t *testing.T) []byte {
	t.Helper()
	frame, err := hex.DecodeString(smartyFrameHex)
	if err != nil {
		t.Fatalf("decode frame hex: %v", err)
	}
	return frame
}

func encryptedReader(t *testing.T, keyHex string, src io.Reader) *GridReader {
	t.Helper()
	reader, err := NewGridReader("/dev/null", testLogger()).WithDecryption(keyHex, luxembourgAuthKey)
	if err != nil {
		t.Fatalf("WithDecryption: %v", err)
	}
	reader.portReader = src
	return reader
}

func TestReadGridTelegrams_EncryptedSmartyVector(t *testing.T) {
	reader := encryptedReader(t, smartyKey, bytes.NewReader(smartyFrame(t)))

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	tel := got[0]
	if tel.Serienummer != "53414731303330373030313134303034" {
		t.Errorf("Serienummer = %q, want the decrypted 0-0:42.0.0 value", tel.Serienummer)
	}
	if tel.UsageCounter1 != 6.695 {
		t.Errorf("UsageCounter1 = %v, want 6.695", tel.UsageCounter1)
	}
	if tel.OutputCounter1 != 0.025 {
		t.Errorf("OutputCounter1 = %v, want 0.025", tel.OutputCounter1)
	}
	wantTime := time.Date(2018, 1, 30, 10, 21, 22, 0, time.FixedZone("CET", 60*60))
	if !tel.Time.Equal(wantTime) {
		t.Errorf("Time = %v, want %v", tel.Time, wantTime)
	}
	if reader.badFrames != 0 {
		t.Errorf("badFrames = %d, want 0", reader.badFrames)
	}
}

func TestReadGridTelegrams_EncryptedWrongKey(t *testing.T) {
	wrongKey := "D491470F47126332B07D1923B3504100"
	reader := encryptedReader(t, wrongKey, bytes.NewReader(smartyFrame(t)))

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF after resync", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d telegrams with the wrong key, want 0", len(got))
	}
	if reader.badFrames != 1 {
		t.Errorf("badFrames = %d, want 1", reader.badFrames)
	}
}

func TestReadGridTelegrams_EncryptedWithoutKeyFails(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = bytes.NewReader(smartyFrame(t))

	_, err := runReader(t, reader)
	if !errors.Is(err, errEncryptedWithoutKey) {
		t.Fatalf("ReadGridTelegrams() error = %v, want errEncryptedWithoutKey", err)
	}
}

// buildEncryptedFrame encrypts a plaintext telegram into a DLMS
// general-glo-ciphering frame. Lengths below 128 use the short form.
func buildEncryptedFrame(t *testing.T, keyHex, akHex string, systemTitle []byte, fc uint32, plaintext string) []byte {
	t.Helper()
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	ak, err := hex.DecodeString(akHex)
	if err != nil {
		t.Fatalf("decode auth key: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	aead, err := cipher.NewGCMWithTagSize(block, dlmsGCMTagLn)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	fcBytes := binary.BigEndian.AppendUint32(nil, fc)
	iv := append(append([]byte{}, systemTitle...), fcBytes...)
	aad := append([]byte{dlmsSecurityByte}, ak...)
	sealed := aead.Seal(nil, iv, []byte(plaintext), aad)

	body := append(append([]byte{dlmsSecurityByte}, fcBytes...), sealed...)
	frame := append([]byte{dlmsStartByte, dlmsSystemTitleLn}, systemTitle...)
	if len(body) < dlmsShortFormMax {
		frame = append(frame, byte(len(body))) //nolint:gosec // G115: guarded by the dlmsShortFormMax check
	} else {
		frame = append(frame, dlmsLengthTwoBytes)
		frame = binary.BigEndian.AppendUint16(frame, uint16(len(body))) //nolint:gosec // bounded by telegram cap
	}
	return append(frame, body...)
}

func TestReadGridTelegrams_EncryptedRoundTrip(t *testing.T) {
	key := "000102030405060708090A0B0C0D0E0F"
	systemTitle := []byte{0x53, 0x41, 0x47, 0x67, 0x70, 0x01, 0x02, 0x03}
	frame := buildEncryptedFrame(t, key, luxembourgAuthKey, systemTitle, 42, luxembourgTelegram)

	reader := encryptedReader(t, key, bytes.NewReader(frame))
	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	if got[0].UsageCounter1 != 273.764 {
		t.Errorf("UsageCounter1 = %v, want 273.764", got[0].UsageCounter1)
	}
}

// TestReadGridTelegrams_EncryptedShortFormLength round-trips a telegram small
// enough for the single-byte length form.
func TestReadGridTelegrams_EncryptedShortFormLength(t *testing.T) {
	key := "000102030405060708090A0B0C0D0E0F"
	body := "/T\r\n" +
		"0-0:1.0.0(191130210919W)\r\n" +
		"1-0:1.8.0(1*kWh)\r\n" +
		"1-0:2.8.0(2*kWh)\r\n" +
		"1-0:1.7.0(0*kW)\r\n" +
		"1-0:2.7.0(0*kW)\r\n" +
		"!"
	telegram := body + crcToHex(calculateCrc16([]byte(body))) + "\r\n"
	systemTitle := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	frame := buildEncryptedFrame(t, key, luxembourgAuthKey, systemTitle, 7, telegram)
	if frame[10] >= dlmsShortFormMax {
		t.Fatalf("test telegram too large for short-form length, got length byte 0x%02X", frame[10])
	}

	reader := encryptedReader(t, key, bytes.NewReader(frame))
	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	if got[0].UsageCounter1 != 1 || got[0].OutputCounter1 != 2 {
		t.Errorf("counters = %v %v, want 1 2", got[0].UsageCounter1, got[0].OutputCounter1)
	}
}

// TestReadGridTelegrams_TruncatedFrameResync corrupts an encrypted frame so
// that its declared length swallows trailing junk; the reader must count one
// bad frame and still decode the plaintext telegram that follows.
func TestReadGridTelegrams_TruncatedFrameResync(t *testing.T) {
	frame := smartyFrame(t)
	var stream []byte
	stream = append(stream, frame[:len(frame)-20]...)
	stream = append(stream, []byte("XXXXXXXXXXXXXXXXXXXX")...)
	stream = append(stream, []byte(fullTelegram)...)

	reader := encryptedReader(t, smartyKey, bytes.NewReader(stream))
	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if reader.badFrames != 1 {
		t.Errorf("badFrames = %d, want 1", reader.badFrames)
	}
	if len(got) != 1 {
		t.Fatalf("got %d telegrams, want 1 (the plaintext telegram after resync)", len(got))
	}
	if got[0].UsageCounter1 != 239.922 {
		t.Errorf("UsageCounter1 = %v, want 239.922", got[0].UsageCounter1)
	}
}

// TestReadGridTelegrams_FrameCutOffAtEOF ends the stream in the middle of an
// encrypted frame: the truncation is dropped and EOF surfaces.
func TestReadGridTelegrams_FrameCutOffAtEOF(t *testing.T) {
	frame := smartyFrame(t)
	reader := encryptedReader(t, smartyKey, bytes.NewReader(frame[:100]))

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d telegrams from a truncated frame, want 0", len(got))
	}
	if reader.badFrames != 1 {
		t.Errorf("badFrames = %d, want 1", reader.badFrames)
	}
}

func TestWithDecryption_InvalidKeys(t *testing.T) {
	tests := []struct {
		name    string
		decKey  string
		authKey string
	}{
		{name: "non-hex decryption key", decKey: strings.Repeat("Z", 32), authKey: luxembourgAuthKey},
		{name: "short decryption key", decKey: "D491470F", authKey: luxembourgAuthKey},
		{name: "short auth key", decKey: smartyKey, authKey: "0011"},
		{name: "non-hex auth key", decKey: smartyKey, authKey: strings.Repeat("Z", 32)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewGridReader("/dev/null", testLogger()).WithDecryption(tt.decKey, tt.authKey)
			if err == nil {
				t.Error("WithDecryption() expected error, got nil")
			}
		})
	}
}

func TestReadDLMSLength_UnsupportedForm(t *testing.T) {
	reader := encryptedReader(t, smartyKey, bytes.NewReader([]byte{
		dlmsStartByte, dlmsSystemTitleLn,
		1, 2, 3, 4, 5, 6, 7, 8,
		0x84, // four-byte length form is not used by any known meter
	}))
	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 0 || reader.badFrames != 1 {
		t.Errorf("got %d telegrams, badFrames %d; want 0 and 1", len(got), reader.badFrames)
	}
}
