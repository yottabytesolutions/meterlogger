package serialmbus

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
	"time"

	"go.bug.st/serial"
)

// fakePort implements the go.bug.st/serial Port methods the adapter uses.
// The embedded interface satisfies the rest; calling those panics, which is
// fine because the adapter never touches them.
type fakePort struct {
	serial.Port

	readData    []byte
	readPos     int
	written     []byte
	writeErr    error
	drainErr    error
	shortWrite  bool
	eintrReads  int
	eintrWrites int
	eintrDrains int
	drainCalls  int
	timeouts    []time.Duration
	closed      bool
}

func (f *fakePort) Read(b []byte) (int, error) {
	if f.eintrReads > 0 {
		f.eintrReads--
		return 0, syscall.EINTR
	}
	if f.readPos >= len(f.readData) {
		return 0, io.EOF
	}
	n := copy(b, f.readData[f.readPos:])
	f.readPos += n
	return n, nil
}

func (f *fakePort) Write(b []byte) (int, error) {
	if f.eintrWrites > 0 {
		f.eintrWrites--
		return 0, syscall.EINTR
	}
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.written = append(f.written, b...)
	if f.shortWrite {
		return 1, nil
	}
	return len(b), nil
}

func (f *fakePort) Drain() error {
	if f.eintrDrains > 0 {
		f.eintrDrains--
		return syscall.EINTR
	}
	f.drainCalls++
	return f.drainErr
}

func (f *fakePort) SetReadTimeout(t time.Duration) error {
	f.timeouts = append(f.timeouts, t)
	return nil
}

func (f *fakePort) Close() error {
	f.closed = true
	return nil
}

func TestSerialConn_WriteFullAndDrain(t *testing.T) {
	port := &fakePort{}
	conn := newSerialConn(port)

	data := []byte{0x10, 0x40, 0xFD, 0x3D, 0x16}
	n, err := conn.Write(data)
	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() n = %d, want %d", n, len(data))
	}
	if !bytes.Equal(port.written, data) {
		t.Errorf("port received %#x, want %#x", port.written, data)
	}
	if port.drainCalls != 1 {
		t.Errorf("Drain() called %d times, want 1", port.drainCalls)
	}
}

func TestSerialConn_WriteShort(t *testing.T) {
	port := &fakePort{shortWrite: true}
	conn := newSerialConn(port)

	if _, err := conn.Write([]byte{0x10, 0x40}); err == nil {
		t.Error("Write() should return error on short write")
	}
}

func TestSerialConn_WriteError(t *testing.T) {
	wantErr := errors.New("write error")
	port := &fakePort{writeErr: wantErr}
	conn := newSerialConn(port)

	if _, err := conn.Write([]byte{0x10}); !errors.Is(err, wantErr) {
		t.Errorf("Write() error = %v, want %v", err, wantErr)
	}
}

func TestSerialConn_DrainError(t *testing.T) {
	wantErr := errors.New("drain error")
	port := &fakePort{drainErr: wantErr}
	conn := newSerialConn(port)

	if _, err := conn.Write([]byte{0x10}); !errors.Is(err, wantErr) {
		t.Errorf("Write() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestSerialConn_EINTRRetries(t *testing.T) {
	port := &fakePort{
		readData:    []byte{0xE5},
		eintrReads:  1,
		eintrWrites: 1,
		eintrDrains: 1,
	}
	conn := newSerialConn(port)

	if _, err := conn.Write([]byte{0x10}); err != nil {
		t.Fatalf("Write() error after EINTR: %v", err)
	}
	buf := make([]byte, 8)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read() error after EINTR: %v", err)
	}
	if n != 1 || buf[0] != 0xE5 {
		t.Errorf("Read() = %#x (n=%d), want 0xE5", buf[:n], n)
	}
}

func TestSerialConn_SetReadDeadline(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want func(time.Duration) bool
	}{
		{
			name: "zero time clears the bound",
			in:   time.Time{},
			want: func(d time.Duration) bool { return d == serial.NoTimeout },
		},
		{
			name: "past deadline arms a zero timeout, never negative",
			in:   time.Now().Add(-time.Second),
			want: func(d time.Duration) bool { return d == 0 },
		},
		{
			name: "future deadline arms a positive timeout",
			in:   time.Now().Add(time.Hour),
			want: func(d time.Duration) bool { return d > 0 && d <= time.Hour },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := &fakePort{}
			conn := newSerialConn(port)
			if err := conn.SetReadDeadline(tt.in); err != nil {
				t.Fatalf("SetReadDeadline() error: %v", err)
			}
			if len(port.timeouts) != 1 || !tt.want(port.timeouts[0]) {
				t.Errorf("SetReadTimeout got %v", port.timeouts)
			}
		})
	}
}

func TestSerialConn_SetWriteDeadlineNoop(t *testing.T) {
	conn := newSerialConn(&fakePort{})
	if err := conn.SetWriteDeadline(time.Now()); err != nil {
		t.Errorf("SetWriteDeadline() error: %v", err)
	}
}

func TestSerialConn_Close(t *testing.T) {
	port := &fakePort{}
	conn := newSerialConn(port)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !port.closed {
		t.Error("Close() did not close the port")
	}
}
