package qdb

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"testing"
	"time"

	qdbclient "github.com/questdb/go-questdb-client/v3"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// recordedRow captures one ILP row: table name, symbols, columns, and the At timestamp.
type recordedRow struct {
	table   string
	symbols map[string]string
	columns map[string]any
	at      time.Time
}

// mockLineSender implements qdb.LineSender and records every row so tests can
// assert what was actually written, not just that no error was returned.
type mockLineSender struct {
	flushErr error
	atErr    error

	current recordedRow
	rows    []recordedRow
}

func (m *mockLineSender) Table(name string) qdbclient.LineSender {
	m.current = recordedRow{
		table:   name,
		symbols: map[string]string{},
		columns: map[string]any{},
	}
	return m
}

func (m *mockLineSender) Symbol(name, value string) qdbclient.LineSender {
	m.current.symbols[name] = value
	return m
}

func (m *mockLineSender) Int64Column(name string, value int64) qdbclient.LineSender {
	m.current.columns[name] = value
	return m
}

func (m *mockLineSender) Long256Column(name string, value *big.Int) qdbclient.LineSender {
	m.current.columns[name] = value
	return m
}

func (m *mockLineSender) TimestampColumn(name string, value time.Time) qdbclient.LineSender {
	m.current.columns[name] = value
	return m
}

func (m *mockLineSender) Float64Column(name string, value float64) qdbclient.LineSender {
	m.current.columns[name] = value
	return m
}

func (m *mockLineSender) StringColumn(name, value string) qdbclient.LineSender {
	m.current.columns[name] = value
	return m
}

func (m *mockLineSender) BoolColumn(name string, value bool) qdbclient.LineSender {
	m.current.columns[name] = value
	return m
}

func (m *mockLineSender) At(_ context.Context, ts time.Time) error {
	if m.atErr != nil {
		return m.atErr
	}
	m.current.at = ts
	m.rows = append(m.rows, m.current)
	return nil
}

func (m *mockLineSender) AtNow(_ context.Context) error {
	if m.atErr != nil {
		return m.atErr
	}
	m.rows = append(m.rows, m.current)
	return nil
}

func (m *mockLineSender) Flush(_ context.Context) error { return m.flushErr }
func (m *mockLineSender) Close(_ context.Context) error { return nil }

