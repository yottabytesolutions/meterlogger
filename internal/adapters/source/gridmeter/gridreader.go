// Package gridmeter implements the DSMR P1 grid meter reader. It opens a
// serial port to a USB-to-P1 cable, scans for telegrams delimited by the
// DSMR start and end markers, and parses each one into a domain.GridTelegram.
// Decoded telegrams are delivered on the channel returned by Telegrams.
package gridmeter

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.bug.st/serial"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	gridBaudRate      = 115200
	gridReadTimeout   = 1 * time.Second
	gridDataBits      = 8
	crc16Polynomial   = 0xA001
	milliToUnit       = 1000
	gridMeterTimezone = "Europe/Amsterdam"
	// maxTelegramBytes caps an in-progress telegram. A real DSMR telegram is
	// around 1 KiB; anything larger means the end marker was lost or the input
	// is garbage, so the partial message is dropped.
	maxTelegramBytes = 64 * 1024
)

// loadGridMeterLocation loads the grid meter's timezone once and caches it,
// instead of re-parsing tzdata on every telegram.
//
//nolint:gochecknoglobals // sync.OnceValues cache, same pattern as enphase's shared httpClient
var loadGridMeterLocation = sync.OnceValues(func() (*time.Location, error) {
	return time.LoadLocation(gridMeterTimezone)
})

type GridReader struct {
	logger     *slog.Logger
	usbPort    string
	telegrams  chan domain.GridTelegram
	portReader io.Reader // if non-nil, used instead of opening the serial port
	decryptor  *dlmsDecryptor
	badFrames  int // encrypted frames dropped due to framing or decryption errors
}

func NewGridReader(usbPort string, logger *slog.Logger) *GridReader {
	return &GridReader{
		logger:    logger,
		usbPort:   usbPort,
		telegrams: make(chan domain.GridTelegram),
	}
}

// WithDecryption enables decryption of DLMS-encrypted telegrams (Luxembourg
// Smarty, Austrian Sagemcom T210-D). Both keys are 32 hex characters.
func (gr *GridReader) WithDecryption(decryptionKeyHex, authenticationKeyHex string) (*GridReader, error) {
	decryptor, err := newDLMSDecryptor(decryptionKeyHex, authenticationKeyHex)
	if err != nil {
		return nil, err
	}
	gr.decryptor = decryptor
	return gr, nil
}

// Telegrams returns the channel on which decoded telegrams are delivered.
// The channel is closed when ReadGridTelegrams returns.
func (gr *GridReader) Telegrams() <-chan domain.GridTelegram {
	return gr.telegrams
}

