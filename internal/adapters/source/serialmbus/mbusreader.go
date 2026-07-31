// Package serialmbus implements the M-Bus heat meter reader over a serial
// port. It opens the port at the meter's settings (9600 baud, even parity),
// adapts it to gombus.Conn, and drives the EN 13757 exchange through the
// yottabytesolutions/gombus client: SND_NKE init, REQ_UD2 request, long frame
// decode. The decoded frame is mapped to a domain.HeatTelegram by the
// converters subpackage.
package serialmbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/gombus"
	"go.bug.st/serial"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus/converters"
	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	waitAfterInitCommand = 300 * time.Millisecond
	frameRetryDelay      = 100 * time.Millisecond
	mbusBaudRate         = 9600

	// maxFrameReadAttempts times gombus's fixed 2s frame timeout gives a slow
	// meter roughly the 10s of patience the previous implementation had.
	maxFrameReadAttempts = 5

	// mbusInitAddress is the SND_NKE destination: 0xFD (secondary select)
	// resets the link layer regardless of the slave's primary address.
	mbusInitAddress = 0xFD

	// Destination address bounds, mirrored from gombus (unexported there).
	// 0 marks an unconfigured slave, 251..255 are reserved except the two
	// special destinations below.
	minPrimaryAddress   = 1
	maxPrimaryAddress   = 250
	addrSecondarySelect = 0xFD
	addrBroadcastReply  = 0xFE
)

type Reader struct {
	port          string
	targetAddress byte
	serialMode    *serial.Mode
	client        *gombus.Client

	logger *slog.Logger
	// initDelay is the pause between SND_NKE and reading the ack.
	initDelay time.Duration
	// readDelay is the pause between frame read retries.
	readDelay time.Duration
	// frameAttempts is the number of frame reads tried per REQ_UD2.
	frameAttempts int
}

// validateTargetAddress accepts the destinations gombus will address: primary
// addresses 1..250 plus 0xFD (secondary select) and 0xFE (broadcast with
// reply). Checking at construction turns a misconfigured address into one
// clear startup error instead of a failure on every read.
func validateTargetAddress(addr byte) error {
	if (addr >= minPrimaryAddress && addr <= maxPrimaryAddress) ||
		addr == addrSecondarySelect || addr == addrBroadcastReply {
		return nil
	}
	return fmt.Errorf("invalid mbus target address %d: want 1..250, 253 or 254", addr)
}

// newReaderFromPort constructs a Reader from an already-opened connection.
// EOF or a read timeout during bus initialization is tolerated (idle bus),
// any other init error closes the connection and fails construction.
func newReaderFromPort(
	ctx context.Context,
	port string,
	targetAddress byte,
	logger *slog.Logger,
	mode *serial.Mode,
	conn gombus.Conn,
) (*Reader, error) {
	if err := validateTargetAddress(targetAddress); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			logger.ErrorContext(ctx, "Error closing serial port", slog.Any("error", closeErr))
		}
		return nil, err
	}

	reader := &Reader{
		port:          port,
		targetAddress: targetAddress,
		serialMode:    mode,
		client:        gombus.NewClient(conn),
		logger:        logger,
		initDelay:     waitAfterInitCommand,
		readDelay:     frameRetryDelay,
		frameAttempts: maxFrameReadAttempts,
	}

	if err := reader.InitMBus(ctx); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, gombus.ErrReadTimeout) {
			if closeErr := reader.client.Close(); closeErr != nil {
				logger.ErrorContext(ctx, "Error closing serial port after failed init", slog.Any("error", closeErr))
			}
			return nil, fmt.Errorf("mbus init: %w", err)
		}
		logger.WarnContext(ctx, "MBus init got no ack, continuing", slog.Any("error", err))
	}

	return reader, nil
}

