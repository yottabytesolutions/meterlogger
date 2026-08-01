package gridmeter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	// maxMBusChannel is the highest M-Bus channel a DSMR meter exposes.
	maxMBusChannel = 4
	// dsmrTimestampLen is the length of a DSMR timestamp without DST suffix.
	dsmrTimestampLen = 12
	dsmrTimeLayout   = "060102150405"

	// Fixed UTC offsets used to honour the W/S suffix on DSMR timestamps.
	// The suffix disambiguates the repeated hour at the DST fall-back.
	winterOffsetSeconds = 1 * 60 * 60
	summerOffsetSeconds = 2 * 60 * 60

	mbusCodeDeviceType  = "24.1.0"
	mbusCodeEquipmentID = "96.1.0"
	// mbusCodeEquipmentIDBE is the Belgian (Fluvius eMUCS) equipment id code.
	mbusCodeEquipmentIDBE = "96.1.1"
	mbusCodeCapture       = "24.2.1"
	// mbusCodeCaptureBE is the Belgian gas reading code: the volume is NOT
	// temperature corrected, unlike the Dutch 24.2.1 reading.
	mbusCodeCaptureBE     = "24.2.3"
	mbusCodeLegacyCapture = "24.3.0"

	captureArgCount       = 2
	legacyCaptureArgCount = 6
)

// mbusChannelState accumulates the per-channel M-Bus lines while scanning a
// telegram. An entry is only emitted once a reading line has been seen.
type mbusChannelState struct {
	deviceType int
	serial     string
	capturedAt time.Time
	value      float64
	unit       string
	hasReading bool
}

// parseMBusDevices extracts the M-Bus subdevice readings (channels 1 to 4)
// from a raw telegram. Malformed M-Bus lines are logged at debug level and
// skipped; they never fail the whole telegram.
func parseMBusDevices(ctx context.Context, message string, logger *slog.Logger) []domain.MBusDeviceReading {
	location, err := loadGridMeterLocation()
	if err != nil {
		logger.DebugContext(ctx, "cannot load meter timezone, skipping M-Bus devices", slog.Any("error", err))
		return nil
	}

	var channels [maxMBusChannel + 1]mbusChannelState
	lines := strings.Split(message, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		channel, code, args, ok := splitMBusLine(line)
		if !ok {
			continue
		}
		state := &channels[channel]

		var lineErr error
		switch code {
		case mbusCodeDeviceType:
			lineErr = parseMBusDeviceType(args, state)
		case mbusCodeEquipmentID, mbusCodeEquipmentIDBE:
			lineErr = parseMBusEquipmentID(args, state)
		case mbusCodeCapture, mbusCodeCaptureBE:
			lineErr = parseMBusCapture(args, location, state)
		case mbusCodeLegacyCapture:
			var next string
			if i+1 < len(lines) {
				next = strings.TrimSpace(lines[i+1])
			}
			lineErr = parseMBusLegacyCapture(args, next, location, state)
		default:
			continue
		}
		if lineErr != nil {
			logger.DebugContext(ctx, "skipping malformed M-Bus line",
				slog.String("line", line),
				slog.Any("error", lineErr),
			)
		}
	}

	var devices []domain.MBusDeviceReading
	for channel := 1; channel <= maxMBusChannel; channel++ {
		state := channels[channel]
		if !state.hasReading {
			continue
		}
		devices = append(devices, domain.MBusDeviceReading{
			Channel:    channel,
			DeviceType: state.deviceType,
			SerialNo:   state.serial,
			CapturedAt: state.capturedAt,
			Value:      state.value,
			Unit:       state.unit,
		})
	}
	return devices
}

// splitMBusLine splits a "0-n:<code>(..)(..)" line into its channel, OBIS
// code, and parenthesised argument groups. The final result is false for any
// line that is not an M-Bus channel line.
func splitMBusLine(line string) (int, string, []string, bool) {
	rest, found := strings.CutPrefix(line, "0-")
	if !found {
		return 0, "", nil, false
	}
	channelStr, rest, found := strings.Cut(rest, ":")
	if !found {
		return 0, "", nil, false
	}
	channel, err := strconv.Atoi(channelStr)
	if err != nil || channel < 1 || channel > maxMBusChannel {
		return 0, "", nil, false
	}
	code, argStr, found := strings.Cut(rest, "(")
	if !found {
		return 0, "", nil, false
	}
	args := strings.Split(strings.TrimSuffix(argStr, ")"), ")(")
	return channel, code, args, true
}

