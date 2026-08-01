package kamstrup

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakePort scripts request/response exchanges without hardware. respond maps
// a decoded request payload to the raw bytes the port will deliver; a nil
// return simulates a silent meter (read timeout).
type fakePort struct {
	t       *testing.T
	respond func(payload []byte) []byte
	rbuf    []byte
	writes  [][]byte
	closed  bool
}

func (p *fakePort) Write(b []byte) (int, error) {
	p.writes = append(p.writes, slices.Clone(b))
	payload, err := decodeFrame(b, startRequest)
	if err != nil {
		p.t.Fatalf("reader wrote an invalid request frame % X: %v", b, err)
	}
	if resp := p.respond(payload); resp != nil {
		p.rbuf = append(p.rbuf, resp...)
	}
	return len(b), nil
}

// Read delivers buffered bytes, or (0, nil) like go.bug.st/serial does on a
// read timeout.
func (p *fakePort) Read(b []byte) (int, error) {
	if len(p.rbuf) == 0 {
		return 0, nil
	}
	n := copy(b, p.rbuf)
	p.rbuf = p.rbuf[n:]
	return n, nil
}

func (p *fakePort) Close() error                       { p.closed = true; return nil }
func (p *fakePort) SetReadTimeout(time.Duration) error { return nil }

func testReader(t *testing.T, respond func(payload []byte) []byte) (*Reader, *fakePort) {
	t.Helper()
	port := &fakePort{t: t, respond: respond}
	logger := slog.New(slog.DiscardHandler)
	r := newReaderFromPort(context.Background(), "/dev/fake", port, logger)
	r.timeout = 50 * time.Millisecond
	return r, port
}

// regBlockResponse builds a GetRegister response payload for one register.
func regBlockResponse(id uint16, unit byte, siEx byte, mantissa ...byte) []byte {
	p := binary.BigEndian.AppendUint16([]byte{destHeatMeter, cmdGetRegister}, id)
	p = append(p, unit, byte(len(mantissa)), siEx) //nolint:gosec // G115: test mantissas fit a byte
	return append(p, mantissa...)
}

// happyResponses scripts a full meter: serial 12345678, type 0x0217, and all
// ten registers present with a mix of units.
func happyResponses() map[uint16][]byte {
	return map[uint16][]byte{
		60:   regBlockResponse(60, 8, 0x42, 0x00, 0x01, 0xE2, 0x40), // 1234.56 GJ
		68:   regBlockResponse(68, 39, 0x00, 0x01, 0xE2, 0x40),      // 123456 l
		86:   regBlockResponse(86, 37, 0x42, 0x19, 0x84),            // 65.32 C
		87:   regBlockResponse(87, 37, 0x42, 0x0F, 0xB5),            // 40.21 C
		89:   regBlockResponse(89, 38, 0x42, 0x09, 0xCF),            // 25.11 K
		74:   regBlockResponse(74, 41, 0x00, 0x03, 0x52),            // 850 l/h
		80:   regBlockResponse(80, 22, 0x42, 0x04, 0xD2),            // 12.34 kW
		124:  regBlockResponse(124, 41, 0x00, 0x04, 0xB0),           // 1200 l/h max
		128:  regBlockResponse(128, 22, 0x42, 0x08, 0xAE),           // 22.22 kW max
		1004: regBlockResponse(1004, 46, 0x00, 0x13, 0x88),          // 5000 h
	}
}

// scriptedRespond answers identity commands and serves register responses
// from regs. Registers missing from regs get an empty GetRegister echo, which
// is how the meter reports an unsupported register.
func scriptedRespond(regs map[uint16][]byte) func(payload []byte) []byte {
	return func(payload []byte) []byte {
		switch payload[1] {
		case cmdGetSerialNo:
			// Serial 12345678 = 0x00BC614E.
			return encodeFrame(startResponse, []byte{destHeatMeter, cmdGetSerialNo, 0x00, 0xBC, 0x61, 0x4E})
		case cmdGetType:
			return encodeFrame(startResponse, []byte{destHeatMeter, cmdGetType, 0x02, 0x17, 0x01, 0x08})
		case cmdGetRegister:
			id := uint16(payload[3])<<8 | uint16(payload[4])
			resp, ok := regs[id]
			if !ok {
				return encodeFrame(startResponse, []byte{destHeatMeter, cmdGetRegister})
			}
			return encodeFrame(startResponse, resp)
		}
		return nil
	}
}

