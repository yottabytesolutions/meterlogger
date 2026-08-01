// Package kamstrup reads Kamstrup Multical heat meters through the optical
// (IR eye) interface using the KMP protocol, as an alternative to the M-Bus
// reader. It targets the KMP generation: Multical 402, 403, 601, 602, 603,
// 801, and 803. The pre-KMP MC 66C and the MC 401 use different optical
// protocols and are not supported. The serial link runs at 1200 baud, 8 data
// bits, no parity, 2 stop bits.
package kamstrup

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"time"

	"go.bug.st/serial"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// KMP payload prefixes: the destination address and the commands used.
const (
	destHeatMeter  = 0x3F // heat meter application
	cmdGetType     = 0x01
	cmdGetSerialNo = 0x02
	cmdGetRegister = 0x10
)

const (
	baudRate = 1200
	dataBits = 8
	// requestTimeout bounds one request/response exchange. At 1200 baud a
	// register response takes well under a second; 2s leaves slack for the
	// meter's own processing time.
	requestTimeout = 2 * time.Second
	maxAttempts    = 3
	readChunkSize  = 64
)

var errReadTimeout = errors.New("read timeout")

// serialPort is the seam between the reader and the physical serial device,
// so tests can script exchanges without hardware.
type serialPort interface {
	io.ReadWriteCloser
	SetReadTimeout(d time.Duration) error
}

// Reader implements domain.HeatMeterReader over the optical interface.
// It is not safe for concurrent use; the logging service reads sequentially.
type Reader struct {
	device string
	port   serialPort
	logger *slog.Logger
	// timeout bounds one request/response exchange, attempts is the number
	// of exchanges tried per register. Fields so tests can shrink them.
	timeout  time.Duration
	attempts int

	// serialNo and meterID are fetched once and cached; they never change
	// for a physical meter.
	serialNo string
	meterID  string
	// warnedMissing tracks registers the meter reported as absent, so the
	// warning is logged once instead of every scrape.
	warnedMissing map[uint16]bool
}

// NewReader opens the optical head's serial device and returns a Reader.
// Untested: serial.Open requires real hardware (documented test exemption).
func NewReader(ctx context.Context, device string, logger *slog.Logger) (*Reader, error) {
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: dataBits,
		Parity:   serial.NoParity,
		StopBits: serial.TwoStopBits,
	}
	port, err := serial.Open(device, mode)
	if err != nil {
		return nil, fmt.Errorf("open optical serial port %s: %w", device, err)
	}
	return newReaderFromPort(ctx, device, port, logger), nil
}

// newReaderFromPort constructs a Reader on an already-open port. Tests use it
// with a scripted fake port.
func newReaderFromPort(ctx context.Context, device string, port serialPort, logger *slog.Logger) *Reader {
	logger.InfoContext(ctx, "kamstrup optical reader initialized", slog.String("device", device))
	return &Reader{
		device:        device,
		port:          port,
		logger:        logger,
		timeout:       requestTimeout,
		attempts:      maxAttempts,
		warnedMissing: make(map[uint16]bool),
	}
}

// ReadHeatTelegram reads the meter identity (cached after the first success)
// and every register in heatRegisters, one exchange per register.
func (r *Reader) ReadHeatTelegram(ctx context.Context) (domain.HeatTelegram, error) {
	serialNo, err := r.readSerialNo(ctx)
	if err != nil {
		return domain.HeatTelegram{}, fmt.Errorf("get serial number: %w", err)
	}
	meterID, err := r.readMeterID(ctx)
	if err != nil {
		return domain.HeatTelegram{}, fmt.Errorf("get meter type: %w", err)
	}

	telegram := domain.HeatTelegram{
		Timestamp: time.Now(),
		SerialNo:  serialNo,
		MeterID:   meterID,
	}
	for _, reg := range heatRegisters() {
		present, value, regErr := r.readRegister(ctx, reg)
		if regErr != nil {
			return domain.HeatTelegram{}, fmt.Errorf("register %d (%s): %w", reg.id, reg.name, regErr)
		}
		if !present {
			if !r.warnedMissing[reg.id] {
				r.warnedMissing[reg.id] = true
				r.logger.WarnContext(ctx, "meter does not provide register; field stays 0",
					slog.Int("register", int(reg.id)), slog.String("name", reg.name))
			}
			continue
		}
		reg.assign(&telegram, value)
	}
	return telegram, nil
}

// readSerialNo returns the cached serial number, fetching it on first use.
func (r *Reader) readSerialNo(ctx context.Context) (string, error) {
	if r.serialNo != "" {
		return r.serialNo, nil
	}
	resp, err := r.exchange(ctx, []byte{destHeatMeter, cmdGetSerialNo})
	if err != nil {
		return "", err
	}
	const serialRespLen = 6 // dest, cmd, 4-byte serial
	if len(resp) < serialRespLen {
		return "", fmt.Errorf("serial number response too short: %d bytes", len(resp))
	}
	r.serialNo = strconv.FormatUint(uint64(binary.BigEndian.Uint32(resp[2:serialRespLen])), 10)
	return r.serialNo, nil
}

