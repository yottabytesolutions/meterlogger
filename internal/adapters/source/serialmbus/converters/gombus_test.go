package converters

import (
	"errors"
	"strings"
	"testing"

	"github.com/yottabytesolutions/gombus"
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
	return &gombus.DecodedFrame{
		SerialNumber: 123456,
		Manufacturer: "TST",
		DeviceType:   "Heat",
		DataRecords: []gombus.DecodedDataRecord{
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),                 // MaxFlow
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000.0),                    // MaxPower
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),                // SecondsCounter
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500.0),               // VolumeCm3
			makeRecord(gombus.VIFFlowTemperature, gombus.FunctionInstantaneous, 45.0),       // Tforward
			makeRecord(gombus.VIFReturnTemperature, gombus.FunctionInstantaneous, 35.0),     // Treturn
			makeRecord(gombus.VIFTemperatureDifference, gombus.FunctionInstantaneous, 10.0), // Tdiff
			makeRecord(gombus.VIFEnergyJoule, gombus.FunctionInstantaneous, 1e9),            // Joules
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionInstantaneous, 50.5),            // ActualFlow
			makeRecord(gombus.VIFPowerW, gombus.FunctionInstantaneous, 2500),                // ActualPower
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5), // MaxFlow present
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500),
			makeRecord(gombus.VIFFlowTemperature, gombus.FunctionInstantaneous, 45),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500),
			makeRecord(gombus.VIFFlowTemperature, gombus.FunctionInstantaneous, 45),
			makeRecord(gombus.VIFReturnTemperature, gombus.FunctionInstantaneous, 35),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500),
			makeRecord(gombus.VIFFlowTemperature, gombus.FunctionInstantaneous, 45),
			makeRecord(gombus.VIFReturnTemperature, gombus.FunctionInstantaneous, 35),
			makeRecord(gombus.VIFTemperatureDifference, gombus.FunctionInstantaneous, 10),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500),
			makeRecord(gombus.VIFFlowTemperature, gombus.FunctionInstantaneous, 45),
			makeRecord(gombus.VIFReturnTemperature, gombus.FunctionInstantaneous, 35),
			makeRecord(gombus.VIFTemperatureDifference, gombus.FunctionInstantaneous, 10),
			makeRecord(gombus.VIFEnergyJoule, gombus.FunctionInstantaneous, 1e9),
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
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 100.5),
			makeRecord(gombus.VIFPowerW, gombus.FunctionMaximum, 5000),
			makeRecord(gombus.VIFOnTime, gombus.FunctionInstantaneous, 3600),
			makeRecord(gombus.VIFVolume, gombus.FunctionInstantaneous, 500),
			makeRecord(gombus.VIFFlowTemperature, gombus.FunctionInstantaneous, 45),
			makeRecord(gombus.VIFReturnTemperature, gombus.FunctionInstantaneous, 35),
			makeRecord(gombus.VIFTemperatureDifference, gombus.FunctionInstantaneous, 10),
			makeRecord(gombus.VIFEnergyJoule, gombus.FunctionInstantaneous, 1e9),
			makeRecord(gombus.VIFVolumeFlow, gombus.FunctionInstantaneous, 50.5),
			// ActualPower (unit 47, instant) missing
		},
	}
	_, err := GombusToDomain(frame)
	if err == nil {
		t.Error("GombusToDomain() should return error when ActualPower record is missing")
	}
}

func TestGombusToDomain_MissingRecordWrapsErrNoRecord(t *testing.T) {
	frame := &gombus.DecodedFrame{DataRecords: []gombus.DecodedDataRecord{}}
	_, err := GombusToDomain(frame)
	if !errors.Is(err, gombus.ErrNoRecord) {
		t.Errorf("GombusToDomain() error = %v, want wrapped gombus.ErrNoRecord", err)
	}
	if err == nil || !strings.Contains(err.Error(), "max flow") {
		t.Errorf("GombusToDomain() error = %v, want field name in message", err)
	}
}

func TestGombusToDomain_UndecodableValueSurfaces(t *testing.T) {
	valueErr := errors.New("BCD filler: value not available")
	rec := makeRecord(gombus.VIFVolumeFlow, gombus.FunctionMaximum, 0)
	rec.ValueErr = valueErr
	frame := &gombus.DecodedFrame{DataRecords: []gombus.DecodedDataRecord{rec}}

	_, err := GombusToDomain(frame)
	if !errors.Is(err, valueErr) {
		t.Errorf("GombusToDomain() error = %v, want the record's ValueErr, not a silent zero", err)
	}
}