func TestReadHeatTelegram_FullMeter(t *testing.T) {
	r, _ := testReader(t, scriptedRespond(happyResponses()))

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error = %v", err)
	}

	if got.SerialNo != "12345678" {
		t.Errorf("SerialNo = %q, want 12345678", got.SerialNo)
	}
	if got.MeterID != "Kamstrup (type 0217)" {
		t.Errorf("MeterID = %q", got.MeterID)
	}
	if got.Joules != 1234560000000 {
		t.Errorf("Joules = %d, want 1234560000000", got.Joules)
	}
	if math.Abs(got.VolumeCm3-123.456) > 1e-9 {
		t.Errorf("VolumeCm3 = %v, want 123.456", got.VolumeCm3)
	}
	if math.Abs(got.Tforward-65.32) > 1e-9 {
		t.Errorf("Tforward = %v, want 65.32", got.Tforward)
	}
	if math.Abs(got.Treturn-40.21) > 1e-9 {
		t.Errorf("Treturn = %v, want 40.21", got.Treturn)
	}
	if math.Abs(got.Tdiff-25.11) > 1e-9 {
		t.Errorf("Tdiff = %v, want 25.11", got.Tdiff)
	}
	if math.Abs(got.ActualFlow-0.85) > 1e-9 {
		t.Errorf("ActualFlow = %v, want 0.85", got.ActualFlow)
	}
	if got.ActualPower != 12340 {
		t.Errorf("ActualPower = %d, want 12340", got.ActualPower)
	}
	if got.SecondsCounter != 18000000 {
		t.Errorf("SecondsCounter = %d, want 18000000", got.SecondsCounter)
	}
	if math.Abs(got.MaxFlow-1.2) > 1e-9 {
		t.Errorf("MaxFlow = %v, want 1.2", got.MaxFlow)
	}
	if got.MaxPower != 22220 {
		t.Errorf("MaxPower = %d, want 22220", got.MaxPower)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
}

// TestReadHeatTelegram_HardcodedFrames feeds raw response frames with CRC
// bytes computed by an independent implementation, so the reader's happy path
// does not depend on this package's encoder.
func TestReadHeatTelegram_HardcodedFrames(t *testing.T) {
	rawFrames := map[byte][]byte{
		cmdGetSerialNo: {0x40, 0x3F, 0x02, 0x00, 0xBC, 0x61, 0x4E, 0xB4, 0x83, 0x0D},
		cmdGetType:     {0x40, 0x3F, 0x01, 0x02, 0x17, 0x01, 0x02, 0x78, 0x36, 0x0D},
	}
	// Register 60: 1234.56 GJ, with the stuffed 0x40 mantissa byte.
	reg60 := []byte{
		0x40, 0x3F, 0x10, 0x00, 0x3C, 0x08, 0x04, 0x42, 0x00, 0x01, 0xE2,
		0x1B, 0xBF, 0x7E, 0x4A, 0x0D,
	}
	regs := happyResponses()
	r, _ := testReader(t, func(payload []byte) []byte {
		if raw, ok := rawFrames[payload[1]]; ok {
			return raw
		}
		id := uint16(payload[3])<<8 | uint16(payload[4])
		if id == 60 {
			return reg60
		}
		return encodeFrame(startResponse, regs[id])
	})

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error = %v", err)
	}
	if got.SerialNo != "12345678" {
		t.Errorf("SerialNo = %q, want 12345678", got.SerialNo)
	}
	if got.Joules != 1234560000000 {
		t.Errorf("Joules = %d, want 1234560000000", got.Joules)
	}
}

func TestReadHeatTelegram_CachesIdentity(t *testing.T) {
	r, port := testReader(t, scriptedRespond(happyResponses()))

	for range 2 {
		if _, err := r.ReadHeatTelegram(context.Background()); err != nil {
			t.Fatalf("ReadHeatTelegram() error = %v", err)
		}
	}

	identityRequests := 0
	for _, w := range port.writes {
		payload, err := decodeFrame(w, startRequest)
		if err != nil {
			t.Fatalf("decode written frame: %v", err)
		}
		if payload[1] == cmdGetSerialNo || payload[1] == cmdGetType {
			identityRequests++
		}
	}
	if identityRequests != 2 {
		t.Errorf("identity requests across two reads = %d, want 2 (cached after first)", identityRequests)
	}
}

