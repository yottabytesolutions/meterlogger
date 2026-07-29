// Hardware-dependent functions that are not covered:
//   - NewReader: calls serial.Open which requires real hardware.
//   - ResetPort: calls serial.Open which requires real hardware.

package serialmbus

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	"go.bug.st/serial"
)

// testPort is a placeholder serial port name used across reader tests.
const testPort = "test"

// zeroThenDataPort returns n=0 for the first `zeroReads` reads, then returns data.
type zeroThenDataPort struct {
	mockSerialPort

	zeroReads int
	callCount int
}

func (m *zeroThenDataPort) Read(b []byte) (int, error) {
	if m.callCount < m.zeroReads {
		m.callCount++
		return 0, nil
	}
	return m.mockSerialPort.Read(b)
}

// eintrThenSuccessPort returns EINTR once, then succeeds.
type eintrThenSuccessPort struct {
	mockSerialPort

	writeCalls int
	drainCalls int
	readCalls  int
}

func (m *eintrThenSuccessPort) Write(b []byte) (int, error) {
	m.writeCalls++
	if m.writeCalls == 1 {
		return 0, syscall.EINTR
	}
	return m.mockSerialPort.Write(b)
}

func (m *eintrThenSuccessPort) Drain() error {
	m.drainCalls++
	if m.drainCalls == 1 {
		return syscall.EINTR
	}
	return nil
}

func (m *eintrThenSuccessPort) Read(b []byte) (int, error) {
	m.readCalls++
	if m.readCalls == 1 {
		return 0, syscall.EINTR
	}
	return m.mockSerialPort.Read(b)
}

func TestReadHeatTelegram_ContextCancelled(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := r.ReadHeatTelegram(ctx)
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on cancelled context")
	}
}

func TestWriteWaitRead_ContextCancelledAfterDrain(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	// Use a delay long enough to allow context cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := r.writeWaitRead(ctx, []byte{0x10}, 100*time.Millisecond)
	if err == nil {
		t.Error("writeWaitRead() should return error when context is cancelled during wait")
	}
}

func TestWriteWithRetry_ContextCancelled(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.writeWithRetry(ctx, []byte{0x10})
	if err == nil {
		t.Error("writeWithRetry() should return error when context is already cancelled")
	}
}

func TestDrainWithRetry_ContextCancelled(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.drainWithRetry(ctx)
	if err == nil {
		t.Error("drainWithRetry() should return error when context is already cancelled")
	}
}

func TestReadWithRetry_ContextCancelled(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.readWithRetry(ctx)
	if err == nil {
		t.Error("readWithRetry() should return error when context is already cancelled")
	}
}

func TestReadWithRetry_ZeroRead_ThenData(t *testing.T) {
	// Port returns 0 bytes once, then real data.
	port := &zeroThenDataPort{
		mockSerialPort: mockSerialPort{readData: []byte{0xE5}},
		zeroReads:      1,
	}
	r := &Reader{
		port:       testPort,
		serialPort: port,
		logger:     testLogger(),
		initDelay:  0,
		readDelay:  0,
	}

	data, err := r.readWithRetry(context.Background())
	if err != nil {
		t.Fatalf("readWithRetry() unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Error("readWithRetry() returned empty data after zero-read")
	}
}

func TestReadWithRetry_ZeroRead_ContextCancelled(t *testing.T) {
	// Port always returns 0 bytes; context will be cancelled.
	alwaysZero := &zeroThenDataPort{
		mockSerialPort: mockSerialPort{},
		zeroReads:      1000,
	}
	r := &Reader{
		port:       testPort,
		serialPort: alwaysZero,
		logger:     testLogger(),
		initDelay:  0,
		readDelay:  0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()

	_, err := r.readWithRetry(ctx)
	if err == nil {
		t.Error("readWithRetry() should return error when context cancelled during zero-read loop")
	}
}

func TestWriteWithRetry_EINTRRetry(t *testing.T) {
	// First write returns EINTR, second succeeds.
	port := &eintrThenSuccessPort{
		mockSerialPort: mockSerialPort{readData: []byte{0xE5}},
	}
	r := &Reader{
		port:       testPort,
		serialPort: port,
		logger:     testLogger(),
	}
	err := r.writeWithRetry(context.Background(), []byte{0x10, 0x40, 0x01, 0x41, 0x16})
	if err != nil {
		t.Errorf("writeWithRetry() unexpected error after EINTR retry: %v", err)
	}
	if port.writeCalls < 2 {
		t.Errorf("expected at least 2 write calls, got %d", port.writeCalls)
	}
}

func TestDrainWithRetry_EINTRRetry(t *testing.T) {
	// First drain returns EINTR, second succeeds.
	port := &eintrThenSuccessPort{
		mockSerialPort: mockSerialPort{readData: []byte{0xE5}},
	}
	r := &Reader{
		port:       testPort,
		serialPort: port,
		logger:     testLogger(),
	}
	err := r.drainWithRetry(context.Background())
	if err != nil {
		t.Errorf("drainWithRetry() unexpected error after EINTR retry: %v", err)
	}
	if port.drainCalls < 2 {
		t.Errorf("expected at least 2 drain calls, got %d", port.drainCalls)
	}
}

func TestReadWithRetry_EINTRRetry(t *testing.T) {
	port := &eintrThenSuccessPort{
		mockSerialPort: mockSerialPort{readData: []byte{0xE5}},
	}
	r := &Reader{
		port:       testPort,
		serialPort: port,
		logger:     testLogger(),
	}
	data, err := r.readWithRetry(context.Background())
	if err != nil {
		t.Fatalf("readWithRetry() unexpected error after EINTR retry: %v", err)
	}
	if len(data) == 0 {
		t.Error("readWithRetry() returned empty data")
	}
}

func TestNewReaderFromPort_NonEOFInitError(t *testing.T) {
	// Port returns a non-EOF error on first read (InitMBus).
	// This is logged but not fatal - the reader is still returned.
	nonEOFErr := errors.New("some hardware error")
	port := &mockSerialPort{readErr: nonEOFErr}
	l := testLogger()
	mode := &serial.Mode{BaudRate: mbusBaudRate}
	r := newReaderFromPort(context.Background(), testPort, 0x01, l, mode, port)
	if r == nil {
		t.Error("newReaderFromPort should return a reader even when init fails with non-EOF error")
	}
}
