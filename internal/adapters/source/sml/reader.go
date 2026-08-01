// Package sml implements a grid meter reader for German SML electricity
// meters (EMH eHZ and mMe4.0, ISKRA MT681, EasyMeter Q3A/Q3B, eBZ DD3
// SM-type, Holley DTZ541) read over an IR read head. The meter pushes SML
// files unsolicited every one to five seconds at 9600 baud 8N1; no request
// or wakeup is sent. Each valid SML file becomes one domain.GridTelegram
// delivered on the channel returned by Telegrams.
package sml

import (
	"context"
	"io"
	"log/slog"
	"time"

	"go.bug.st/serial"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	smlBaudRate    = 9600
	smlDataBits    = 8
	smlReadTimeout = 1 * time.Second
)

// Reader reads SML files from an IR read head and implements
// domain.GridTelegramReader.
type Reader struct {
	logger     *slog.Logger
	usbPort    string
	telegrams  chan domain.GridTelegram
	portReader io.Reader // if non-nil, used instead of opening the serial port
	crcVariant string    // CRC variant of the first valid frame, logged once
}

func NewReader(usbPort string, logger *slog.Logger) *Reader {
	return &Reader{
		logger:    logger,
		usbPort:   usbPort,
		telegrams: make(chan domain.GridTelegram),
	}
}

// Telegrams returns the channel on which decoded telegrams are delivered.
// The channel is closed when ReadGridTelegrams returns.
func (r *Reader) Telegrams() <-chan domain.GridTelegram {
	return r.telegrams
}

// ReadGridTelegrams reads and parses SML files until ctx is cancelled or a
// non-recoverable error occurs. It must be called at most once: it closes
// the telegram channel on return.
func (r *Reader) ReadGridTelegrams(ctx context.Context) error {
	defer close(r.telegrams)
	src := r.portReader
	if src == nil {
		// Serial port opening requires real hardware and is exercised only
		// in the field; tests inject portReader instead.
		port, err := r.openPort()
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := port.Close(); closeErr != nil {
				r.logger.ErrorContext(ctx, "Failed to close serial port", slog.Any("error", closeErr))
			}
		}()
		src = port
	}

	scanner := newFrameScanner(src, r.logger)
	for {
		frame, err := scanner.nextFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		payload, variant, err := validateFrame(frame)
		if err != nil {
			r.logger.WarnContext(ctx, "Invalid SML frame", slog.Any("error", err))
			continue
		}
		r.logCRCVariantOnce(ctx, variant)
		telegram, err := parsePayload(payload, time.Now())
		if err != nil {
			r.logger.ErrorContext(ctx, "Failed to parse SML file", slog.Any("error", err))
			continue
		}
		r.logger.DebugContext(ctx, "SML telegram parsed, queuing", debuglog.GridAttrs(telegram))
		select {
		case r.telegrams <- telegram:
		case <-ctx.Done():
			return nil
		}
	}
}

//nolint:ireturn // serial.Open returns the driver's port interface
func (r *Reader) openPort() (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: smlBaudRate,
		DataBits: smlDataBits,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(r.usbPort, mode)
	if err != nil {
		return nil, err
	}
	if err = port.SetReadTimeout(smlReadTimeout); err != nil {
		if closeErr := port.Close(); closeErr != nil {
			r.logger.Error("Failed to close serial port", slog.Any("error", closeErr))
		}
		return nil, err
	}
	return port, nil
}

// logCRCVariantOnce records which CRC variant the meter uses. The Kermit
// variant identifies Holley DTZ541 firmware with the known CRC quirk.
func (r *Reader) logCRCVariantOnce(ctx context.Context, variant string) {
	if r.crcVariant != "" {
		return
	}
	r.crcVariant = variant
	r.logger.InfoContext(ctx, "SML frame CRC variant detected", slog.String("variant", variant))
}
