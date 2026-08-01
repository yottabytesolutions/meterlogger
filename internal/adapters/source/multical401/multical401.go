// Package multical401 reads Kamstrup Multical 401 and 66C heat meters
// through the optical (IR eye) interface. These pre-KMP meters, common in
// Dutch district heating, answer a plain ASCII poll with a fixed telegram of
// ten 7-digit fields. The link is asymmetric: requests go out at 300 baud,
// the response comes back at 1200 baud, both 7 data bits, even parity,
// 2 stop bits. The meter is battery powered and only answers when polled.
package multical401

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"go.bug.st/serial"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// Requests are bare ASCII with no terminator. The meter ignores anything
// else. reqSerial returns the customer (serial) number telegram.
const (
	reqData   = "/#1"
	reqSerial = "/#2"
)

const (
	requestBaud  = 300
	responseBaud = 1200
	dataBits     = 7
	// meterID is fixed: this protocol generation has no type register.
	meterID = "Kamstrup Multical 401/66C (optical)"

	// txDrainDelay lets the request drain at 300 baud (about 100 ms for
	// three bytes). It must stay well under the meter's earliest response
	// (0.3 s): the port switches to the response baud rate and flushes
	// stale input after this delay, and a long delay would discard the
	// start of an early answer.
	txDrainDelay = 200 * time.Millisecond
	// firstByteTimeout bounds the wait for the response to start; the meter
	// answers 0.3 to 2 s after the request. interReadTimeout bounds the gap
	// between subsequent reads within one response.
	firstByteTimeout = 3 * time.Second
	interReadTimeout = 2 * time.Second
	// Kamstrup: repeat ignored requests at minimum 5 s intervals.
	retryDelay  = 5 * time.Second
	maxAttempts = 3

	readChunkSize = 64
	maxLineLen    = 128
)

var errReadTimeout = errors.New("read timeout")

// serialPort is the seam between the reader and the physical serial device.
// It includes the mode switch so tests can assert the 300-to-1200 baud
// sequence without hardware.
type serialPort interface {
	io.ReadWriteCloser
	SetMode(mode *serial.Mode) error
	ResetInputBuffer() error
	SetReadTimeout(d time.Duration) error
}

// Reader implements domain.HeatMeterReader over the optical interface.
// It is not safe for concurrent use; the logging service reads sequentially.
type Reader struct {
	device string
	port   serialPort
	cfg    Config
	logger *slog.Logger

	// Timing fields mirror the package constants so tests can shrink them.
	drainDelay time.Duration
	firstByte  time.Duration
	interRead  time.Duration
	retryDelay time.Duration
	attempts   int

	// serialNo is fetched once at construction and cached; it never changes
	// for a physical meter. It stays empty when the meter ignores reqSerial.
	serialNo string
}

// requestMode is the serial mode for sending: 300 baud, 7 data bits, even
// parity, 2 stop bits.
func requestMode() *serial.Mode {
	return &serial.Mode{
		BaudRate: requestBaud,
		DataBits: dataBits,
		Parity:   serial.EvenParity,
		StopBits: serial.TwoStopBits,
	}
}

// responseMode is the serial mode for receiving: same framing at 1200 baud.
func responseMode() *serial.Mode {
	return &serial.Mode{
		BaudRate: responseBaud,
		DataBits: dataBits,
		Parity:   serial.EvenParity,
		StopBits: serial.TwoStopBits,
	}
}

// NewReader opens the optical head's serial device, fetches the meter serial
// number once, and returns a Reader.
// Untested: serial.Open requires real hardware (documented test exemption).
func NewReader(ctx context.Context, device string, cfg Config, logger *slog.Logger) (*Reader, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	port, err := serial.Open(device, requestMode())
	if err != nil {
		return nil, fmt.Errorf("open optical serial port %s: %w", device, err)
	}
	r := newReaderFromPort(ctx, device, port, cfg, logger)
	r.fetchSerialNo(ctx)
	return r, nil
}

// newReaderFromPort constructs a Reader on an already-open port without
// touching it. Tests use it with a scripted fake port and shrunk timings;
// NewReader calls fetchSerialNo right after.
func newReaderFromPort(
	ctx context.Context, device string, port serialPort, cfg Config, logger *slog.Logger,
) *Reader {
	logger.InfoContext(ctx, "multical401 optical reader initialized", slog.String("device", device))
	return &Reader{
		device:     device,
		port:       port,
		cfg:        cfg,
		logger:     logger,
		drainDelay: txDrainDelay,
		firstByte:  firstByteTimeout,
		interRead:  interReadTimeout,
		retryDelay: retryDelay,
		attempts:   maxAttempts,
	}
}