// readMeterID returns the cached meter ID, fetching the type code on first
// use. The first two payload bytes after the command echo are the raw meter
// type code, rendered as hex.
func (r *Reader) readMeterID(ctx context.Context) (string, error) {
	if r.meterID != "" {
		return r.meterID, nil
	}
	resp, err := r.exchange(ctx, []byte{destHeatMeter, cmdGetType})
	if err != nil {
		return "", err
	}
	const typeRespLen = 4 // dest, cmd, 2-byte type code
	if len(resp) < typeRespLen {
		return "", fmt.Errorf("meter type response too short: %d bytes", len(resp))
	}
	r.meterID = fmt.Sprintf("Kamstrup (type %02X%02X)", resp[2], resp[3])
	return r.meterID, nil
}

// readRegister requests one register and converts the value to the canonical
// unit. The returned bool is false when the meter answered without a register
// block, which is how KMP signals an unsupported register.
func (r *Reader) readRegister(ctx context.Context, reg registerSpec) (bool, float64, error) {
	req := binary.BigEndian.AppendUint16([]byte{destHeatMeter, cmdGetRegister, 0x01}, reg.id)
	resp, err := r.exchange(ctx, req)
	if err != nil {
		return false, 0, err
	}
	block := resp[2:] // exchange verified the two-byte dest/cmd echo
	if len(block) == 0 {
		return false, 0, nil
	}

	const headerLen = 5 // rid(2), unit(1), length(1), siEx(1)
	if len(block) < headerLen {
		return false, 0, fmt.Errorf("register block too short: %d bytes", len(block))
	}
	rid := binary.BigEndian.Uint16(block[:2])
	if rid != reg.id {
		return false, 0, fmt.Errorf("response for register %d, want %d", rid, reg.id)
	}
	unitCode := block[2]
	length := int(block[3])
	siEx := block[4]
	const maxMantissaLen = 8
	if length == 0 || length > maxMantissaLen || len(block) != headerLen+length {
		return false, 0, fmt.Errorf("bad register block: length byte %d, block %d bytes", length, len(block))
	}

	raw := decodeValue(siEx, block[headerLen:])
	if math.IsInf(raw, 0) {
		return false, 0, fmt.Errorf("value overflow: siEx 0x%02X", siEx)
	}
	value, err := convertToCanonical(reg, unitCode, raw)
	if err != nil {
		return false, 0, err
	}
	return true, value, nil
}

// exchange performs one request/response round trip with retries. The
// response payload must echo the destination and command of the request.
func (r *Reader) exchange(ctx context.Context, payload []byte) ([]byte, error) {
	var lastErr error
	for attempt := range r.attempts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := r.exchangeOnce(ctx, payload)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		r.logger.DebugContext(ctx, "optical exchange failed",
			slog.Int("attempt", attempt+1), slog.Any("error", err))
	}
	return nil, lastErr
}

func (r *Reader) exchangeOnce(ctx context.Context, payload []byte) ([]byte, error) {
	if _, err := r.port.Write(encodeFrame(startRequest, payload)); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	raw, err := r.readFrame(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := decodeFrame(raw, startResponse)
	if err != nil {
		return nil, err
	}
	if len(resp) < 2 || resp[0] != payload[0] || resp[1] != payload[1] {
		return nil, fmt.Errorf("response does not echo request %02X %02X", payload[0], payload[1])
	}
	return resp, nil
}

// readFrame reads bytes until a complete response frame (0x40 ... 0x0D)
// arrives or the per-request timeout expires. Garbage before the start byte
// is discarded. go.bug.st/serial reports a read timeout as (0, nil), which is
// mapped to errReadTimeout; the fake test port does the same.
func (r *Reader) readFrame(ctx context.Context) ([]byte, error) {
	deadline := time.Now().Add(r.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	var frame []byte
	buf := make([]byte, readChunkSize)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errReadTimeout
		}
		if err := r.port.SetReadTimeout(remaining); err != nil {
			return nil, fmt.Errorf("set read timeout: %w", err)
		}
		n, err := r.port.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if n == 0 {
			return nil, errReadTimeout
		}
		var done bool
		frame, done, err = scanFrameBytes(frame, buf[:n])
		if err != nil {
			return nil, err
		}
		if done {
			return frame, nil
		}
	}
}

// scanFrameBytes appends chunk to the frame under construction, skipping
// garbage before the 0x40 start byte. It reports whether the 0x0D terminator
// arrived.
func scanFrameBytes(frame, chunk []byte) ([]byte, bool, error) {
	for _, b := range chunk {
		if len(frame) == 0 {
			if b == startResponse {
				frame = append(frame, b)
			}
			continue
		}
		frame = append(frame, b)
		if b == frameEnd {
			return frame, true, nil
		}
		if len(frame) > maxFrameLen {
			return nil, false, fmt.Errorf("response frame exceeds %d bytes", maxFrameLen)
		}
	}
	return frame, false, nil
}
