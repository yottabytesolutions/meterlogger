package converters

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jonaz/gombus"
)

// makeRecord creates a DecodedDataRecord for testing.
func makeRecord(unitType int, function string, value float64) gombus.DecodedDataRecord {
	return gombus.DecodedDataRecord{
		Unit:          gombus.Unit{Type: unitType},
		Function:      function,
		StorageNumber: 0,
		Device:        0,
		Value:         value,
	}
}

// fullFrame creates a DecodedFrame with all records needed for GombusToDomain.
func fullFrame() *gombus.DecodedFrame {
	const maxVal = "Maximum value"
	const instantVal = "Instantaneous value"

	return &gombus.DecodedFrame{
		SerialNumber: 123456,
		Manufacturer: "TST",
		DeviceType:   "Heat",
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, maxVal, 100.5),     // MaxFlow
			makeRecord(47, maxVal, 5000.0),    // MaxPower
			makeRecord(35, instantVal, 3600),  // SecondsCounter
			makeRecord(23, instantVal, 500.0), // VolumeCm3
			makeRecord(91, instantVal, 45.0),  // Tforward
			makeRecord(95, instantVal, 35.0),  // Treturn
			makeRecord(99, instantVal, 10.0),  // Tdiff
			makeRecord(15, instantVal, 1e9),   // Joules
			makeRecord(63, instantVal, 50.5),  // ActualFlow
			makeRecord(47, instantVal, 2500),  // ActualPower
		},
	}
}

func TestGombusToDomain_Success(t *testing.T) {
	frame := fullFrame()
	result, err := GombusToDomain(frame)
	if err != nil {
		t.Fatalf("GombusToDomain() unexpected error: %v", err)
	}

	if result.SerialNo != "123456" {
		t.Errorf("SerialNo = %q, want 123456", result.SerialNo)
	}
	if result.MaxFlow != 100.5 {
		t.Errorf("MaxFlow = %v, want 100.5", result.MaxFlow)
	}
	if result.MaxPower != 5000 {
		t.Errorf("MaxPower = %v, want 5000", result.MaxPower)
	}
	if result.SecondsCounter != 3600 {
		t.Errorf("SecondsCounter = %v, want 3600", result.SecondsCounter)
	}
	if result.VolumeCm3 != 500.0 {
		t.Errorf("VolumeCm3 = %v, want 500.0", result.VolumeCm3)
	}
	if result.Tforward != 45.0 {
		t.Errorf("Tforward = %v, want 45.0", result.Tforward)
	}
	if result.Treturn != 35.0 {
		t.Errorf("Treturn = %v, want 35.0", result.Treturn)
	}
	if result.Tdiff != 10.0 {
		t.Errorf("Tdiff = %v, want 10.0", result.Tdiff)
	}
	if result.ActualFlow != 50.5 {
		t.Errorf("ActualFlow = %v, want 50.5", result.ActualFlow)
	}
	if result.ActualPower != 2500 {
		t.Errorf("ActualPower = %v, want 2500", result.ActualPower)
	}
	if result.Joules != int64(1e9) {
		t.Errorf("Joules = %v, want 1e9", result.Joules)
	}
	if result.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestGombusToDomain_MissingMaxFlow(t *testing.T) {
	frame := &gombus.DecodedFrame{
		SerialNumber: 1,
		DataRecords:  []gombus.DecodedDataRecord{},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when MaxFlow record is missing")
	}
}

func TestGombusToDomain_MissingMaxPower(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5), // MaxFlow present
			// MaxPower (unit 47, max) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when MaxPower record is missing")
	}
}

func TestGombusToDomain_MissingSecondsCounter(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			// SecondsCounter (unit 35, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when SecondsCounter record is missing")
	}
}

func TestGombusToDomain_MissingVolume(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			// Volume (unit 23, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when Volume record is missing")
	}
}

func TestGombusToDomain_MissingTforward(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			makeRecord(23, "Instantaneous value", 500),
			// Tforward (unit 91, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when Tforward record is missing")
	}
}

func TestGombusToDomain_MissingTreturn(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			makeRecord(23, "Instantaneous value", 500),
			makeRecord(91, "Instantaneous value", 45),
			// Treturn (unit 95, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when Treturn record is missing")
	}
}

func TestGombusToDomain_MissingTdiff(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			makeRecord(23, "Instantaneous value", 500),
			makeRecord(91, "Instantaneous value", 45),
			makeRecord(95, "Instantaneous value", 35),
			// Tdiff (unit 99, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when Tdiff record is missing")
	}
}

func TestGombusToDomain_MissingJoules(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			makeRecord(23, "Instantaneous value", 500),
			makeRecord(91, "Instantaneous value", 45),
			makeRecord(95, "Instantaneous value", 35),
			makeRecord(99, "Instantaneous value", 10),
			// Joules (unit 15, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when Joules record is missing")
	}
}

