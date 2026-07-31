package gridmeter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewGridReader(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	if reader == nil {
		t.Fatal("NewGridReader() returned nil")
	}
	if reader.usbPort != "/dev/null" {
		t.Errorf("usbPort = %q, want /dev/null", reader.usbPort)
	}
	if reader.Telegrams() == nil {
		t.Error("Telegrams() returned nil channel")
	}
}

// runReader drains the reader's telegram channel while ReadGridTelegrams runs
// and returns the collected telegrams together with the reader's error.
func runReader(t *testing.T, reader *GridReader) ([]domain.GridTelegram, error) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- reader.ReadGridTelegrams(context.Background()) }()
	var got []domain.GridTelegram
	for telegram := range reader.Telegrams() {
		got = append(got, telegram)
	}
	return got, <-errCh
}

func TestCalculateCrc16(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint16
	}{
		{
			name: "empty",
			data: []byte{},
			want: 0x0000,
		},
		{
			name: "single byte 0x31",
			data: []byte{0x31},
			want: 0xD4C1,
		},
		{
			name: "slash and letters",
			data: []byte{0x2F, 0x41, 0x42, 0x43},
			want: 0x9129,
		},
		{
			// Standard CRC-16/ARC check value.
			name: "reference vector 123456789",
			data: []byte("123456789"),
			want: 0xBB3D,
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.name, func(t *testing.T) {
				got := calculateCrc16(tt.data)
				if got != tt.want {
					t.Errorf("calculateCrc16() = %04X, want %04X", got, tt.want)
				}
			},
		)
	}
}

func TestCalculateCrc16_Consistency(t *testing.T) {
	data := []byte("hello world")
	crc := calculateCrc16(data)
	if crc != 0x39C1 {
		t.Errorf("calculateCrc16(%q) = %04X, want 39C1", data, crc)
	}
}

func TestIsValidChecksum_NoExclamation(t *testing.T) {
	if isValidChecksum("no exclamation mark here") {
		t.Error("isValidChecksum should return false when no '!' found")
	}
}

func TestIsValidChecksum_TooShort(t *testing.T) {
	// '!' at end without 4 more chars
	if isValidChecksum("abc!AB") {
		t.Error("isValidChecksum should return false when not enough chars after '!'")
	}
}

func TestIsValidChecksum_InvalidHexChecksum(t *testing.T) {
	if isValidChecksum("message!ZZZZ") {
		t.Error("isValidChecksum should return false for non-hex checksum")
	}
}

func TestIsValidChecksum_ValidMessage(t *testing.T) {
	// Build a real message with correct checksum
	body := "/ISk5\\2MT382-1000\r\n\r\n"
	// Compute checksum for body + "!"
	msgBody := body + "!"
	crc := calculateCrc16([]byte(msgBody))
	fullMsg := msgBody + strings.ToUpper(crcToHex(crc))
	if !isValidChecksum(fullMsg) {
		t.Errorf("isValidChecksum should return true for correctly checksummed message")
	}
}

func TestIsValidChecksum_WrongChecksum(t *testing.T) {
	// The correct checksum for "/test\r\n!" is EA36, so 0000 must be rejected.
	if isValidChecksum("/test\r\n!0000") {
		t.Error("isValidChecksum should return false for a wrong checksum")
	}
	if !isValidChecksum("/test\r\n!EA36") {
		t.Error("isValidChecksum should return true for the correct checksum")
	}
}

// crcToHex converts uint16 to 4-char hex string.
func crcToHex(crc uint16) string {
	chars := "0123456789ABCDEF"
	return string(
		[]byte{
			chars[(crc>>12)&0xF],
			chars[(crc>>8)&0xF],
			chars[(crc>>4)&0xF],
			chars[crc&0xF],
		},
	)
}

const sampleTelegram = `/ISk5\2MT382-1000

1-3:0.2.8(50)
0-0:1.0.0(191130210919W)
0-0:96.1.1(4530303334303037343337383430323139)
1-0:1.8.1(000239.922*kWh)
1-0:1.8.2(000239.621*kWh)
1-0:2.8.1(000003.448*kWh)
1-0:2.8.2(000000.000*kWh)
0-0:96.14.0(0001)
1-0:1.7.0(00.577*kW)
1-0:2.7.0(00.000*kW)
0-0:96.7.21(00009)
0-0:96.7.9(00010)
1-0:32.32.0(00000)
1-0:52.32.0(00000)
1-0:72.32.0(00000)
1-0:32.36.0(00001)
1-0:52.36.0(00001)
1-0:72.36.0(00001)
0-0:96.13.0()
1-0:32.7.0(227.4*V)
1-0:52.7.0(227.2*V)
1-0:72.7.0(228.2*V)
1-0:31.7.0(001*A)
1-0:51.7.0(000*A)
1-0:71.7.0(001*A)
1-0:21.7.0(00.298*kW)
1-0:41.7.0(00.054*kW)
1-0:61.7.0(00.223*kW)
1-0:22.7.0(00.000*kW)
1-0:42.7.0(00.000*kW)
1-0:62.7.0(00.000*kW)
!`