func TestReadHeatTelegram_MissingRegisterLeavesFieldZero(t *testing.T) {
	regs := happyResponses()
	delete(regs, 1004) // meter without an hour counter register
	r, _ := testReader(t, scriptedRespond(regs))

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error = %v", err)
	}
	if got.SecondsCounter != 0 {
		t.Errorf("SecondsCounter = %d, want 0 for a missing register", got.SecondsCounter)
	}
	if got.Joules == 0 {
		t.Error("Joules = 0, other registers should still be read")
	}
}

func TestReadHeatTelegram_UnknownUnitCodeFails(t *testing.T) {
	regs := happyResponses()
	regs[80] = regBlockResponse(80, 200, 0x00, 0x01)
	r, _ := testReader(t, scriptedRespond(regs))

	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown unit code") {
		t.Fatalf("ReadHeatTelegram() error = %v, want unknown unit code", err)
	}
}

func TestReadHeatTelegram_WrongDimensionFails(t *testing.T) {
	regs := happyResponses()
	regs[86] = regBlockResponse(86, 2, 0x00, 0x01) // kWh on a temperature register
	r, _ := testReader(t, scriptedRespond(regs))

	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil || !strings.Contains(err.Error(), "does not fit register dimension") {
		t.Fatalf("ReadHeatTelegram() error = %v, want dimension mismatch", err)
	}
}

func TestReadHeatTelegram_GarbageBeforeStartByte(t *testing.T) {
	base := scriptedRespond(happyResponses())
	r, _ := testReader(t, func(payload []byte) []byte {
		return append([]byte{0x00, 0xFF, 0x12}, base(payload)...)
	})

	if _, err := r.ReadHeatTelegram(context.Background()); err != nil {
		t.Fatalf("ReadHeatTelegram() with leading garbage error = %v", err)
	}
}

func TestReadHeatTelegram_RetriesAfterCorruptFrame(t *testing.T) {
	base := scriptedRespond(happyResponses())
	corruptOnce := true
	r, _ := testReader(t, func(payload []byte) []byte {
		resp := base(payload)
		if corruptOnce {
			corruptOnce = false
			resp = slices.Clone(resp)
			resp[1] ^= 0x01 // breaks the CRC of the first response
		}
		return resp
	})

	if _, err := r.ReadHeatTelegram(context.Background()); err != nil {
		t.Fatalf("ReadHeatTelegram() after one corrupt frame error = %v", err)
	}
}

func TestReadHeatTelegram_SilentMeterTimesOut(t *testing.T) {
	r, port := testReader(t, func([]byte) []byte { return nil })

	_, err := r.ReadHeatTelegram(context.Background())
	if !errors.Is(err, errReadTimeout) {
		t.Fatalf("ReadHeatTelegram() error = %v, want %v", err, errReadTimeout)
	}
	if len(port.writes) != r.attempts {
		t.Errorf("attempts = %d, want %d", len(port.writes), r.attempts)
	}
}

func TestReadHeatTelegram_ContextCancelled(t *testing.T) {
	r, _ := testReader(t, scriptedRespond(happyResponses()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := r.ReadHeatTelegram(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadHeatTelegram() error = %v, want context.Canceled", err)
	}
}

func TestReadHeatTelegram_NegativeValue(t *testing.T) {
	regs := happyResponses()
	regs[89] = regBlockResponse(89, 38, 0xC2, 0x09, 0xCF) // -25.11 K
	r, _ := testReader(t, scriptedRespond(regs))

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error = %v", err)
	}
	if math.Abs(got.Tdiff-(-25.11)) > 1e-9 {
		t.Errorf("Tdiff = %v, want -25.11", got.Tdiff)
	}
}

func TestReadHeatTelegram_MalformedRegisterBlocks(t *testing.T) {
	tests := []struct {
		name  string
		block []byte
	}{
		{"wrong register id echoed", regBlockResponse(61, 8, 0x00, 0x01)},
		{"length byte exceeds block", []byte{destHeatMeter, cmdGetRegister, 0x00, 0x3C, 0x08, 0x09, 0x00, 0x01}},
		{"header too short", []byte{destHeatMeter, cmdGetRegister, 0x00, 0x3C}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regs := happyResponses()
			regs[60] = tt.block
			r, _ := testReader(t, scriptedRespond(regs))
			if _, err := r.ReadHeatTelegram(context.Background()); err == nil {
				t.Error("ReadHeatTelegram() succeeded, want error")
			}
		})
	}
}
