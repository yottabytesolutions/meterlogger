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

// GombusToDomain converts a gombus.DecodedFrame to a domain.HeatTelegram.
//
//nolint:funlen // function is long but each step maps one MBus record; extracting helpers would obscure the protocol
func GombusToDomain(frame *gombus.DecodedFrame) (domain.HeatTelegram, error) {
	result := domain.HeatTelegram{}

	// ReadHeatTelegram generic fields first
	result.Timestamp = time.Now()
	result.SerialNo = strconv.FormatInt(int64(frame.SerialNumber), 10)
	result.MeterID = fmt.Sprintf("%s (%s)", frame.Manufacturer, frame.DeviceType)

	var err error
	var record gombus.DecodedDataRecord

	const maxVal = "Maximum value"
	const instantVal = "Instantaneous value"

	const recordMaxFlow = 63
	const recordMaxPower = 47
	const recordSecondsCounter = 35
	const recordVolumeCm3 = 23
	const recordTforward = 91
	const recordTreturn = 95
	const recordTdiff = 99
	const recordJoules = 15
	const recordActualFlow = 63
	const recordActualPower = 47

	record, err = FindDataRecordValue(&frame.DataRecords, recordMaxFlow, maxVal)
	if err != nil {
		return result, err
	}
	result.MaxFlow = record.Value

	record, err = FindDataRecordValue(&frame.DataRecords, recordMaxPower, maxVal)
	if err != nil {
		return result, err
	}
	result.MaxPower = int64(record.Value)

	record, err = FindDataRecordValue(&frame.DataRecords, recordSecondsCounter, instantVal)
	if err != nil {
		return result, err
	}
	result.SecondsCounter = int64(record.Value)

	record, err = FindDataRecordValue(&frame.DataRecords, recordVolumeCm3, instantVal)
	if err != nil {
		return result, err
	}
	result.VolumeCm3 = record.Value

	record, err = FindDataRecordValue(&frame.DataRecords, recordTforward, instantVal)
	if err != nil {
		return result, err
	}
	result.Tforward = record.Value

	record, err = FindDataRecordValue(&frame.DataRecords, recordTreturn, instantVal)
	if err != nil {
		return result, err
	}
	result.Treturn = record.Value

	record, err = FindDataRecordValue(&frame.DataRecords, recordTdiff, instantVal)
	if err != nil {
		return result, err
	}
	result.Tdiff = record.Value

	record, err = FindDataRecordValue(&frame.DataRecords, recordJoules, instantVal)
	if err != nil {
		return result, err
	}
	result.Joules = int64(record.Value)

	record, err = FindDataRecordValue(&frame.DataRecords, recordActualFlow, instantVal)
	if err != nil {
		return result, err
	}
	result.ActualFlow = record.Value

	record, err = FindDataRecordValue(&frame.DataRecords, recordActualPower, instantVal)
	if err != nil {
		return result, err
	}
	result.ActualPower = int64(record.Value)

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
