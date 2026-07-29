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

// mockLineSender implements qdb.LineSender for testing.
type mockLineSender struct {
	flushErr error
	atErr    error
}

func (m *mockLineSender) Table(_ string) qdbclient.LineSender     { return m }
func (m *mockLineSender) Symbol(_, _ string) qdbclient.LineSender { return m }
func (m *mockLineSender) Int64Column(_ string, _ int64) qdbclient.LineSender {
	return m
}

func (m *mockLineSender) Long256Column(_ string, _ *big.Int) qdbclient.LineSender {
	return m
}

func (m *mockLineSender) TimestampColumn(_ string, _ time.Time) qdbclient.LineSender {
	return m
}

func (m *mockLineSender) Float64Column(_ string, _ float64) qdbclient.LineSender {
	return m
}
func (m *mockLineSender) StringColumn(_, _ string) qdbclient.LineSender { return m }
func (m *mockLineSender) BoolColumn(_ string, _ bool) qdbclient.LineSender {
	return m
}
func (m *mockLineSender) At(_ context.Context, _ time.Time) error { return m.atErr }
func (m *mockLineSender) AtNow(_ context.Context) error           { return m.atErr }
func (m *mockLineSender) Flush(_ context.Context) error           { return m.flushErr }
func (m *mockLineSender) Close(_ context.Context) error           { return nil }

