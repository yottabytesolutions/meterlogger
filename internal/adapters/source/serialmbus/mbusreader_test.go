package serialmbus

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

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

// mockSerialPort implements serialPortIface for testing.
type mockSerialPort struct {
	readData    []byte
	readPos     int
	writeErr    error
	flushErr    error
	readErr     error
	writtenData []byte
	shortWrite  bool
}

func (m *mockSerialPort) Read(b []byte) (int, error) {
	if m.readErr != nil {
		return 0, m.readErr
	}
	if m.readPos >= len(m.readData) {
		return 0, io.EOF
	}
	n := copy(b, m.readData[m.readPos:])
	m.readPos += n
	return n, nil
}

func (m *mockSerialPort) Write(b []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.writtenData = append(m.writtenData, b...)
	if m.shortWrite {
		return 1, nil // write less than requested
	}
	return len(b), nil
}

func (m *mockSerialPort) Drain() error {
	return m.flushErr
}

func (m *mockSerialPort) Close() error {
	return nil
}

func newTestReader(port *mockSerialPort) *Reader {
	l := testLogger()
	mode := &serial.Mode{BaudRate: mbusBaudRate}
	return newReaderFromPort(context.Background(), "test", 0x01, l, mode, port)
}

func TestNewReaderFromPort_InitMBusEOF(t *testing.T) {
	// port returns EOF on first read (InitMBus) - this error is tolerated
	port := &mockSerialPort{readErr: io.EOF}
	r := newTestReader(port)
	if r == nil {
		t.Error("newReaderFromPort returned nil")
	}
}

func TestNewReaderFromPort_InitMBusSuccess(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	if r == nil {
		t.Error("newReaderFromPort returned nil on successful init")
	}
}

func TestInitMBus_Success(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	port.readPos = 0
	port.readData = []byte{0xE5}
	err := r.InitMBus(context.Background())
	if err != nil {
		t.Errorf("InitMBus() unexpected error: %v", err)
	}
}

func TestInitMBus_WriteError(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	port.writeErr = errors.New("write error")
	err := r.InitMBus(context.Background())
	if err == nil {
		t.Error("InitMBus() should return error on write failure")
	}
}

func TestReadHeatTelegram_Success(t *testing.T) {
	// Init reads ACK, then ReadHeatTelegram reads valid response
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	port.readPos = 0
	port.readData = validMBusResponse
	telegram, err := r.ReadHeatTelegram(context.Background())
	if err != nil {
		t.Fatalf("ReadHeatTelegram() error: %v", err)
	}
	if telegram.MeterID == "" {
		t.Error("ReadHeatTelegram() returned empty MeterID")
	}
}

func TestReadHeatTelegram_WriteError(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	port.writeErr = errors.New("write error")
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on write failure")
	}
}

func TestReadHeatTelegram_ShortWrite(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	port.shortWrite = true
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on short write")
	}
}

func TestReadHeatTelegram_DrainError(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	port.flushErr = errors.New("drain error")
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on drain failure")
	}
}

func TestReadHeatTelegram_ReadError(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	port.readErr = errors.New("read error")
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error on read failure")
	}
}

func TestReadHeatTelegram_InvalidResponse(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)
	r.readDelay = 0

	// Valid MBus frame structure but with CI byte 0x00 that ParseUsingGombus rejects
	port.readPos = 0
	port.readData = []byte{0x68, 0x03, 0x03, 0x68, 0x08, 0x01, 0x00, 0x09, 0x16}
	_, err := r.ReadHeatTelegram(context.Background())
	if err == nil {
		t.Error("ReadHeatTelegram() should return error for invalid MBus frame")
	}
}

func TestWriteWaitRead_Success(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	port.readPos = 0
	data, err := r.writeWaitRead(context.Background(), []byte{0x10, 0x40, 0x01, 0x41, 0x16}, 0)
	if err != nil {
		t.Fatalf("writeWaitRead() error: %v", err)
	}
	if len(data) == 0 {
		t.Error("writeWaitRead() returned empty data")
	}
}

func TestWriteWaitRead_WithSleep(t *testing.T) {
	port := &mockSerialPort{readData: []byte{0xE5}}
	r := newTestReader(port)

	port.readPos = 0
	start := time.Now()
	_, _ = r.writeWaitRead(context.Background(), []byte{0x10}, time.Millisecond)
	if time.Since(start) < time.Millisecond {
		t.Error("writeWaitRead() did not sleep for the specified duration")
	}
}