func TestGombusToDomain_MissingActualFlow(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			makeRecord(23, "Instantaneous value", 500),
			makeRecord(91, "Instantaneous value", 45),
			makeRecord(95, "Instantaneous value", 35),
			makeRecord(99, "Instantaneous value", 10),
			makeRecord(15, "Instantaneous value", 1e9),
			// ActualFlow (unit 63, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when ActualFlow record is missing")
	}
}

func TestGombusToDomain_MissingActualPower(t *testing.T) {
	frame := &gombus.DecodedFrame{
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(63, "Maximum value", 100.5),
			makeRecord(47, "Maximum value", 5000),
			makeRecord(35, "Instantaneous value", 3600),
			makeRecord(23, "Instantaneous value", 500),
			makeRecord(91, "Instantaneous value", 45),
			makeRecord(95, "Instantaneous value", 35),
			makeRecord(99, "Instantaneous value", 10),
			makeRecord(15, "Instantaneous value", 1e9),
			makeRecord(63, "Instantaneous value", 50.5),
			// ActualPower (unit 47, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when ActualPower record is missing")
	}
}

func TestFindDataRecordValue_Found(t *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:          gombus.Unit{Type: 15},
			Function:      "Instantaneous value",
			StorageNumber: 0,
			Device:        0,
			Value:         42.0,
		},
		{
			Unit:          gombus.Unit{Type: 63},
			Function:      "Maximum value",
			StorageNumber: 0,
			Device:        0,
			Value:         100.5,
		},
	}

	record, err := FindDataRecordValue(&records, 15, "Instantaneous value")
	if err != nil {
		t.Fatalf("FindDataRecordValue() unexpected error: %v", err)
	}
	if record.Value != 42.0 {
		t.Errorf("FindDataRecordValue() value = %v, want 42.0", record.Value)
	}
}

func TestFindDataRecordValue_MaxValue(t *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:          gombus.Unit{Type: 63},
			Function:      "Maximum value",
			StorageNumber: 0,
			Device:        0,
			Value:         100.5,
		},
	}

	record, err := FindDataRecordValue(&records, 63, "Maximum value")
	if err != nil {
		t.Fatalf("FindDataRecordValue() unexpected error: %v", err)
	}
	if record.Value != 100.5 {
		t.Errorf("FindDataRecordValue() value = %v, want 100.5", record.Value)
	}
}

func TestFindDataRecordValue_NotFound(t *testing.T) {
	records := []gombus.DecodedDataRecord{}

	_, err := FindDataRecordValue(&records, 15, "Instantaneous value")
	if err == nil {
		t.Error("FindDataRecordValue() should return error when not found")
	}
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("FindDataRecordValue() error = %v, want ErrRecordNotFound", err)
	}
}

func TestFindDataRecordValue_WrongUnitType(t *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:          gombus.Unit{Type: 99},
			Function:      "Instantaneous value",
			StorageNumber: 0,
			Device:        0,
		},
	}

	_, err := FindDataRecordValue(&records, 15, "Instantaneous value")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("FindDataRecordValue() should return ErrRecordNotFound for wrong unit type")
	}
}

func TestFindDataRecordValue_WrongFunction(t *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:          gombus.Unit{Type: 15},
			Function:      "Maximum value",
			StorageNumber: 0,
			Device:        0,
		},
	}

	_, err := FindDataRecordValue(&records, 15, "Instantaneous value")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("FindDataRecordValue() should return ErrRecordNotFound for wrong function")
	}
}

func TestFindDataRecordValue_WrongStorageNumber(t *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:          gombus.Unit{Type: 15},
			Function:      "Instantaneous value",
			StorageNumber: 1,
			Device:        0,
		},
	}

	_, err := FindDataRecordValue(&records, 15, "Instantaneous value")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("FindDataRecordValue() should return ErrRecordNotFound for non-zero storage number")
	}
}

func TestFindDataRecordValue_WrongDevice(t *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:          gombus.Unit{Type: 15},
			Function:      "Instantaneous value",
			StorageNumber: 0,
			Device:        1,
		},
	}

	_, err := FindDataRecordValue(&records, 15, "Instantaneous value")
	if !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("FindDataRecordValue() should return ErrRecordNotFound for non-zero device")
	}
}

func TestLogAllDataRecords_Empty(_ *testing.T) {
	records := []gombus.DecodedDataRecord{}
	LogAllDataRecords(&records, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestLogAllDataRecords_WithRecords(_ *testing.T) {
	records := []gombus.DecodedDataRecord{
		{
			Unit:     gombus.Unit{Type: 15},
			Function: "Instantaneous value",
			Value:    42.0,
		},
	}
	LogAllDataRecords(&records, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