// parseMBusDeviceType parses a "0-n:24.1.0(ddd)" argument. The zero padding
// varies per DSMR version: (3), (03), and (003) all mean device type 3.
func parseMBusDeviceType(args []string, state *mbusChannelState) error {
	if len(args) != 1 {
		return fmt.Errorf("device type: want 1 group, got %d", len(args))
	}
	deviceType, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("device type: %w", err)
	}
	state.deviceType = deviceType
	return nil
}

// parseMBusEquipmentID keeps the equipment identifier exactly as printed.
func parseMBusEquipmentID(args []string, state *mbusChannelState) error {
	if len(args) != 1 {
		return fmt.Errorf("equipment id: want 1 group, got %d", len(args))
	}
	state.serial = args[0]
	return nil
}

// parseMBusCapture parses a DSMR 4.x/5.x "0-n:24.2.1(TST)(value*unit)" line
// or the Belgian eMUCS "0-n:24.2.3" equivalent.
func parseMBusCapture(args []string, location *time.Location, state *mbusChannelState) error {
	if len(args) != captureArgCount {
		return fmt.Errorf("capture: want %d groups, got %d", captureArgCount, len(args))
	}
	capturedAt, err := parseMBusTimestamp(args[0], location)
	if err != nil {
		return fmt.Errorf("capture timestamp: %w", err)
	}
	valueStr, unit, found := strings.Cut(args[1], "*")
	if !found {
		return fmt.Errorf("capture value %q: missing unit separator", args[1])
	}
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return fmt.Errorf("capture value: %w", err)
	}
	state.capturedAt = capturedAt
	state.value = value
	state.unit = unit
	state.hasReading = true
	return nil
}

// parseMBusLegacyCapture parses a DSMR 2.2/3.0
// "0-n:24.3.0(TST)(..)(..)(..)(0-n:24.2.1)(unit)" line. The reading itself
// stands alone in parentheses on the next telegram line.
func parseMBusLegacyCapture(args []string, nextLine string, location *time.Location, state *mbusChannelState) error {
	if len(args) != legacyCaptureArgCount {
		return fmt.Errorf("legacy capture: want %d groups, got %d", legacyCaptureArgCount, len(args))
	}
	capturedAt, err := parseMBusTimestamp(args[0], location)
	if err != nil {
		return fmt.Errorf("legacy capture timestamp: %w", err)
	}
	inner, found := strings.CutPrefix(nextLine, "(")
	if !found || !strings.HasSuffix(inner, ")") {
		return errors.New("legacy capture: reading line missing")
	}
	value, err := strconv.ParseFloat(strings.TrimSuffix(inner, ")"), 64)
	if err != nil {
		return fmt.Errorf("legacy capture value: %w", err)
	}
	state.capturedAt = capturedAt
	state.value = value
	state.unit = args[len(args)-1]
	state.hasReading = true
	return nil
}

// parseMBusTimestamp parses a DSMR timestamp. A 13-char value carries a W
// (winter, UTC+1) or S (summer, UTC+2) suffix that disambiguates the repeated
// DST fall-back hour; a bare 12-char value (DSMR 2.2/3.0) is interpreted as
// Europe/Amsterdam local time.
func parseMBusTimestamp(timestamp string, location *time.Location) (time.Time, error) {
	switch len(timestamp) {
	case dsmrTimestampLen:
		return time.ParseInLocation(dsmrTimeLayout, timestamp, location)
	case dsmrTimestampLen + 1:
		var zone *time.Location
		switch timestamp[dsmrTimestampLen] {
		case 'W':
			zone = time.FixedZone("CET", winterOffsetSeconds)
		case 'S':
			zone = time.FixedZone("CEST", summerOffsetSeconds)
		default:
			return time.Time{}, fmt.Errorf("invalid DST suffix %q", timestamp[dsmrTimestampLen])
		}
		parsed, err := time.ParseInLocation(dsmrTimeLayout, timestamp[:dsmrTimestampLen], zone)
		if err != nil {
			return time.Time{}, err
		}
		return parsed.In(location), nil
	default:
		return time.Time{}, fmt.Errorf("invalid timestamp length %d", len(timestamp))
	}
}
