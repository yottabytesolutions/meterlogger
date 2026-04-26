// Package serialmbus implements the M-Bus heat meter reader over a serial
// port. The package speaks the EN 13757 application layer using the
// yottabytesolutions/gombus fork, decodes the response into a
// domain.HeatTelegram, and recovers from common transient errors (EOF on
// init, framing errors, port hiccups). Frame parsing helpers and unit
// converters live in subpackages.
package serialmbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"syscall"
	"time"

	"go.bug.st/serial"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus/converters"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus/protocol"
	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	waitAfterInitCommand   = 300 * time.Millisecond
	waitAfterNormalCommand = 10 * time.Second
	readTimeoutDuration    = 1 * time.Second
	mbusBaudRate           = 9600
	maxZeroReadRetries     = 3
	busyLoopDelay          = 10 * time.Millisecond

	mbusInitControl    = 0x40
	mbusInitAddress    = 0xFD
	mbusUD2Control     = 0x5B
	mbusUD2Address     = 0x01
	mbusReadBufferSize = 2048
)

// serialPortIface abstracts the serial port for testability.
type serialPortIface interface {
	Read(b []byte) (n int, err error)
	Write(b []byte) (n int, err error)
	Drain() error
	Close() error
}

type Reader struct {
	port          string
	targetAddress byte
	serialMode    *serial.Mode
	serialPort    serialPortIface

	initRequest protocol.Frame
	ud2Request  protocol.Frame
	logger      *slog.Logger
	initDelay   time.Duration
	readDelay   time.Duration
}

// newReaderFromPort constructs a Reader from an already-opened port.
func newReaderFromPort(
	ctx context.Context,
	port string,
	targetAddress byte,
	logger *slog.Logger,
	mode *serial.Mode,
	sp serialPortIface,
) *Reader {
	reader := &Reader{
		port:          port,
		targetAddress: targetAddress,
		serialMode:    mode,
		serialPort:    sp,
		logger:        logger,
		initDelay:     waitAfterInitCommand,
		readDelay:     waitAfterNormalCommand,
		initRequest: (&protocol.ShortFrameStruct{
			Control: mbusInitControl,
			Address: mbusInitAddress,
		}).Prepare(),
		ud2Request: (&protocol.ShortFrameStruct{
			Control: mbusUD2Control,
			Address: mbusUD2Address,
		}).Prepare(),
	}

	err := reader.InitMBus(ctx)
	if err != nil && err.Error() != "EOF" {
		logger.ErrorContext(ctx, "Error initializing MBus", slog.Any("error", err))
	}

	return reader
}

func NewReader(ctx context.Context, port string, targetAddress byte, logger *slog.Logger) (*Reader, error) {
	mode := &serial.Mode{
		BaudRate: mbusBaudRate,
		Parity:   serial.EvenParity,
		StopBits: serial.OneStopBit,
	}

	// Open the serial connection
	s, err := serial.Open(port, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}

	if err = s.SetReadTimeout(readTimeoutDuration); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("failed to set read timeout: %w", err)
	}

	return newReaderFromPort(ctx, port, targetAddress, logger, mode, s), nil
}

func (r *Reader) InitMBus(ctx context.Context) error {
	r.logger.InfoContext(ctx, "Initializing MBus")
	_, err := r.writeWaitRead(ctx, r.initRequest.ToBytes(), r.initDelay)
	return err
}

func (r *Reader) ResetPort(ctx context.Context) {
	if err := r.serialPort.Close(); err != nil {
		r.logger.ErrorContext(ctx, "Error closing serial port", slog.Any("error", err))
	}

	s, err := serial.Open(r.port, r.serialMode)
	if err != nil {
		r.logger.ErrorContext(ctx, "Failed to open serial port", slog.Any("error", err))
		return
	}
	if err = s.SetReadTimeout(readTimeoutDuration); err != nil {
		r.logger.ErrorContext(ctx, "Failed to set read timeout", slog.Any("error", err))
		if closeErr := s.Close(); closeErr != nil {
			r.logger.ErrorContext(ctx, "Failed to close serial port after timeout error", slog.Any("error", closeErr))
		}
		return
	}
	r.serialPort = s

	_ = r.InitMBus(ctx)
}

func (r *Reader) ReadHeatTelegram(ctx context.Context) (domain.HeatTelegram, error) {
	telegram := domain.HeatTelegram{}
	response, err := r.writeWaitRead(ctx, r.ud2Request.ToBytes(), r.readDelay)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.logger.WarnContext(ctx, "Error writing UD2 request", slog.Any("error", err))
		}
		return telegram, err
	}

	r.logger.DebugContext(ctx, "Received response", slog.Any("response", fmt.Sprintf("%#x", response)))

	gombusResponse, err := protocol.ParseUsingGombus(response)
	if err != nil {
		r.logger.ErrorContext(ctx, "Error parsing response", slog.Any("error", err))
		return telegram, err
	}

	r.logger.DebugContext(ctx, "Parsed response", slog.Any("response", gombusResponse))
	for _, rec := range gombusResponse.DataRecords {
		r.logger.DebugContext(
			ctx,
			"data record",
			slog.Int("unitType", rec.Unit.Type),
			slog.Int("device", rec.Device),
			slog.Int("storageNr", rec.StorageNumber),
			slog.String("function", rec.Function),
			slog.Any("record", rec),
		)
	}

	telegram, err = converters.GombusToDomain(gombusResponse)
	if err != nil {
		r.logger.ErrorContext(ctx, "Error converting to domain", slog.Any("error", err))
		return telegram, err
	}

	r.logger.DebugContext(ctx, "heat telegram read", debuglog.HeatAttrs(telegram))
	return telegram, nil
}

func (r *Reader) writeWaitRead(ctx context.Context, b []byte, dur time.Duration) ([]byte, error) {
	if err := r.writeWithRetry(ctx, b); err != nil {
		return nil, err
	}
	if err := r.drainWithRetry(ctx); err != nil {
		return nil, err
	}

	r.logger.DebugContext(ctx, "Wrote bytes to serial port", slog.Any("bytes", fmt.Sprintf("%#x", b)))

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(dur):
	}

	return r.readWithRetry(ctx)
}

func (r *Reader) writeWithRetry(ctx context.Context, b []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := r.serialPort.Write(b)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		if n != len(b) {
			return fmt.Errorf("wrote %d bytes, expected to write %d bytes", n, len(b))
		}
		return nil
	}
}

func (r *Reader) drainWithRetry(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := r.serialPort.Drain(); err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		return nil
	}
}

func (r *Reader) readWithRetry(ctx context.Context) ([]byte, error) {
	buf := make([]byte, mbusReadBufferSize)
	zeroReads := 0
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		n, err := r.serialPort.Read(buf)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return nil, err
		}
		if n == 0 {
			zeroReads++
			if zeroReads > maxZeroReadRetries {
				return nil, fmt.Errorf("serial read: no data after %d retries", maxZeroReadRetries)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(busyLoopDelay):
				continue
			}
		}
		return buf[:n], nil
	}
}