func newTestDBClient() (*DBClient, *mockLineSender) {
	sender := &mockLineSender{}
	return &DBClient{
		sender: sender,
		logger: testLogger(),
	}, sender
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// requireRows fails the test unless exactly want rows were recorded.
func requireRows(t *testing.T, sender *mockLineSender, want int) []recordedRow {
	t.Helper()
	if len(sender.rows) != want {
		t.Fatalf("recorded %d rows, want %d", len(sender.rows), want)
	}
	return sender.rows
}

func assertTable(t *testing.T, row recordedRow, want string) {
	t.Helper()
	if row.table != want {
		t.Errorf("table = %q, want %q", row.table, want)
	}
}

func assertSymbol(t *testing.T, row recordedRow, name, want string) {
	t.Helper()
	got, ok := row.symbols[name]
	if !ok {
		t.Errorf("symbol %q not written", name)
		return
	}
	if got != want {
		t.Errorf("symbol %q = %q, want %q", name, got, want)
	}
}

func assertColumn(t *testing.T, row recordedRow, name string, want any) {
	t.Helper()
	got, ok := row.columns[name]
	if !ok {
		t.Errorf("column %q not written", name)
		return
	}
	if got != want {
		t.Errorf("column %q = %v (%T), want %v (%T)", name, got, got, want, want)
	}
}

func assertAt(t *testing.T, row recordedRow, want time.Time) {
	t.Helper()
	if !row.at.Equal(want) {
		t.Errorf("At timestamp = %v, want %v", row.at, want)
	}
}

// --- HeatTelegramStore tests ---

func TestNewQuestDbHeatTelegramWriter(t *testing.T) {
	client, _ := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat_table", testLogger())
	if store == nil {
		t.Error("NewQuestDBHeatTelegramWriter returned nil")
	}
}

func TestHeatTelegramStore_StoreHeatTelegram(t *testing.T) {
	client, sender := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat", testLogger())
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	telegram := domain.HeatTelegram{
		MeterID:        "test",
		SerialNo:       "12345",
		Joules:         3_000_000, // 3 MJ stored as energy=3
		Tforward:       45.0,
		Treturn:        35.0,
		Tdiff:          10.0,
		VolumeCm3:      500.0,
		SecondsCounter: 7200,
		MaxFlow:        100.0,
		MaxPower:       500,
		ActualPower:    200_000, // mW, stored as power=200 W
		ActualFlow:     50.0,
		Timestamp:      ts,
	}
	if err := store.StoreHeatTelegram(context.Background(), telegram); err != nil {
		t.Fatalf("StoreHeatTelegram() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "heat")
	assertSymbol(t, row, "device", "Multical test")
	assertSymbol(t, row, "serial", "12345")
	assertColumn(t, row, "power", int64(200))
	assertColumn(t, row, "energy", int64(3))
	assertColumn(t, row, "t1", 4500.0)
	assertColumn(t, row, "t2", 3500.0)
	assertColumn(t, row, "volume", int64(500_000))
	assertColumn(t, row, "hours", int64(2))
	assertColumn(t, row, "max_power", int64(5))
	assertColumn(t, row, "seconds", int64(7200))
	assertAt(t, row, ts)
}

func TestHeatTelegramStore_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat", testLogger())
	err := store.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestHeatTelegramStore_Close(t *testing.T) {
	client, _ := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat", testLogger())
	err := store.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- SolarWriter tests ---

func TestNewQuestDbSolarWriter(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	if writer == nil {
		t.Error("NewQuestDBSolarWriter returned nil")
	}
}

func TestSolarWriter_StoreEnvoySolarData_NoInverters(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	data := domain.EnvoySolarData{
		EnvoySerial:  "12345",
		ProductionWh: 1000.5,
		Watt:         250.0,
		PanelCount:   10,
		ReadingTime:  ts,
		Inverters:    []domain.InverterDetails{},
	}
	if err := writer.StoreEnvoySolarData(context.Background(), data); err != nil {
		t.Fatalf("StoreEnvoySolarData() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "solar")
	assertSymbol(t, row, "EnvoySerialNumber", "12345")
	assertColumn(t, row, "ProductionWattHours", 1000.5)
	assertColumn(t, row, "ProductionWatt", 250.0)
	assertColumn(t, row, "PanelCount", int64(10))
	assertAt(t, row, ts)
}

func TestSolarWriter_StoreEnvoySolarData_WithInverters(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	readingTime := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reportTime := time.Date(2026, 7, 1, 11, 59, 0, 0, time.UTC)
	data := domain.EnvoySolarData{
		EnvoySerial:  "serial-1",
		ProductionWh: 5000.0,
		Watt:         300.0,
		PanelCount:   20,
		ReadingTime:  readingTime,
		Inverters: []domain.InverterDetails{
			{
				SerialNumber:      "inv-1",
				Chaneid:           1,
				Operating:         true,
				Communicating:     true,
				Producing:         true,
				Phase:             "A",
				DeviceStatus:      []string{"ok", "envoy.global.ok"},
				ReportTime:        reportTime,
				LastReportedWatts: 250,
				MaxReportWatts:    300,
			},
		},
	}
	if err := writer.StoreEnvoySolarData(context.Background(), data); err != nil {
		t.Fatalf("StoreEnvoySolarData() with inverters unexpected error: %v", err)
	}

	rows := requireRows(t, sender, 2)
	assertTable(t, rows[0], "solar")
	assertSymbol(t, rows[0], "EnvoySerialNumber", "serial-1")

	inv := rows[1]
	assertTable(t, inv, "solar_inverters")
	assertSymbol(t, inv, "InverterSerialNumber", "inv-1")
	assertColumn(t, inv, "EnvoySerialNumber", "serial-1")
	assertColumn(t, inv, "ChannelID", int64(1))
	assertColumn(t, inv, "Operating", true)
	assertColumn(t, inv, "Phase", "A")
	assertColumn(t, inv, "Status", "ok,envoy.global.ok")
	assertColumn(t, inv, "Watts", int64(250))
	assertColumn(t, inv, "PeakWatts", int64(300))
	assertAt(t, inv, reportTime)
}

func TestSolarWriter_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	err := writer.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestSolarWriter_Close(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	err := writer.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- GridStore tests ---

func TestNewQuestDbGridWriter(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	if writer == nil {
		t.Error("NewQuestDBGridWriter returned nil")
	}
}

func TestGridStore_StoreGridTelegram(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	telegram := domain.GridTelegram{
		Time:             ts,
		MeterMerkType:    "ISK",
		Serienummer:      "0012345678",
		UsageCounter1:    100.5,
		UsageCounter2:    200.3,
		OutputCounter1:   10.1,
		OutputCounter2:   5.0,
		TotalPowerUsage:  500,
		TotalPowerOutput: 42,
		VoltageP1:        230.1,
		VoltageP2:        229.8,
		VoltageP3:        231.0,
		CurrentP1:        2,
		CurrentP2:        1,
		CurrentP3:        3,
		AvgDemand:        2351,
		MaxDemandMonth:   2589,
		MaxDemandMonthAt: ts.Add(-2 * time.Hour),
	}
	if err := writer.StoreGridTelegram(context.Background(), telegram); err != nil {
		t.Fatalf("StoreGridTelegram() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "grid")
	assertSymbol(t, row, "MeterMerkType", "ISK")
	assertSymbol(t, row, "Serienummer", "0012345678")
	assertColumn(t, row, "UsageCounter1", 100.5)
	assertColumn(t, row, "UsageCounter2", 200.3)
	assertColumn(t, row, "OutputCounter1", 10.1)
	assertColumn(t, row, "TotalPowerUsage", int64(500))
	assertColumn(t, row, "TotalPowerOutput", int64(42))
	assertColumn(t, row, "VoltageP1", 230.1)
	assertColumn(t, row, "VoltageP3", 231.0)
	assertColumn(t, row, "CurrentP1", int64(2))
	assertColumn(t, row, "CurrentP3", int64(3))
	assertColumn(t, row, "AvgDemand", int64(2351))
	assertColumn(t, row, "MaxDemandMonth", int64(2589))
	assertColumn(t, row, "MaxDemandMonthAt", ts.Add(-2*time.Hour))
	assertAt(t, row, ts)
}

// TestGridStore_ZeroDemandTimeOmitted asserts that an unset MaxDemandMonthAt
// writes no timestamp column: the zero time is not representable in ILP.
func TestGridStore_ZeroDemandTimeOmitted(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if err := writer.StoreGridTelegram(context.Background(), domain.GridTelegram{Time: ts}); err != nil {
		t.Fatalf("StoreGridTelegram() unexpected error: %v", err)
	}
	row := requireRows(t, sender, 1)[0]
	assertColumn(t, row, "AvgDemand", int64(0))
	if _, present := row.columns["MaxDemandMonthAt"]; present {
		t.Error("MaxDemandMonthAt column written for zero time")
	}
}

func TestGridStore_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	err := writer.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestGridStore_Close(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	err := writer.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- QuestDBGasWriter tests ---

func TestNewQuestDBGasWriter(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBGasWriter(client, "gas", testLogger())
	if writer == nil {
		t.Error("NewQuestDBGasWriter returned nil")
	}
}

//nolint:dupl // gas, water and thermal writer tests share the same shape but cover distinct domain types
func TestQuestDBGasWriter_StoreGasReading(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBGasWriter(client, "gas", testLogger())
	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	receivedAt := capturedAt.Add(90 * time.Second)
	reading := domain.GasReading{
		CapturedAt: capturedAt,
		ReceivedAt: receivedAt,
		Channel:    1,
		DeviceType: domain.DeviceTypeGas,
		SerialNo:   "4730303339",
		ReadingM3:  1234.567,
	}
	if err := writer.StoreGasReading(context.Background(), reading); err != nil {
		t.Fatalf("StoreGasReading() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "gas")
	assertSymbol(t, row, "serial_no", "4730303339")
	assertColumn(t, row, "channel", int64(1))
	assertColumn(t, row, "device_type", int64(domain.DeviceTypeGas))
	assertColumn(t, row, "reading_m3", 1234.567)
	assertColumn(t, row, "received_at", receivedAt)
	assertAt(t, row, capturedAt)
}

func TestQuestDBGasWriter_StoreGasReading_WriteError(t *testing.T) {
	client := &DBClient{
		sender: &mockLineSender{atErr: errTest},
		logger: testLogger(),
	}
	writer := NewQuestDBGasWriter(client, "gas", testLogger())
	if err := writer.StoreGasReading(context.Background(), domain.GasReading{}); err == nil {
		t.Error("StoreGasReading should return error when sender fails")
	}
}

func TestQuestDBGasWriter_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBGasWriter(client, "gas", testLogger())
	if err := writer.Flush(context.Background()); err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestQuestDBGasWriter_Close(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBGasWriter(client, "gas", testLogger())
	if err := writer.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- QuestDBWaterWriter tests ---

func TestNewQuestDBWaterWriter(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBWaterWriter(client, "water", testLogger())
	if writer == nil {
		t.Error("NewQuestDBWaterWriter returned nil")
	}
}

//nolint:dupl // gas, water and thermal writer tests share the same shape but cover distinct domain types
func TestQuestDBWaterWriter_StoreWaterReading(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBWaterWriter(client, "water", testLogger())
	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	receivedAt := capturedAt.Add(90 * time.Second)
	reading := domain.WaterReading{
		CapturedAt: capturedAt,
		ReceivedAt: receivedAt,
		Channel:    2,
		DeviceType: domain.DeviceTypeWater,
		SerialNo:   "3853414731",
		ReadingM3:  872.234,
	}
	if err := writer.StoreWaterReading(context.Background(), reading); err != nil {
		t.Fatalf("StoreWaterReading() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "water")
	assertSymbol(t, row, "serial_no", "3853414731")
	assertColumn(t, row, "channel", int64(2))
	assertColumn(t, row, "device_type", int64(domain.DeviceTypeWater))
	assertColumn(t, row, "reading_m3", 872.234)
	assertColumn(t, row, "received_at", receivedAt)
	assertAt(t, row, capturedAt)
}

func TestQuestDBWaterWriter_StoreWaterReading_WriteError(t *testing.T) {
	client := &DBClient{
		sender: &mockLineSender{atErr: errTest},
		logger: testLogger(),
	}
	writer := NewQuestDBWaterWriter(client, "water", testLogger())
	if err := writer.StoreWaterReading(context.Background(), domain.WaterReading{}); err == nil {
		t.Error("StoreWaterReading should return error when sender fails")
	}
}

func TestQuestDBWaterWriter_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBWaterWriter(client, "water", testLogger())
	if err := writer.Flush(context.Background()); err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestQuestDBWaterWriter_Close(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBWaterWriter(client, "water", testLogger())
	if err := writer.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- QuestDBThermalWriter tests ---

func TestNewQuestDBThermalWriter(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBThermalWriter(client, "thermal", testLogger())
	if writer == nil {
		t.Error("NewQuestDBThermalWriter returned nil")
	}
}

//nolint:dupl // gas, water and thermal writer tests share the same shape but cover distinct domain types
func TestQuestDBThermalWriter_StoreThermalReading(t *testing.T) {
	client, sender := newTestDBClient()
	writer := NewQuestDBThermalWriter(client, "thermal", testLogger())
	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	receivedAt := capturedAt.Add(90 * time.Second)
	reading := domain.ThermalReading{
		CapturedAt: capturedAt,
		ReceivedAt: receivedAt,
		Channel:    3,
		DeviceType: domain.DeviceTypeHeat,
		SerialNo:   "3253414735",
		ReadingGJ:  12.345,
	}
	if err := writer.StoreThermalReading(context.Background(), reading); err != nil {
		t.Fatalf("StoreThermalReading() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "thermal")
	assertSymbol(t, row, "serial_no", "3253414735")
	assertColumn(t, row, "channel", int64(3))
	assertColumn(t, row, "device_type", int64(domain.DeviceTypeHeat))
	assertColumn(t, row, "reading_gj", 12.345)
	assertColumn(t, row, "received_at", receivedAt)
	assertAt(t, row, capturedAt)
}

func TestQuestDBThermalWriter_StoreThermalReading_WriteError(t *testing.T) {
	client := &DBClient{
		sender: &mockLineSender{atErr: errTest},
		logger: testLogger(),
	}
	writer := NewQuestDBThermalWriter(client, "thermal", testLogger())
	if err := writer.StoreThermalReading(context.Background(), domain.ThermalReading{}); err == nil {
		t.Error("StoreThermalReading should return error when sender fails")
	}
}

func TestQuestDBThermalWriter_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBThermalWriter(client, "thermal", testLogger())
	if err := writer.Flush(context.Background()); err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestQuestDBThermalWriter_Close(t *testing.T) {
	client, _ := newTestDBClient()
	writer := NewQuestDBThermalWriter(client, "thermal", testLogger())
	if err := writer.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- DucoQuestDBRepository tests ---

func TestNewDucoQuestDBRepository(t *testing.T) {
	client, _ := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	if repo == nil {
		t.Error("NewDucoQuestDBRepository returned nil")
	}
}

func TestDucoQuestDBRepository_StoreBoxStatus(t *testing.T) {
	client, sender := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	boxStatus := domain.DucoBoxStatus{
		General: domain.General{RFHomeID: "home1", Time: 12345},
		EnergyCalib: domain.EnergyCalib{
			CalibKinZone1: 100,
			CalibState:    "OK",
		},
		EnergyFan: domain.EnergyFan{
			ExhaustFanSpeed: 1200,
		},
		EnergyInfo: domain.EnergyInfo{
			FrostProtState: false,
			TempEHA:        200,
		},
		WeatherStation: domain.WeatherStation{Present: true},
	}
	if err := repo.StoreBoxStatus(context.Background(), boxStatus); err != nil {
		t.Fatalf("StoreBoxStatus() unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "ventilation_box_general")
	assertSymbol(t, row, "rfHomeId", "home1")
	assertColumn(t, row, "CalibKinZone1", int64(100))
	assertColumn(t, row, "CalibState", "OK")
	assertColumn(t, row, "ExhaustFanSpeed", int64(1200))
	assertColumn(t, row, "FrostProtState", false)
	assertColumn(t, row, "TempEHA", int64(200))
	assertColumn(t, row, "WeatherStationPresent", true)
}

func TestDucoQuestDBRepository_StoreNodeData_RFSensor(t *testing.T) {
	client, sender := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	node := domain.DucoRFSensorStatus{
		BaseDucoNodeStatus: domain.BaseDucoNodeStatus{
			Node:     3,
			DevType:  "UCCO2",
			Location: "living room",
		},
		Co2:  800.0,
		Temp: 21.5,
		Rh:   55.0,
	}
	if err := repo.StoreNodeData(context.Background(), node); err != nil {
		t.Fatalf("StoreNodeData(RFSensor) unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "ventilation_node")
	assertSymbol(t, row, "node", "3")
	assertSymbol(t, row, "device", "UCCO2")
	assertSymbol(t, row, "location", "living room")
	assertColumn(t, row, "co2", 800.0)
	assertColumn(t, row, "temp", 21.5)
	assertColumn(t, row, "humidity", 55.0)
}

func TestDucoQuestDBRepository_StoreNodeData_BoxNode(t *testing.T) {
	client, sender := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	node := domain.DucoNodeBoxStatus{
		BaseDucoNodeStatus: domain.BaseDucoNodeStatus{
			Node:    1,
			DevType: "BOX",
		},
		Trgt: 100,
		Actl: 80,
		Co2:  600.0,
	}
	if err := repo.StoreNodeData(context.Background(), node); err != nil {
		t.Fatalf("StoreNodeData(BoxNode) unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "ventilation_box_node")
	assertSymbol(t, row, "node", "1")
	assertSymbol(t, row, "device", "BOX")
	assertColumn(t, row, "trgt", int64(100))
	assertColumn(t, row, "actl", int64(80))
	assertColumn(t, row, "co2", 600.0)
}

func TestDucoQuestDBRepository_StoreNodeData_Valve(t *testing.T) {
	client, sender := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	node := domain.DucoNodeBoxValveStatus{
		BaseDucoNodeStatus: domain.BaseDucoNodeStatus{
			Node:    2,
			DevType: "VLV",
		},
		Trgt: 50,
		Actl: 45,
	}
	if err := repo.StoreNodeData(context.Background(), node); err != nil {
		t.Fatalf("StoreNodeData(Valve) unexpected error: %v", err)
	}

	row := requireRows(t, sender, 1)[0]
	assertTable(t, row, "ventilation_valve")
	assertSymbol(t, row, "node", "2")
	assertSymbol(t, row, "device", "VLV")
	assertColumn(t, row, "trgt", int64(50))
	assertColumn(t, row, "actl", int64(45))
}

type unknownQDBNode struct{ domain.BaseDucoNodeStatus }

func TestDucoQuestDBRepository_StoreNodeData_Unknown(t *testing.T) {
	client, sender := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	if err := repo.StoreNodeData(context.Background(), unknownQDBNode{}); err != nil {
		t.Errorf("StoreNodeData(unknown) should not error: %v", err)
	}
	requireRows(t, sender, 0)
}

func TestDucoQuestDBRepository_Flush(t *testing.T) {
	client, _ := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	err := repo.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestDucoQuestDBRepository_Close(t *testing.T) {
	client, _ := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	err := repo.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

func TestDBClient_Close(_ *testing.T) {
	// Close should not panic.
	client, _ := newTestDBClient()
	client.Close()
}

func TestDBClient_Close_FlushError(_ *testing.T) {
	client := &DBClient{
		sender: &mockLineSender{flushErr: errTest},
		logger: testLogger(),
	}
	// Should not panic; errors are logged
	client.Close()
}

func TestDBClient_Close_SenderCloseError(_ *testing.T) {
	client := &DBClient{
		sender: &mockLineSenderWithCloseErr{},
		logger: testLogger(),
	}
	client.Close()
}

var errTest = errors.New("test error")

// mockLineSenderWithCloseErr fails on Close but not on Flush.
type mockLineSenderWithCloseErr struct {
	mockLineSender
}

func (m *mockLineSenderWithCloseErr) Close(_ context.Context) error {
	return errTest
}

func TestSolarWriter_StoreEnvoySolarData_WriteError(t *testing.T) {
	client := &DBClient{
		sender: &mockLineSender{atErr: errTest},
		logger: testLogger(),
	}
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	data := domain.EnvoySolarData{
		EnvoySerial:  "serial",
		ProductionWh: 1000.0,
		Watt:         250.0,
		PanelCount:   5,
		Inverters: []domain.InverterDetails{
			{SerialNumber: "inv1", Chaneid: 1},
		},
	}
	err := writer.StoreEnvoySolarData(context.Background(), data)
	if err == nil {
		t.Error("StoreEnvoySolarData should return error when sender fails")
	}
}
