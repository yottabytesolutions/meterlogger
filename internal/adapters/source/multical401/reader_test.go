package multical401

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

// fakePort scripts request/response exchanges without hardware and records
// the operations the reader performs, so tests can assert the asymmetric
// 300-baud-send / 1200-baud-receive sequence. respond maps a request string
// to the raw bytes the port will deliver; a nil return simulates a silent
// meter (read timeout).
type fakePort struct {
	respond func(request string) []byte
	rbuf    []byte
	// events logs mode switches, writes, and flushes in order, for example
	// "setmode:300", "write:/#1", "flush".
	events []string
	baud   int
	closed bool
}

func (p *fakePort) SetMode(mode *serial.Mode) error {
	p.baud = mode.BaudRate
	p.events = append(p.events, "setmode:"+itoa(mode.BaudRate))
	return nil
}

func (p *fakePort) Write(b []byte) (int, error) {
	p.events = append(p.events, "write:"+string(b))
	if resp := p.respond(string(b)); resp != nil {
		p.rbuf = append(p.rbuf, resp...)
	}
	return len(b), nil
}

func (p *fakePort) ResetInputBuffer() error {
	p.events = append(p.events, "flush")
	return nil
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

func itoa(n int) string {
	switch n {
	case requestBaud:
		return "300"
	case responseBaud:
		return "1200"
	}
	return "?"
}

const (
	testSerialLine = "12345678901 0000000\r"
	testDataLine   = captureLine + "\r"
)

// scriptedRespond answers both request types like a healthy meter.
func scriptedRespond(request string) []byte {
	switch request {
	case reqData:
		return []byte(testDataLine)
	case reqSerial:
		return []byte(testSerialLine)
	}
	return nil
}

func testReader(t *testing.T, respond func(request string) []byte) (*Reader, *fakePort) {
	t.Helper()
	port := &fakePort{respond: respond}
	logger := slog.New(slog.DiscardHandler)
	r := newReaderFromPort(context.Background(), "/dev/fake", port, nlDefaults(), logger)
	r.drainDelay = 0
	r.retryDelay = 0
	r.firstByte = 50 * time.Millisecond
	r.interRead = 50 * time.Millisecond
	return r, port
}

func TestReadHeatTelegram_Success(t *testing.T) {
	r, port := testReader(t, scriptedRespond)
	r.fetchSerialNo(context.Background())

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error = %v", err)
	}
	if got.SerialNo != "12345678901" {
		t.Errorf("SerialNo = %q, want 12345678901", got.SerialNo)
	}
	if got.MeterID != "Kamstrup Multical 401/66C (optical)" {
		t.Errorf("MeterID = %q", got.MeterID)
	}
	if got.Joules != 507109000000 {
		t.Errorf("Joules = %d, want 507109000000", got.Joules)
	}
	if got.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}

	// Both exchanges must send at 300 baud, then switch the same port to
	// 1200 baud and flush before reading.
	wantEvents := []string{
		"setmode:300", "write:/#2", "setmode:1200", "flush",
		"setmode:300", "write:/#1", "setmode:1200", "flush",
	}
	if len(port.events) != len(wantEvents) {
		t.Fatalf("port events = %v, want %v", port.events, wantEvents)
	}
	for i, want := range wantEvents {
		if port.events[i] != want {
			t.Fatalf("port events[%d] = %q, want %q (full: %v)", i, port.events[i], want, port.events)
		}
	}
}

func TestReadHeatTelegram_RetriesAfterGarbage(t *testing.T) {
	garbageOnce := true
	r, port := testReader(t, func(request string) []byte {
		if request == reqData && garbageOnce {
			garbageOnce = false
			return []byte("05071@9 not a telegram\r")
		}
		return scriptedRespond(request)
	})

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() after one garbage response error = %v", err)
	}
	if got.Joules != 507109000000 {
		t.Errorf("Joules = %d, want 507109000000", got.Joules)
	}
	writes := 0
	for _, e := range port.events {
		if e == "write:/#1" {
			writes++
		}
	}
	if writes != 2 {
		t.Errorf("data requests = %d, want 2 (one retry)", writes)
	}
}

func TestReadHeatTelegram_SilentMeterTimesOut(t *testing.T) {
	r, port := testReader(t, func(string) []byte { return nil })

	_, err := r.ReadHeatTelegram(context.Background())
	if !errors.Is(err, errReadTimeout) {
		t.Fatalf("ReadHeatTelegram() error = %v, want %v", err, errReadTimeout)
	}
	writes := 0
	for _, e := range port.events {
		if strings.HasPrefix(e, "write:") {
			writes++
		}
	}
	if writes != r.attempts {
		t.Errorf("attempts = %d, want %d", writes, r.attempts)
	}
}

func TestReadHeatTelegram_SerialRequestFailureTolerated(t *testing.T) {
	// One field-tested 401 ignored some request types; a dead "/#2" must
	// not break data reads.
	r, _ := testReader(t, func(request string) []byte {
		if request == reqSerial {
			return nil
		}
		return scriptedRespond(request)
	})
	r.fetchSerialNo(context.Background())

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error = %v", err)
	}
	if got.SerialNo != "" {
		t.Errorf("SerialNo = %q, want empty after failed serial request", got.SerialNo)
	}
	if got.Joules != 507109000000 {
		t.Errorf("Joules = %d, want 507109000000", got.Joules)
	}
}

func TestReadHeatTelegram_NonzeroInfoCodeStillReturns(t *testing.T) {
	line := strings.Replace(testDataLine, " 0000000\r", " 0000016\r", 1)
	r, _ := testReader(t, func(request string) []byte {
		if request == reqData {
			return []byte(line)
		}
		return nil
	})

	got, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() with info code error = %v", err)
	}
	if got.Joules != 507109000000 {
		t.Errorf("Joules = %d, want 507109000000", got.Joules)
	}
}

func TestReadHeatTelegram_ContextCancelled(t *testing.T) {
	r, _ := testReader(t, scriptedRespond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := r.ReadHeatTelegram(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadHeatTelegram() error = %v, want context.Canceled", err)
	}
}

func TestReadHeatTelegram_OverlongResponse(t *testing.T) {
	r, _ := testReader(t, func(string) []byte {
		return []byte(strings.Repeat("1", maxLineLen+2) + "\r")
	})

	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil || !strings.Contains(err.Error(), "without terminator") {
		t.Fatalf("ReadHeatTelegram() error = %v, want overlong response error", err)
	}
}
