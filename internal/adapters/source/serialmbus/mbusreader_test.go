// Hardware-dependent functions that are not covered:
//   - NewReader: calls serial.Open which requires real hardware.
//   - ResetPort: calls serial.Open which requires real hardware.

package serialmbus

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yottabytesolutions/gombus"
	"go.bug.st/serial"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// validMBusResponse is the bytes from testresponse/response.hex.
//
//nolint:gochecknoglobals // test data constant
var validMBusResponse = []byte{
	0x68, 0xC7, 0xC7, 0x68, 0x08, 0x01, 0x72, 0x02, 0x75, 0x92, 0x72, 0x2D, 0x2C, 0x34, 0x0C, 0x53,
	0x00, 0x00, 0x00, 0x04, 0x0E, 0xE0, 0x01, 0x00, 0x00, 0x04, 0xFF, 0x07, 0xBA, 0x0B, 0x00, 0x00,
	0x04, 0xFF, 0x08, 0x24, 0x07, 0x00, 0x00, 0x04, 0x13, 0x91, 0x12, 0x00, 0x00, 0x84, 0x40, 0x14,
	0x00, 0x00, 0x00, 0x00, 0x84, 0x80, 0x40, 0x14, 0x00, 0x00, 0x00, 0x00, 0x04, 0x22, 0xF0, 0x0B,
	0x00, 0x00, 0x34, 0x22, 0x00, 0x00, 0x00, 0x00, 0x02, 0x59, 0x50, 0x15, 0x02, 0x5D, 0xFF, 0x13,
	0x02, 0x61, 0x51, 0x01, 0x04, 0x2D, 0x00, 0x00, 0x00, 0x00, 0x14, 0x2D, 0x28, 0x00, 0x00, 0x00,
	0x04, 0x3B, 0x00, 0x00, 0x00, 0x00, 0x14, 0x3B, 0x5D, 0x00, 0x00, 0x00, 0x04, 0xFF, 0x22, 0x00,
	0x00, 0x00, 0x00, 0x04, 0x6D, 0x3B, 0x2A, 0xF6, 0x27, 0x44, 0x0E, 0xF4, 0x00, 0x00, 0x00, 0x44,
	0xFF, 0x07, 0x0C, 0x06, 0x00, 0x00, 0x44, 0xFF, 0x08, 0xB5, 0x03, 0x00, 0x00, 0x44, 0x13, 0x9E,
	0x09, 0x00, 0x00, 0xC4, 0x40, 0x14, 0x00, 0x00, 0x00, 0x00, 0xC4, 0x80, 0x40, 0x14, 0x00, 0x00,
	0x00, 0x00, 0x54, 0x2D, 0x25, 0x00, 0x00, 0x00, 0x54, 0x3B, 0x5D, 0x00, 0x00, 0x00, 0x42, 0x6C,
	0xE1, 0x27, 0x02, 0xFF, 0x1A, 0x01, 0x1A, 0x0C, 0x78, 0x02, 0x75, 0x92, 0x72, 0x04, 0xFF, 0x16,
	0x86, 0x0B, 0x20, 0x00, 0x04, 0xFF, 0x17, 0xC9, 0xFF, 0x0E, 0x01, 0x49, 0x16,
}

// mockConn implements gombus.Conn for testing. Read serves readData once and
// then returns io.EOF, so a test that expects more reads must reset readPos.
type mockConn struct {
	readData []byte
	readPos  int
	readErrs []error // consumed one per Read call before readData is served
	readCall int
	writeErr error
	written  []byte
	closed   bool
}

func (m *mockConn) Read(b []byte) (int, error) {
	m.readCall++
	if len(m.readErrs) > 0 {
		err := m.readErrs[0]
		m.readErrs = m.readErrs[1:]
		if err != nil {
			return 0, err
		}
	}
	if m.readPos >= len(m.readData) {
		return 0, io.EOF
	}
	n := copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockConn) Write(b []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.written = append(m.written, b...)
	return len(b), nil
}

func (*mockConn) SetReadDeadline(time.Time) error  { return nil }
func (*mockConn) SetWriteDeadline(time.Time) error { return nil }

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