// ReadGridTelegrams reads and parses telegrams until ctx is cancelled or a
// non-recoverable error occurs. It must be called at most once: it closes the
// telegram channel on return.
//
//nolint:gocognit // complexity is inherent to the serial protocol state machine
func (gr *GridReader) ReadGridTelegrams(ctx context.Context) error {
	defer close(gr.telegrams)
	var src io.Reader
	if gr.portReader != nil {
		src = gr.portReader
	} else {
		mode := &serial.Mode{
			BaudRate: gridBaudRate,
			DataBits: gridDataBits,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}

		port, err := serial.Open(gr.usbPort, mode)
		if err != nil {
			return err
		}
		if err = port.SetReadTimeout(gridReadTimeout); err != nil {
			return err
		}
		defer func() {
			if closeErr := port.Close(); closeErr != nil {
				gr.logger.ErrorContext(ctx, "Failed to close serial port", slog.Any("error", closeErr))
			}
		}()
		src = port
	}

	reader := bufio.NewReader(src)
	var messageBuilder strings.Builder
	messageStarted := false

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		head, err := reader.Peek(1)
		if err != nil {
			if stop, readErr := gr.classifyReadError(ctx, err); stop {
				return readErr
			}
			continue
		}

		// An encrypted DLMS frame; a plaintext telegram never contains 0xDB
		// at the read position between lines.
		if head[0] == dlmsStartByte {
			messageBuilder.Reset()
			messageStarted = false
			if gr.decryptor == nil {
				return errEncryptedWithoutKey
			}
			message, frameErr := gr.decryptor.readFrame(reader)
			if frameErr != nil {
				gr.badFrames++
				gr.logger.WarnContext(ctx, "dropping encrypted frame", slog.Any("error", frameErr))
				continue
			}
			if stopped := gr.deliver(ctx, message); stopped {
				return nil
			}
			continue
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if stop, readErr := gr.classifyReadError(ctx, err); stop {
				return readErr
			}
			continue
		}

		// Check for the start of a message
		if strings.HasPrefix(line, "/") {
			messageStarted = true
			messageBuilder.Reset()
			gr.logger.DebugContext(ctx, "P1 message start")
		}

		if messageStarted {
			messageBuilder.WriteString(line)

			if messageBuilder.Len() > maxTelegramBytes {
				gr.logger.WarnContext(ctx, "Telegram exceeds size cap, dropping partial message",
					slog.Int("size", messageBuilder.Len()),
					slog.Int("cap", maxTelegramBytes),
				)
				messageBuilder.Reset()
				messageStarted = false
				continue
			}

			// Check for the end of the message
			if strings.HasPrefix(line, "!") {
				messageStarted = false
				if stopped := gr.deliver(ctx, messageBuilder.String()); stopped {
					return nil
				}
			}
		}
	}
}

// classifyReadError decides how to react to a read error. EOF stops the
// reader: a closed serial stream means the producer is dead, and surfacing it
// lets the service's error path terminate the process instead of staying
// ready with no data flow. Interrupts are retried; everything else is logged
// and retried.
func (gr *GridReader) classifyReadError(ctx context.Context, err error) (bool, error) {
	if errors.Is(err, io.EOF) {
		return true, fmt.Errorf("serial stream ended: %w", err)
	}
	if errors.Is(err, syscall.EINTR) || errors.Is(err, io.ErrNoProgress) {
		return false, nil
	}
	gr.logger.ErrorContext(ctx, "Error reading from serial port", slog.Any("error", err))
	return false, nil
}

// deliver validates, parses, and queues one complete telegram text. It
// returns true when ctx was cancelled while queueing.
func (gr *GridReader) deliver(ctx context.Context, message string) bool {
	if !isValidChecksum(message) {
		gr.logger.WarnContext(ctx, "Invalid checksum for message")
		return false
	}
	telegram, parseErr := parseTelegram(message)
	if parseErr != nil {
		gr.logger.ErrorContext(ctx, "Failed to parse telegram", slog.Any("error", parseErr))
		return false
	}
	telegram.MBusDevices = parseMBusDevices(ctx, message, gr.logger)
	gr.logger.DebugContext(ctx, "grid telegram parsed, queuing", debuglog.GridAttrs(telegram))
	select {
	case gr.telegrams <- telegram:
		return false
	case <-ctx.Done():
		return true
	}
}