func TestParseTelegram_Valid(t *testing.T) {
	telegram, err := parseTelegram(sampleTelegram)
	if err != nil {
		t.Fatalf("parseTelegram() error: %v", err)
	}

	if telegram.UsageCounter1 != 239.922 {
		t.Errorf("UsageCounter1 = %v, want 239.922", telegram.UsageCounter1)
	}
	if telegram.UsageCounter2 != 239.621 {
		t.Errorf("UsageCounter2 = %v, want 239.621", telegram.UsageCounter2)
	}
	if telegram.OutputCounter1 != 3.448 {
		t.Errorf("OutputCounter1 = %v, want 3.448", telegram.OutputCounter1)
	}
	if telegram.OutputCounter2 != 0.0 {
		t.Errorf("OutputCounter2 = %v, want 0.0", telegram.OutputCounter2)
	}
	if telegram.TotalPowerUsage != 577 {
		t.Errorf("TotalPowerUsage = %v, want 577 (0.577*1000)", telegram.TotalPowerUsage)
	}
	if telegram.TotalPowerOutput != 0 {
		t.Errorf("TotalPowerOutput = %v, want 0", telegram.TotalPowerOutput)
	}
	if telegram.VoltageP1 != 227.4 {
		t.Errorf("VoltageP1 = %v, want 227.4", telegram.VoltageP1)
	}
	if telegram.CurrentP1 != 1 {
		t.Errorf("CurrentP1 = %v, want 1", telegram.CurrentP1)
	}
	if telegram.BrownoutsP1 != 0 {
		t.Errorf("BrownoutsP1 = %v, want 0", telegram.BrownoutsP1)
	}
	if telegram.SpikesP1 != 1 {
		t.Errorf("SpikesP1 = %v, want 1", telegram.SpikesP1)
	}
}

func TestParseTelegram_MissingTimestamp(t *testing.T) {
	msg := "/test\r\n1-0:1.8.1(000239.922*kWh)\r\n!"
	_, err := parseTelegram(msg)
	if err == nil {
		t.Error("parseTelegram should return error when timestamp is missing")
	}
}

func TestParseTelegram_MissingRequiredField(t *testing.T) {
	// Has timestamp but missing usage counters
	msg := `/test

0-0:1.0.0(191130210919W)
!`
	_, err := parseTelegram(msg)
	if err == nil {
		t.Error("parseTelegram should return error when required fields are missing")
	}
}

func TestParseTelegram_PowerConversion(t *testing.T) {
	telegram, err := parseTelegram(sampleTelegram)
	if err != nil {
		t.Fatalf("parseTelegram() error: %v", err)
	}
	// 0.298 kW * 1000 = 298
	if telegram.PowerUsageP1 != 298 {
		t.Errorf("PowerUsageP1 = %v, want 298", telegram.PowerUsageP1)
	}
}

func TestParseTelegram_InvalidTimestampTooShort(t *testing.T) {
	// Timestamp value shorter than 12 chars
	msg := "/test\r\n0-0:1.0.0(19113)\r\n!"
	_, err := parseTelegram(msg)
	if err == nil {
		t.Error("parseTelegram should return error when timestamp too short")
	}
}

func TestParseTelegram_InvalidTimestampFormat(t *testing.T) {
	// Timestamp present but not parseable
	msg := "/test\r\n0-0:1.0.0(NOTADATE0000)\r\n!"
	_, err := parseTelegram(msg)
	if err == nil {
		t.Error("parseTelegram should return error for invalid timestamp format")
	}
}