func newTestReader(t *testing.T, conn *mockConn) *Reader {
	t.Helper()
	mode := &serial.Mode{BaudRate: mbusBaudRate}
	r, err := newReaderFromPort(context.Background(), "test", 0x01, testLogger(), mode, conn)
	if err != nil {
		t.Fatalf("newReaderFromPort() unexpected error: %v", err)
	}
	r.initDelay = 0
	r.readDelay = 0
	return r
}

func TestNewReaderFromPort_InitMBusEOF(t *testing.T) {
	// Conn returns EOF on the init ack read. This is tolerated (idle bus).
	conn := &mockConn{}
	r := newTestReader(t, conn)
	if r == nil {
		t.Error("newReaderFromPort returned nil")
	}
}

func TestNewReaderFromPort_InitMBusSuccess(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)
	if r == nil {
		t.Error("newReaderFromPort returned nil on successful init")
	}
}

func TestNewReaderFromPort_InitTimeoutTolerated(t *testing.T) {
	// A frame read timeout on the init ack is tolerated like EOF.
	conn := &mockConn{readErrs: []error{gombus.ErrReadTimeout}}
	r := newTestReader(t, conn)
	if r == nil {
		t.Error("newReaderFromPort returned nil on init timeout")
	}
}

func TestNewReaderFromPort_NonEOFInitError(t *testing.T) {
	// A non-EOF, non-timeout init error must fail construction, close the
	// connection, and report the underlying error.
	hwErr := errors.New("some hardware error")
	conn := &mockConn{readErrs: []error{hwErr}}
	mode := &serial.Mode{BaudRate: mbusBaudRate}
	r, err := newReaderFromPort(context.Background(), "test", 0x01, testLogger(), mode, conn)
	if err == nil {
		t.Fatal("newReaderFromPort should fail when init fails with a non-EOF error")
	}
	if !errors.Is(err, hwErr) {
		t.Errorf("newReaderFromPort error = %v, want wrapped %v", err, hwErr)
	}
	if r != nil {
		t.Error("newReaderFromPort should return a nil reader on init failure")
	}
	if !conn.closed {
		t.Error("newReaderFromPort should close the connection on init failure")
	}
}

func TestNewReaderFromPort_InvalidTargetAddress(t *testing.T) {
	tests := []struct {
		name string
		addr byte
		ok   bool
	}{
		{name: "zero is unconfigured", addr: 0x00, ok: false},
		{name: "min primary", addr: 0x01, ok: true},
		{name: "max primary", addr: 250, ok: true},
		{name: "reserved 251", addr: 251, ok: false},
		{name: "secondary select", addr: 0xFD, ok: true},
		{name: "broadcast with reply", addr: 0xFE, ok: true},
		{name: "broadcast without reply", addr: 0xFF, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockConn{readData: []byte{0xE5}}
			mode := &serial.Mode{BaudRate: mbusBaudRate}
			_, err := newReaderFromPort(context.Background(), "test", tt.addr, testLogger(), mode, conn)
			if tt.ok && err != nil {
				t.Fatalf("newReaderFromPort(addr=%d) unexpected error: %v", tt.addr, err)
			}
			if !tt.ok {
				if err == nil {
					t.Fatalf("newReaderFromPort(addr=%d) should fail", tt.addr)
				}
				if !conn.closed {
					t.Error("newReaderFromPort should close the connection on invalid address")
				}
			}
		})
	}
}

func TestNewReaderFromPort_UsesConfiguredAddress(t *testing.T) {
	// Regression: the UD2 request must target the configured M-Bus address,
	// not a hardcoded one.
	const addr = byte(0x47)
	conn := &mockConn{readData: []byte{0xE5}}
	mode := &serial.Mode{BaudRate: mbusBaudRate}
	r, err := newReaderFromPort(context.Background(), "test", addr, testLogger(), mode, conn)
	if err != nil {
		t.Fatalf("newReaderFromPort() unexpected error: %v", err)
	}
	r.initDelay = 0
	r.readDelay = 0

	conn.written = nil
	conn.readPos = 0
	conn.readData = validMBusResponse
	if _, readErr := r.ReadHeatTelegram(context.Background()); readErr != nil {
		t.Fatalf("ReadHeatTelegram() error: %v", readErr)
	}

	// Short frame: start, control, address, checksum, stop.
	want := []byte{0x10, 0x5B, addr, 0x5B + addr, 0x16}
	if !bytes.Equal(conn.written, want) {
		t.Fatalf("written UD2 frame = %#x, want %#x", conn.written, want)
	}
	if !bytes.Equal(conn.written, gombus.RequestUD2(addr)) {
		t.Fatalf("written UD2 frame = %#x, want gombus.RequestUD2 = %#x", conn.written, gombus.RequestUD2(addr))
	}
}

