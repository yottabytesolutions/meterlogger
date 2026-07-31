// Package converters maps gombus M-Bus data records into domain.HeatTelegram
// fields. It centralises unit handling (J to kJ, mW to W, decimal scaling)
// so the reader stays free of unit-conversion logic.
package converters

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/yottabytesolutions/gombus"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// ErrRecordNotFound is returned when a required MBus data record is not found.
var ErrRecordNotFound = errors.New("record not found")

// heatField maps one M-Bus record (selected by VIF type and function) to one
// HeatTelegram field.
type heatField struct {
	name     string
	unitType int
	function string
	assign   func(t *domain.HeatTelegram, v float64)
}

// heatFields lists every record a heat telegram is built from.
func heatFields() []heatField {
	return []heatField{
		{
			"max flow", gombus.VIFVolumeFlow, gombus.FunctionMaximum,
			func(t *domain.HeatTelegram, v float64) { t.MaxFlow = v },
		},
		{
			"max power", gombus.VIFPowerW, gombus.FunctionMaximum,
			func(t *domain.HeatTelegram, v float64) { t.MaxPower = int64(v) },
		},
		{
			"seconds counter", gombus.VIFOnTime, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.SecondsCounter = int64(v) },
		},
		{
			"volume", gombus.VIFVolume, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.VolumeCm3 = v },
		},
		{
			"flow temperature", gombus.VIFFlowTemperature, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.Tforward = v },
		},
		{
			"return temperature", gombus.VIFReturnTemperature, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.Treturn = v },
		},
		{
			"temperature difference", gombus.VIFTemperatureDifference, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.Tdiff = v },
		},
		{
			"energy", gombus.VIFEnergyJoule, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.Joules = int64(v) },
		},
		{
			"actual flow", gombus.VIFVolumeFlow, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.ActualFlow = v },
		},
		{
			"actual power", gombus.VIFPowerW, gombus.FunctionInstantaneous,
			func(t *domain.HeatTelegram, v float64) { t.ActualPower = int64(v) },
		},
	}
}

// GombusToDomain converts a gombus.DecodedFrame to a domain.HeatTelegram.
func GombusToDomain(frame *gombus.DecodedFrame) (domain.HeatTelegram, error) {
	result := domain.HeatTelegram{
		Timestamp: time.Now(),
		SerialNo:  strconv.FormatInt(int64(frame.SerialNumber), 10),
		MeterID:   fmt.Sprintf("%s (%s)", frame.Manufacturer, frame.DeviceType),
	}

	for _, f := range heatFields() {
		record, err := FindDataRecordValue(&frame.DataRecords, f.unitType, f.function)
		if err != nil {
			return result, fmt.Errorf("%s: %w", f.name, err)
		}
		f.assign(&result, record.Value)
	}
	return result, nil
}

func FindDataRecordValue(
	records *[]gombus.DecodedDataRecord,
	unitType int,
	measurementType string,
) (gombus.DecodedDataRecord, error) {
	const storageNo = 0
	const deviceID = 0
	for _, record := range *records {
		if record.Unit.Type == unitType &&
			record.Function == measurementType &&
			record.StorageNumber == storageNo &&
			record.Device == deviceID {
			return record, nil
		}
	}

	return gombus.DecodedDataRecord{}, ErrRecordNotFound
}

func LogAllDataRecords(records *[]gombus.DecodedDataRecord, logger *slog.Logger) {
	for _, record := range *records {
		logger.Info("Record", slog.Any("record", record))
	}
}