// fetchSerialNo polls the customer number telegram once and caches the
// result. Failure is tolerated: one field-tested 401 ignored some request
// types, and a missing serial should not take the whole reader down.
func (r *Reader) fetchSerialNo(ctx context.Context) {
	err := r.exchange(ctx, reqSerial, func(line []byte) error {
		serialNo, parseErr := parseSerialLine(line)
		if parseErr != nil {
			return parseErr
		}
		r.serialNo = serialNo
		return nil
	})
	if err != nil {
		r.logger.WarnContext(ctx, "meter did not answer serial number request; using empty serial",
			slog.Any("error", err))
	}
}

// ReadHeatTelegram polls the data telegram and maps it to the domain type.
// A nonzero meter info code is logged but does not fail the read.
func (r *Reader) ReadHeatTelegram(ctx context.Context) (domain.HeatTelegram, error) {
	var fields dataFields
	err := r.exchange(ctx, reqData, func(line []byte) error {
		parsed, parseErr := parseDataLine(line)
		if parseErr != nil {
			return parseErr
		}
		fields = parsed
		return nil
	})
	if err != nil {
		return domain.HeatTelegram{}, fmt.Errorf("read data telegram: %w", err)
	}
	if fields.infoCode != 0 {
		r.logger.WarnContext(ctx, "meter reports nonzero info code",
			slog.Int64("infocode", fields.infoCode))
	}
	telegram, err := buildTelegram(fields, r.cfg)
	if err != nil {
		return domain.HeatTelegram{}, err
	}
	telegram.Timestamp = time.Now()
	telegram.SerialNo = r.serialNo
	telegram.MeterID = meterID
	return telegram, nil
}

// exchange performs one poll with retries. accept parses the response line;
// a parse failure counts as a failed attempt, since a garbled line means the
// exchange went wrong. Repeats wait retryDelay, the minimum repeat interval
// Kamstrup specifies for ignored requests.
func (r *Reader) exchange(ctx context.Context, request string, accept func(line []byte) error) error {
	var lastErr error
	for attempt := range r.attempts {
		if attempt > 0 {
			if err := sleepCtx(ctx, r.retryDelay); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := r.exchangeOnce(ctx, request)
		if err == nil {
			err = accept(line)
			if err == nil {
				return nil
			}
		}
		lastErr = err
		r.logger.DebugContext(ctx, "optical exchange failed",
			slog.String("request", request), slog.Int("attempt", attempt+1), slog.Any("error", err))
	}
	return lastErr
}

// exchangeOnce sends one request at 300 baud, switches the open port to
// 1200 baud, flushes stale input, and reads the response line.
func (r *Reader) exchangeOnce(ctx context.Context, request string) ([]byte, error) {
	if err := r.port.SetMode(requestMode()); err != nil {
		return nil, fmt.Errorf("set request mode: %w", err)
	}
	if _, err := r.port.Write([]byte(request)); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if err := sleepCtx(ctx, r.drainDelay); err != nil {
		return nil, err
	}
	if err := r.port.SetMode(responseMode()); err != nil {
		return nil, fmt.Errorf("set response mode: %w", err)
	}
	if err := r.port.ResetInputBuffer(); err != nil {
		return nil, fmt.Errorf("flush input buffer: %w", err)
	}
	return r.readLine(ctx)
}

// readLine reads bytes until the CR terminator. The first read waits up to
// firstByte for the meter to start answering; subsequent reads use the
// shorter interRead gap. go.bug.st/serial reports a read timeout as (0, nil),
// which is mapped to errReadTimeout; the fake test port does the same.
func (r *Reader) readLine(ctx context.Context) ([]byte, error) {
	var line []byte
	buf := make([]byte, readChunkSize)
	timeout := r.firstByte
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := r.port.SetReadTimeout(timeout); err != nil {
			return nil, fmt.Errorf("set read timeout: %w", err)
		}
		n, err := r.port.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if n == 0 {
			return nil, errReadTimeout
		}
		for _, b := range buf[:n] {
			if b == '\r' {
				return line, nil
			}
			line = append(line, b)
			if len(line) > maxLineLen {
				return nil, fmt.Errorf("response exceeds %d bytes without terminator", maxLineLen)
			}
		}
		timeout = r.interRead
	}
}

// sleepCtx waits d unless the context ends first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