func newTestDBClient() *DBClient {
	return &DBClient{
		sender: &mockLineSender{},
		logger: testLogger(),
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// --- HeatTelegramStore tests ---

func TestNewQuestDbHeatTelegramWriter(t *testing.T) {
	client := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat_table", testLogger())
	if store == nil {
		t.Error("NewQuestDBHeatTelegramWriter returned nil")
	}
}

func TestHeatTelegramStore_StoreHeatTelegram(t *testing.T) {
	client := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat", testLogger())
	telegram := domain.HeatTelegram{
		MeterID:        "test",
		SerialNo:       "12345",
		Joules:         1000000,
		Tforward:       45.0,
		Treturn:        35.0,
		Tdiff:          10.0,
		VolumeCm3:      500.0,
		SecondsCounter: 3600,
		MaxFlow:        100.0,
		MaxPower:       500,
		ActualPower:    200,
		ActualFlow:     50.0,
		Timestamp:      time.Now(),
	}
	err := store.StoreHeatTelegram(context.Background(), telegram)
	if err != nil {
		t.Errorf("StoreHeatTelegram() unexpected error: %v", err)
	}
}

func TestHeatTelegramStore_Flush(t *testing.T) {
	client := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat", testLogger())
	err := store.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestHeatTelegramStore_Close(t *testing.T) {
	client := newTestDBClient()
	store := NewQuestDBHeatTelegramWriter(client, "heat", testLogger())
	err := store.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- SolarWriter tests ---

func TestNewQuestDbSolarWriter(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	if writer == nil {
		t.Error("NewQuestDBSolarWriter returned nil")
	}
}

func TestSolarWriter_StoreEnvoySolarData_NoInverters(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	data := domain.EnvoySolarData{
		EnvoySerial:  "12345",
		ProductionWh: 1000.5,
		Watt:         250.0,
		PanelCount:   10,
		ReadingTime:  time.Now(),
		Inverters:    []domain.InverterDetails{},
	}
	err := writer.StoreEnvoySolarData(context.Background(), data)
	if err != nil {
		t.Errorf("StoreEnvoySolarData() unexpected error: %v", err)
	}
}

func TestSolarWriter_StoreEnvoySolarData_WithInverters(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	data := domain.EnvoySolarData{
		EnvoySerial:  "serial-1",
		ProductionWh: 5000.0,
		Watt:         300.0,
		PanelCount:   20,
		ReadingTime:  time.Now(),
		Inverters: []domain.InverterDetails{
			{
				SerialNumber:      "inv-1",
				Chaneid:           1,
				Operating:         true,
				Communicating:     true,
				Producing:         true,
				Phase:             "A",
				DeviceStatus:      []string{"ok"},
				ReportTime:        time.Now(),
				LastReportedWatts: 250,
				MaxReportWatts:    300,
			},
		},
	}
	err := writer.StoreEnvoySolarData(context.Background(), data)
	if err != nil {
		t.Errorf("StoreEnvoySolarData() with inverters unexpected error: %v", err)
	}
}

func TestSolarWriter_Flush(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	err := writer.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestSolarWriter_Close(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBSolarWriter(client, "solar", testLogger())
	err := writer.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- GridStore tests ---

func TestNewQuestDbGridWriter(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	if writer == nil {
		t.Error("NewQuestDBGridWriter returned nil")
	}
}

func TestGridStore_StoreGridTelegram(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	telegram := domain.GridTelegram{
		Time:             time.Now(),
		MeterMerkType:    "ISK",
		Serienummer:      "0012345678",
		UsageCounter1:    100.5,
		UsageCounter2:    200.3,
		OutputCounter1:   10.1,
		OutputCounter2:   5.0,
		TotalPowerUsage:  500,
		TotalPowerOutput: 0,
		VoltageP1:        230.1,
		VoltageP2:        229.8,
		VoltageP3:        231.0,
		CurrentP1:        2,
		CurrentP2:        1,
		CurrentP3:        2,
	}
	err := writer.StoreGridTelegram(context.Background(), telegram)
	if err != nil {
		t.Errorf("StoreGridTelegram() unexpected error: %v", err)
	}
}

func TestGridStore_Flush(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	err := writer.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestGridStore_Close(t *testing.T) {
	client := newTestDBClient()
	writer := NewQuestDBGridWriter(client, "grid", testLogger())
	err := writer.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

// --- DucoQuestDBRepository tests ---

func TestNewDucoQuestDBRepository(t *testing.T) {
	client := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	if repo == nil {
		t.Error("NewDucoQuestDBRepository returned nil")
	}
}

func TestDucoQuestDBRepository_StoreBoxStatus(t *testing.T) {
	client := newTestDBClient()
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
	err := repo.StoreBoxStatus(context.Background(), boxStatus)
	if err != nil {
		t.Errorf("StoreBoxStatus() unexpected error: %v", err)
	}
}

func TestDucoQuestDBRepository_StoreNodeData_RFSensor(t *testing.T) {
	client := newTestDBClient()
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
	err := repo.StoreNodeData(context.Background(), node)
	if err != nil {
		t.Errorf("StoreNodeData(RFSensor) unexpected error: %v", err)
	}
}

func TestDucoQuestDBRepository_StoreNodeData_BoxNode(t *testing.T) {
	client := newTestDBClient()
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
	err := repo.StoreNodeData(context.Background(), node)
	if err != nil {
		t.Errorf("StoreNodeData(BoxNode) unexpected error: %v", err)
	}
}

func TestDucoQuestDBRepository_StoreNodeData_Valve(t *testing.T) {
	client := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	node := domain.DucoNodeBoxValveStatus{
		BaseDucoNodeStatus: domain.BaseDucoNodeStatus{
			Node:    2,
			DevType: "VLV",
		},
		Trgt: 50,
		Actl: 45,
	}
	err := repo.StoreNodeData(context.Background(), node)
	if err != nil {
		t.Errorf("StoreNodeData(Valve) unexpected error: %v", err)
	}
}

type unknownQDBNode struct{ domain.BaseDucoNodeStatus }

func TestDucoQuestDBRepository_StoreNodeData_Unknown(t *testing.T) {
	client := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	err := repo.StoreNodeData(context.Background(), unknownQDBNode{})
	if err != nil {
		t.Errorf("StoreNodeData(unknown) should not error: %v", err)
	}
}

func TestDucoQuestDBRepository_Flush(t *testing.T) {
	client := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	err := repo.Flush(context.Background())
	if err != nil {
		t.Errorf("Flush() unexpected error: %v", err)
	}
}

func TestDucoQuestDBRepository_Close(t *testing.T) {
	client := newTestDBClient()
	repo := NewDucoQuestDBRepository(client, "ventilation", testLogger())
	err := repo.Close()
	if err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
}

func TestDBClient_Close(_ *testing.T) {
	// Close should not panic.
	client := newTestDBClient()
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
