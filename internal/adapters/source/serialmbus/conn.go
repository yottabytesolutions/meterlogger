package serialmbus

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"go.bug.st/serial"
)

// serialConn adapts a go.bug.st/serial Port to gombus.Conn. gombus works with
// absolute deadlines while the port exposes a relative read timeout, so
// SetReadDeadline converts between the two. EINTR from the OS is retried here
// so the gombus client never sees it.
type serialConn struct {
	port serial.Port
}

func newSerialConn(port serial.Port) *serialConn {
	return &serialConn{port: port}
}

func (s *serialConn) Read(b []byte) (int, error) {
	for {
		n, err := s.port.Read(b)
		if err != nil && errors.Is(err, syscall.EINTR) {
			continue
		}
		return n, err
	}
}

// Write sends all of b and drains the port so the bytes are on the wire before
// the caller starts waiting for the reply. gombus ignores the returned count,
// so a short write must surface as an error here.
func (s *serialConn) Write(b []byte) (int, error) {
	n, err := s.writeRetry(b)
	if err != nil {
		return n, err
	}
	if n != len(b) {
		return n, fmt.Errorf("wrote %d bytes, expected to write %d bytes", n, len(b))
	}
	if drainErr := s.drainRetry(); drainErr != nil {
		return n, fmt.Errorf("drain: %w", drainErr)
	}
	return n, nil
}

func (s *serialConn) writeRetry(b []byte) (int, error) {
	for {
		n, err := s.port.Write(b)
		if err != nil && errors.Is(err, syscall.EINTR) {
			continue
		}
		return n, err
	}
}

func (s *serialConn) drainRetry() error {
	for {
		err := s.port.Drain()
		if err != nil && errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}

// SetReadDeadline maps an absolute deadline onto the port's relative read
// timeout. The zero time clears the bound, matching net.Conn. A deadline that
// has already passed arms a zero timeout so the next Read polls once: the raw
// time.Until value is negative, and a negative timeout means serial.NoTimeout,
// which would block forever.
func (s *serialConn) SetReadDeadline(t time.Time) error {
	if t.IsZero() {
		return s.port.SetReadTimeout(serial.NoTimeout)
	}
	return s.port.SetReadTimeout(max(time.Until(t), 0))
}

// SetWriteDeadline is a no-op. go.bug.st/serial writes synchronously and
// exposes no write timeout.
func (*serialConn) SetWriteDeadline(time.Time) error { return nil }

func (s *serialConn) Close() error {
	return s.port.Close()
}