func TestInitMBus_Success(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.written = nil
	conn.readPos = 0
	err := r.InitMBus(context.Background())
	if err != nil {
		t.Errorf("InitMBus() unexpected error: %v", err)
	}
	want := gombus.SndNKE(mbusInitAddress)
	if !bytes.Equal(conn.written, want) {
		t.Errorf("written SND_NKE frame = %#x, want %#x", conn.written, []byte(want))
	}
}

func TestInitMBus_WriteError(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.writeErr = errors.New("write error")
	err := r.InitMBus(context.Background())
	if err == nil {
		t.Error("InitMBus() should return error on write failure")
	}
}

func TestReadHeatTelegram_Success(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.readPos = 0
	conn.readData = validMBusResponse
	telegram, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error: %v", err)
	}
	if telegram.MeterID == "" {
		t.Error("ReadHeatTelegram() returned empty MeterID")
	}
	// Serial number is BCD little-endian at bytes 7..10 of the response.
	if telegram.SerialNo != "72927502" {
		t.Errorf("ReadHeatTelegram() SerialNo = %q, want %q", telegram.SerialNo, "72927502")
	}
}

func TestReadHeatTelegram_RetriesFrameTimeout(t *testing.T) {
	// First frame read times out, the retry succeeds without a second REQ_UD2.
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.written = nil
	conn.readPos = 0
	conn.readData = validMBusResponse
	conn.readErrs = []error{gombus.ErrReadTimeout}
	if _, err := r.ReadHeatTelegram(context.Background()); err != nil {
		t.Fatalf("ReadHeatTelegram() error after timeout retry: %v", err)
	}
	if !bytes.Equal(conn.written, gombus.RequestUD2(r.targetAddress)) {
		t.Errorf("request written more than once: %#x", conn.written)
	}
}

func TestReadHeatTelegram_AllAttemptsTimeOut(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.readPos = 0
	conn.readData = nil
	conn.readErrs = []error{
		gombus.ErrReadTimeout, gombus.ErrReadTimeout, gombus.ErrReadTimeout,
		gombus.ErrReadTimeout, gombus.ErrReadTimeout,
	}
	conn.readCall = 0
	_, err := r.ReadHeatTelegram(context.Background())
	if !errors.Is(err, gombus.ErrReadTimeout) {
		t.Fatalf("ReadHeatTelegram() error = %v, want gombus.ErrReadTimeout", err)
	}
	if conn.readCall != maxFrameReadAttempts {
		t.Errorf("frame read attempts = %d, want %d", conn.readCall, maxFrameReadAttempts)
	}
}

func TestReadHeatTelegram_WriteError(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.writeErr = errors.New("write error")
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on write failure")
	}
}

func TestReadHeatTelegram_ReadError(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	conn.readErrs = []error{errors.New("read error")}
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on read failure")
	}
}

func TestReadHeatTelegram_InvalidResponse(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	// Valid MBus frame structure but with CI byte 0x00 that Decode rejects.
	conn.readPos = 0
	conn.readData = []byte{0x68, 0x03, 0x03, 0x68, 0x08, 0x01, 0x00, 0x09, 0x16}
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error for invalid MBus frame")
	}
}

func TestReadHeatTelegram_ContextCancelled(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.ReadHeatTelegram(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("ReadHeatTelegram() error = %v, want context.Canceled", err)
	}
}

func TestInitMBus_ContextCancelledDuringDelay(t *testing.T) {
	conn := &mockConn{readData: []byte{0xE5}}
	r := newTestReader(t, conn)
	r.initDelay = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	conn.readPos = 0
	err := r.InitMBus(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("InitMBus() error = %v, want context.Canceled", err)
	}
}

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Errorf("sleepCtx(0) error = %v", err)
	}

	start := time.Now()
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Errorf("sleepCtx(1ms) error = %v", err)
	}
	if time.Since(start) < time.Millisecond {
		t.Error("sleepCtx() did not sleep for the specified duration")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("sleepCtx() on cancelled ctx error = %v, want context.Canceled", err)
	}
}
