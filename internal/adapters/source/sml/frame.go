package sml

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"syscall"
)

// SML transport frame (SML transport protocol version 1):
//
//	1B 1B 1B 1B  01 01 01 01   start escape + version
//	<payload>                  SML file, padded with 0x00 fill bytes
//	1B 1B 1B 1B  1A <n> <c c>  end escape + fill count n + CRC-16
//
// A literal 1B 1B 1B 1B inside the payload is escaped by prefixing it with
// another 1B 1B 1B 1B. The CRC covers everything from the first start
// escape byte through the fill count byte inclusive.

const (
	escByte      = 0x1B
	escLen       = 4
	endMsgByte   = 0x1A
	maxFillBytes = 3
	// maxFrameBytes caps an in-progress frame. A real SML file is a few
	// hundred bytes; anything larger means the end escape was lost or the
	// input is garbage, so the partial frame is dropped and the scanner
	// resynchronizes on the next start sequence.
	maxFrameBytes = 64 * 1024
	// frameOverhead is the start escape + version plus end escape + end
	// message, the fixed bytes around the payload.
	frameOverhead = 16
	// frameBufSize is the initial frame buffer capacity; a typical SML
	// file is a few hundred bytes.
	frameBufSize = 512
)

// CRC variant names reported by validateFrame.
const (
	crcVariantX25    = "x25"
	crcVariantKermit = "kermit"
)

//nolint:gochecknoglobals // immutable protocol byte sequences
var (
	escSeq   = []byte{escByte, escByte, escByte, escByte}
	startMsg = []byte{0x01, 0x01, 0x01, 0x01}
)

var errBadCRC = errors.New("SML frame CRC mismatch")

// frameScanner extracts raw SML transport frames from a byte stream.
type frameScanner struct {
	r      *bufio.Reader
	logger *slog.Logger
}

func newFrameScanner(src io.Reader, logger *slog.Logger) *frameScanner {
	return &frameScanner{r: bufio.NewReader(src), logger: logger}
}

// readByte reads one byte, retrying transient errors. io.EOF is wrapped so
// the caller can surface a dead serial stream as a fatal error.
func (s *frameScanner) readByte(ctx context.Context) (byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		b, err := s.r.ReadByte()
		if err == nil {
			return b, nil
		}
		if errors.Is(err, io.EOF) {
			// A closed serial stream means the producer is dead. Surface
			// it so the service's error path terminates the process
			// instead of staying ready with no data flow.
			return 0, fmt.Errorf("serial stream ended: %w", err)
		}
		if errors.Is(err, syscall.EINTR) || errors.Is(err, io.ErrNoProgress) {
			continue
		}
		return 0, err
	}
}

// findStart consumes the stream until a start sequence (escape + version)
// has been read.
func (s *frameScanner) findStart(ctx context.Context) error {
	escCount, verCount := 0, 0
	for {
		b, err := s.readByte(ctx)
		if err != nil {
			return err
		}
		switch {
		case b == escByte:
			if verCount > 0 {
				escCount = 0
			}
			escCount++
			verCount = 0
		case b == 0x01 && escCount >= escLen:
			verCount++
			if verCount == escLen {
				return nil
			}
		default:
			escCount, verCount = 0, 0
		}
	}
}

// nextFrame returns the next complete raw frame, from the first start
// escape byte through the two CRC bytes inclusive. Escaped payload escape
// sequences are kept verbatim (unescaping happens in validateFrame). On a
// nested start sequence or garbage after an escape the scanner
// resynchronizes and keeps looking.
//
//nolint:gocognit // complexity is inherent to the escape-sequence state machine
func (s *frameScanner) nextFrame(ctx context.Context) ([]byte, error) {
	for {
		if err := s.findStart(ctx); err != nil {
			return nil, err
		}
		buf := make([]byte, 0, frameBufSize)
		buf = append(buf, escSeq...)
		buf = append(buf, startMsg...)
		escRun := 0
		resync := false
		for !resync {
			if len(buf) > maxFrameBytes {
				s.logger.WarnContext(ctx, "SML frame exceeds size cap, dropping partial frame",
					slog.Int("cap", maxFrameBytes))
				resync = true
				continue
			}
			b, err := s.readByte(ctx)
			if err != nil {
				return nil, err
			}
			if b == escByte {
				escRun++
				if escRun < escLen {
					continue
				}
				escRun = 0
				done, ok, msgErr := s.handleEscape(ctx, &buf)
				if msgErr != nil {
					return nil, msgErr
				}
				if done {
					return buf, nil
				}
				if !ok {
					resync = true
				}
				continue
			}
			for range escRun {
				buf = append(buf, escByte)
			}
			escRun = 0
			buf = append(buf, b)
		}
	}
}

// handleEscape processes the four bytes following an escape sequence.
// done reports a complete frame in buf; ok=false asks the caller to
// resynchronize on the next start sequence.
func (s *frameScanner) handleEscape(ctx context.Context, buf *[]byte) (bool, bool, error) {
	msg := make([]byte, escLen)
	for i := range msg {
		b, err := s.readByte(ctx)
		if err != nil {
			return false, false, err
		}
		msg[i] = b
	}
	switch {
	case msg[0] == endMsgByte:
		*buf = append(*buf, escSeq...)
		*buf = append(*buf, msg...)
		return true, true, nil
	case bytes.Equal(msg, startMsg):
		// A new start inside a frame: the previous frame lost its end.
		// Restart with the new frame.
		s.logger.DebugContext(ctx, "SML start sequence inside frame, restarting frame")
		*buf = (*buf)[:frameOverhead/2]
		return false, true, nil
	case bytes.Equal(msg, escSeq):
		// Escaped literal escape sequence: keep all eight bytes so the
		// CRC still covers the raw frame.
		*buf = append(*buf, escSeq...)
		*buf = append(*buf, escSeq...)
		return false, true, nil
	default:
		s.logger.DebugContext(ctx, "unknown SML escape message, resynchronizing",
			slog.String("message", fmt.Sprintf("% X", msg)))
		return false, false, nil
	}
}

// validateFrame checks the frame CRC (X-25 first, then the Holley Kermit
// variant) and returns the unescaped payload with the fill bytes stripped,
// plus the name of the CRC variant that matched.
func validateFrame(frame []byte) ([]byte, string, error) {
	if len(frame) < frameOverhead {
		return nil, "", fmt.Errorf("frame too short: %d bytes", len(frame))
	}
	crcData := frame[:len(frame)-2]
	got := uint16(frame[len(frame)-2])<<byteBits | uint16(frame[len(frame)-1])
	var variant string
	switch got {
	case swap16(crc16X25(crcData)):
		variant = crcVariantX25
	case swap16(crc16Kermit(crcData)):
		variant = crcVariantKermit
	default:
		return nil, "", fmt.Errorf("%w: got 0x%04X, want 0x%04X (x25) or 0x%04X (kermit)",
			errBadCRC, got, swap16(crc16X25(crcData)), swap16(crc16Kermit(crcData)))
	}
	fill := int(frame[len(frame)-3])
	body := frame[frameOverhead/2 : len(frame)-frameOverhead/2]
	if fill > maxFillBytes || fill > len(body) {
		return nil, "", fmt.Errorf("invalid fill byte count %d", fill)
	}
	body = body[:len(body)-fill]
	// Undo payload escaping: a doubled escape sequence is one literal one.
	payload := bytes.ReplaceAll(body, append(append([]byte{}, escSeq...), escSeq...), escSeq)
	return payload, variant, nil
}