func NewReader(ctx context.Context, port string, targetAddress byte, logger *slog.Logger) (*Reader, error) {
	mode := &serial.Mode{
		BaudRate: mbusBaudRate,
		Parity:   serial.EvenParity,
		StopBits: serial.OneStopBit,
	}

	s, err := serial.Open(port, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port: %w", err)
	}

	return newReaderFromPort(ctx, port, targetAddress, logger, mode, newSerialConn(s))
}

// InitMBus resets the bus link layer with SND_NKE and waits for the E5 ack.
func (r *Reader) InitMBus(ctx context.Context) error {
	r.logger.InfoContext(ctx, "Initializing MBus")
	if err := r.client.WriteFrame(ctx, gombus.SndNKE(mbusInitAddress)); err != nil {
		return fmt.Errorf("write SND_NKE: %w", err)
	}
	if err := sleepCtx(ctx, r.initDelay); err != nil {
		return err
	}
	if _, err := r.client.ReadSingleCharFrame(ctx); err != nil {
		return err
	}
	return nil
}

// ResetPort closes and reopens the serial port, then re-initializes the bus.
// Best effort: failures are logged and the next read cycle retries.
func (r *Reader) ResetPort(ctx context.Context) {
	if err := r.client.Close(); err != nil {
		r.logger.ErrorContext(ctx, "Error closing serial port", slog.Any("error", err))
	}

	s, err := serial.Open(r.port, r.serialMode)
	if err != nil {
		r.logger.ErrorContext(ctx, "Failed to open serial port", slog.Any("error", err))
		return
	}
	r.client = gombus.NewClient(newSerialConn(s))

	if initErr := r.InitMBus(ctx); initErr != nil {
		r.logger.WarnContext(ctx, "MBus init after port reset failed", slog.Any("error", initErr))
	}
}

func (r *Reader) ReadHeatTelegram(ctx context.Context) (domain.HeatTelegram, error) {
	telegram := domain.HeatTelegram{}

	frame, err := r.requestFrame(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			r.logger.WarnContext(ctx, "Error reading UD2 response", slog.Any("error", err))
		}
		return telegram, err
	}

	r.logger.DebugContext(ctx, "Received response", slog.Any("response", fmt.Sprintf("%#x", []byte(frame))))

	decoded, err := frame.Decode()
	if err != nil {
		r.logger.ErrorContext(ctx, "Error parsing response", slog.Any("error", err))
		return telegram, err
	}

	r.logger.DebugContext(ctx, "Parsed response", slog.Any("response", decoded))
	for _, rec := range decoded.DataRecords {
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

	telegram, err = converters.GombusToDomain(decoded)
	if err != nil {
		r.logger.ErrorContext(ctx, "Error converting to domain", slog.Any("error", err))
		return telegram, err
	}

	r.logger.DebugContext(ctx, "heat telegram read", debuglog.HeatAttrs(telegram))
	return telegram, nil
}

// requestFrame sends one REQ_UD2 and reads the long frame response. gombus
// caps each frame read at a fixed 2s; a slow meter needs more, so timeouts
// are retried up to frameAttempts times without re-sending the request.
func (r *Reader) requestFrame(ctx context.Context) (gombus.LongFrame, error) {
	if err := r.client.WriteFrame(ctx, gombus.RequestUD2(r.targetAddress)); err != nil {
		return nil, fmt.Errorf("write REQ_UD2: %w", err)
	}

	var lastErr error
	for attempt := range r.frameAttempts {
		if attempt > 0 {
			if err := sleepCtx(ctx, r.readDelay); err != nil {
				return nil, err
			}
		}
		frame, err := r.client.ReadLongFrame(ctx)
		if err == nil {
			return frame, nil
		}
		if !errors.Is(err, gombus.ErrReadTimeout) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// sleepCtx waits for dur or until ctx ends, whichever comes first.
func sleepCtx(ctx context.Context, dur time.Duration) error {
	if dur <= 0 {
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(dur):
		return nil
	}
}