func calculateCrc16(data []byte) uint16 {
	var crc uint16 = 0x0000
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&0x0001 != 0 {
				crc = (crc >> 1) ^ crc16Polynomial
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func isValidChecksum(message string) bool {
	message = strings.TrimSpace(message)

	// Find the index of '!' character
	idx := strings.LastIndex(message, "!")
	if idx == -1 || idx+5 > len(message) {
		return false
	}

	// Extract the checksum from the message
	checksumStr := message[idx+1 : idx+5]
	expectedChecksum, err := strconv.ParseUint(checksumStr, 16, 16)
	if err != nil {
		return false
	}

	// Extract the message body up to and including '!'
	data := []byte(message[:idx+1])

	// Compute the checksum
	computedChecksum := calculateCrc16(data)

	return uint16(expectedChecksum) == computedChecksum
}

//nolint:gocognit,funlen // complexity is inherent to the P1 telegram parsing with many optional fields
func parseTelegram(message string) (domain.GridTelegram, error) {
	values := make(map[string]string)
	for line := range strings.SplitSeq(message, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] == '/' || line[0] == '!' {
			continue
		}

		key, rest, found := strings.Cut(line, "(")
		if !found {
			continue
		}

		value := rest
		// Handle possible nested parentheses
		if strings.Contains(value, ")(") {
			value = strings.ReplaceAll(value, ")(", "|")
		}
		value = strings.TrimRight(value, ")")
		values[key] = value
	}

	// Parse the timestamp
	timestampStr, ok := values["0-0:1.0.0"]
	if !ok || len(timestampStr) < 12 {
		return domain.GridTelegram{}, errors.New("invalid timestamp format")
	}
	timestampStr = timestampStr[:12]
	location, err := loadGridMeterLocation()
	if err != nil {
		return domain.GridTelegram{}, err
	}
	timestamp, err := time.ParseInLocation("060102150405", timestampStr, location)
	if err != nil {
		return domain.GridTelegram{}, err
	}

	// Helper functions
	parseFloat := func(key string) (float64, error) {
		val, found := values[key]
		if !found {
			return 0, fmt.Errorf("missing key %s", key)
		}
		val = strings.Split(val, "*")[0]
		return strconv.ParseFloat(val, 64)
	}

	parseInt := func(key string) (int, error) {
		val, found := values[key]
		if !found {
			return 0, fmt.Errorf("missing key %s", key)
		}
		val = strings.Split(val, "*")[0]
		return strconv.Atoi(val)
	}

	// Currents are integer amperes on Dutch meters (001*A) but carry
	// decimals on Belgian meters (000.27*A); parse as float and round.
	parseAmps := func(key string) int {
		amps, ampErr := parseFloat(key)
		if ampErr != nil {
			return 0
		}
		return int(math.Round(amps))
	}

	// Parse required values with error checking
	usageCounter1, usageCounter2, outputCounter1, outputCounter2, err := parseEnergyCounters(parseFloat, values)
	if err != nil {
		return domain.GridTelegram{}, err
	}
	totalPowerUsage, err := parseFloat("1-0:1.7.0")
	if err != nil {
		return domain.GridTelegram{}, err
	}
	totalPowerOutput, err := parseFloat("1-0:2.7.0")
	if err != nil {
		return domain.GridTelegram{}, err
	}

	// Parse optional values
	brownoutsP1, _ := parseInt("1-0:32.32.0")
	brownoutsP2, _ := parseInt("1-0:52.32.0")
	brownoutsP3, _ := parseInt("1-0:72.32.0")
	spikesP1, _ := parseInt("1-0:32.36.0")
	spikesP2, _ := parseInt("1-0:52.36.0")
	spikesP3, _ := parseInt("1-0:72.36.0")
	voltageP1, _ := parseFloat("1-0:32.7.0")
	voltageP2, _ := parseFloat("1-0:52.7.0")
	voltageP3, _ := parseFloat("1-0:72.7.0")
	currentP1 := parseAmps("1-0:31.7.0")
	currentP2 := parseAmps("1-0:51.7.0")
	currentP3 := parseAmps("1-0:71.7.0")
	powerUsageP1, _ := parseFloat("1-0:21.7.0")
	powerUsageP2, _ := parseFloat("1-0:41.7.0")
	powerUsageP3, _ := parseFloat("1-0:61.7.0")
	powerOutputP1, _ := parseFloat("1-0:22.7.0")
	powerOutputP2, _ := parseFloat("1-0:42.7.0")
	powerOutputP3, _ := parseFloat("1-0:62.7.0")

	// Belgian capaciteitstarief peak demand fields; zero when absent.
	avgDemandKW, _ := parseFloat("1-0:1.4.0")
	maxDemandMonth, maxDemandMonthAt := parseMonthlyPeak(values["1-0:1.6.0"], location)

	return domain.GridTelegram{
		Time:             timestamp,
		MeterMerkType:    firstValue(values, "1-3:0.2.8", "0-0:96.1.4"),
		Serienummer:      firstValue(values, "0-0:96.1.1", "0-0:42.0.0"),
		UsageCounter1:    usageCounter1,
		UsageCounter2:    usageCounter2,
		OutputCounter1:   outputCounter1,
		OutputCounter2:   outputCounter2,
		TotalPowerUsage:  int(totalPowerUsage * milliToUnit),
		TotalPowerOutput: int(totalPowerOutput * milliToUnit),
		BrownoutsP1:      brownoutsP1,
		BrownoutsP2:      brownoutsP2,
		BrownoutsP3:      brownoutsP3,
		SpikesP1:         spikesP1,
		SpikesP2:         spikesP2,
		SpikesP3:         spikesP3,
		VoltageP1:        voltageP1,
		VoltageP2:        voltageP2,
		VoltageP3:        voltageP3,
		CurrentP1:        currentP1,
		CurrentP2:        currentP2,
		CurrentP3:        currentP3,
		PowerUsageP1:     int(powerUsageP1 * milliToUnit),
		PowerUsageP2:     int(powerUsageP2 * milliToUnit),
		PowerUsageP3:     int(powerUsageP3 * milliToUnit),
		PowerOutputP1:    int(powerOutputP1 * milliToUnit),
		PowerOutputP2:    int(powerOutputP2 * milliToUnit),
		PowerOutputP3:    int(powerOutputP3 * milliToUnit),
		AvgDemand:        int(math.Round(avgDemandKW * milliToUnit)),
		MaxDemandMonth:   maxDemandMonth,
		MaxDemandMonthAt: maxDemandMonthAt,
	}, nil
}

// parseEnergyCounters reads the energy registers and returns usage tariff 1
// and 2 and output tariff 1 and 2. Dutch and Belgian meters publish tariffed
// counters (1.8.1/1.8.2 and 2.8.1/2.8.2); Luxembourgish and Austrian meters
// publish only totals (1.8.0/2.8.0). Totals land in counter 1 with counter 2
// left at zero.
func parseEnergyCounters(
	parseFloat func(string) (float64, error), values map[string]string,
) (float64, float64, float64, float64, error) {
	if _, tariffed := values["1-0:1.8.1"]; tariffed {
		usage1, err := parseFloat("1-0:1.8.1")
		if err != nil {
			return 0, 0, 0, 0, err
		}
		usage2, err := parseFloat("1-0:1.8.2")
		if err != nil {
			return 0, 0, 0, 0, err
		}
		output1, err := parseFloat("1-0:2.8.1")
		if err != nil {
			return 0, 0, 0, 0, err
		}
		output2, err := parseFloat("1-0:2.8.2")
		if err != nil {
			return 0, 0, 0, 0, err
		}
		return usage1, usage2, output1, output2, nil
	}
	usage1, err := parseFloat("1-0:1.8.0")
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("no energy counters found (need 1-0:1.8.1 or 1-0:1.8.0): %w", err)
	}
	output1, err := parseFloat("1-0:2.8.0")
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return usage1, 0, output1, 0, nil
}

// parseMonthlyPeak parses the "1-0:1.6.0(TST)(vv.vvv*kW)" running-month
// maximum demand line. The nested groups were joined with '|' during line
// splitting. The line is optional and defensively parsed: anything malformed
// yields zero values.
func parseMonthlyPeak(raw string, location *time.Location) (int, time.Time) {
	timestampStr, valueStr, found := strings.Cut(raw, "|")
	if !found {
		return 0, time.Time{}
	}
	capturedAt, err := parseMBusTimestamp(timestampStr, location)
	if err != nil {
		return 0, time.Time{}
	}
	demandKW, err := strconv.ParseFloat(strings.Split(valueStr, "*")[0], 64)
	if err != nil {
		return 0, time.Time{}
	}
	return int(math.Round(demandKW * milliToUnit)), capturedAt
}

// firstValue returns the first key present in values, or an empty string.
func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if v, ok := values[key]; ok {
			return v
		}
	}
	return ""
}