// fullTelegram builds a complete telegram with the correct CRC for ReadGridTelegrams tests.
//
//nolint:gochecknoglobals // test data used across multiple test functions
var fullTelegram = "/ISk5\\2MT382-1000\r\n" +
	"\r\n" +
	"1-3:0.2.8(50)\r\n" +
	"0-0:1.0.0(191130210919W)\r\n" +
	"0-0:96.1.1(4530303334303037343337383430323139)\r\n" +
	"1-0:1.8.1(000239.922*kWh)\r\n" +
	"1-0:1.8.2(000239.621*kWh)\r\n" +
	"1-0:2.8.1(000003.448*kWh)\r\n" +
	"1-0:2.8.2(000000.000*kWh)\r\n" +
	"0-0:96.14.0(0001)\r\n" +
	"1-0:1.7.0(00.577*kW)\r\n" +
	"1-0:2.7.0(00.000*kW)\r\n" +
	"0-0:96.7.21(00009)\r\n" +
	"0-0:96.7.9(00010)\r\n" +
	"1-0:32.32.0(00000)\r\n" +
	"1-0:52.32.0(00000)\r\n" +
	"1-0:72.32.0(00000)\r\n" +
	"1-0:32.36.0(00001)\r\n" +
	"1-0:52.36.0(00001)\r\n" +
	"1-0:72.36.0(00001)\r\n" +
	"0-0:96.13.0()\r\n" +
	"1-0:32.7.0(227.4*V)\r\n" +
	"1-0:52.7.0(227.2*V)\r\n" +
	"1-0:72.7.0(228.2*V)\r\n" +
	"1-0:31.7.0(001*A)\r\n" +
	"1-0:51.7.0(000*A)\r\n" +
	"1-0:71.7.0(001*A)\r\n" +
	"1-0:21.7.0(00.298*kW)\r\n" +
	"1-0:41.7.0(00.054*kW)\r\n" +
	"1-0:61.7.0(00.223*kW)\r\n" +
	"1-0:22.7.0(00.000*kW)\r\n" +
	"1-0:42.7.0(00.000*kW)\r\n" +
	"1-0:62.7.0(00.000*kW)\r\n" +
	"!4ECE\r\n"

func TestReadGridTelegrams_ValidTelegram(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(fullTelegram)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	if got[0].UsageCounter1 != 239.922 {
		t.Errorf("UsageCounter1 = %v, want 239.922", got[0].UsageCounter1)
	}
}

func TestReadGridTelegrams_InvalidChecksum(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	// Same telegram but wrong CRC
	invalidTelegram := strings.Replace(fullTelegram, "!4ECE\r\n", "!0000\r\n", 1)
	reader.portReader = strings.NewReader(invalidTelegram)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadGridTelegrams() produced %d telegrams for invalid checksum, want 0", len(got))
	}
}

func TestReadGridTelegrams_ParseError(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())

	// A telegram with valid checksum but missing required fields
	badMsg := "/test\r\n0-0:1.0.0(191130210919W)\r\n!"
	crc := calculateCrc16([]byte(badMsg))
	chars := "0123456789ABCDEF"
	crcStr := string([]byte{chars[(crc>>12)&0xF], chars[(crc>>8)&0xF], chars[(crc>>4)&0xF], chars[crc&0xF]})
	badTelegram := badMsg + crcStr + "\r\n"

	reader.portReader = strings.NewReader(badTelegram)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadGridTelegrams() produced %d telegrams when parse fails, want 0", len(got))
	}
}

func TestReadGridTelegrams_ReadError(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	// errReader returns a line, then an error, then EOF
	reader.portReader = &errorThenEOFReader{line: "/ISk5\\2MT\r\n"}

	_, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
}

func TestReadGridTelegrams_EOFReturnsError(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader("")

	_, err := runReader(t, reader)
	if err == nil {
		t.Fatal("ReadGridTelegrams() should return an error when the stream ends")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
}

func TestReadGridTelegrams_ContextCancelled(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(fullTelegram)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := reader.ReadGridTelegrams(ctx); err != nil {
		t.Fatalf("ReadGridTelegrams() error = %v, want nil on context cancellation", err)
	}
	if _, open := <-reader.Telegrams(); open {
		t.Error("Telegrams() channel should be closed after ReadGridTelegrams returns")
	}
}

func TestReadGridTelegrams_TelegramSizeCap(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())

	// A start marker followed by more than 64 KiB of lines without an end
	// marker, then a valid telegram. The oversized partial must be dropped
	// and the valid telegram must still come through.
	junkLine := strings.Repeat("A", 1024) + "\r\n"
	oversized := "/stuck\r\n" + strings.Repeat(junkLine, 70)
	reader.portReader = strings.NewReader(oversized + fullTelegram)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams after oversized partial, want 1", len(got))
	}
	if got[0].UsageCounter1 != 239.922 {
		t.Errorf("UsageCounter1 = %v, want 239.922", got[0].UsageCounter1)
	}
}

// errorThenEOFReader returns one line, then a non-EOF error, then EOF.
type errorThenEOFReader struct {
	line  string
	phase int
}

func (r *errorThenEOFReader) Read(p []byte) (int, error) {
	switch r.phase {
	case 0:
		r.phase++
		n := copy(p, r.line)
		return n, nil
	case 1:
		r.phase++
		return 0, errors.New("simulated read error")
	default:
		return 0, io.EOF
	}
}
