package sqlsink_test

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func newHeatStore(t *testing.T, dc dialectCase) (*sqlsink.HeatStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testDB(t, dc.dialect)
	expectMigrationAlreadyApplied(mock, dc, dc.prefix+"_heat_heat")
	store, err := sqlsink.NewHeatStore(context.Background(), db, "heat", testLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	return store, mock
}

func newDucoStore(t *testing.T, dc dialectCase) (*sqlsink.DucoStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testDB(t, dc.dialect)
	expectMigrationAlreadyApplied(mock, dc, dc.prefix+"_duco_duco")
	store, err := sqlsink.NewDucoStore(context.Background(), db, "duco", testLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	return store, mock
}

// TestHeatStore_InsertArgs asserts the exact argument order of the heat insert
// for both placeholder styles, so a column/value transposition fails the test.
func TestHeatStore_InsertArgs(t *testing.T) {
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	telegram := domain.HeatTelegram{
		Timestamp:      ts,
		MeterID:        "m1",
		SerialNo:       "s1",
		Joules:         2_000_000_000,
		VolumeCm3:      9.5,
		SecondsCounter: 3600,
		Tforward:       70.5,
		Treturn:        40.25,
		Tdiff:          30.25,
		ActualPower:    1200,
		MaxPower:       5000,
		MaxFlow:        1.5,
	}
	for _, dc := range []dialectCase{dialectCases()[0], dialectCases()[1]} {
		t.Run(dc.prefix, func(t *testing.T) {
			store, mock := newHeatStore(t, dc)
			mock.ExpectExec("INSERT INTO heat").
				WithArgs(ts, "m1", "s1", int64(1200), 2.0, 70.5, 40.25, 30.25, 9.5, int64(3600), 1.5, int64(5000)).
				WillReturnResult(sqlmock.NewResult(1, 1))
			if err := store.StoreHeatTelegram(context.Background(), telegram); err != nil {
				t.Errorf("StoreHeatTelegram: %v", err)
			}
			if metErr := mock.ExpectationsWereMet(); metErr != nil {
				t.Error(metErr)
			}
		})
	}
}

func TestGridStore_InsertArgs(t *testing.T) {
	dc := dialectCases()[0]
	db, mock := testDB(t, dc.dialect)
	expectMigrationAlreadyApplied(mock, dc, "postgres_grid_grid")
	store, err := sqlsink.NewGridStore(context.Background(), db, "grid", testLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	telegram := domain.GridTelegram{
		Time: ts, MeterMerkType: "ISK", Serienummer: "sn1",
		UsageCounter1: 1.1, UsageCounter2: 2.2, OutputCounter1: 3.3, OutputCounter2: 4.4,
		TotalPowerUsage: 500, TotalPowerOutput: 600,
		BrownoutsP1: 1, BrownoutsP2: 2, BrownoutsP3: 3,
		SpikesP1: 4, SpikesP2: 5, SpikesP3: 6,
		VoltageP1: 230.1, VoltageP2: 231.2, VoltageP3: 232.3,
		CurrentP1: 7, CurrentP2: 8, CurrentP3: 9,
		PowerUsageP1: 10, PowerUsageP2: 11, PowerUsageP3: 12,
		PowerOutputP1: 13, PowerOutputP2: 14, PowerOutputP3: 15,
	}
	mock.ExpectExec("INSERT INTO grid").
		WithArgs(
			ts, "ISK", "sn1",
			1.1, 2.2, 3.3, 4.4,
			500, 600,
			1, 2, 3,
			4, 5, 6,
			230.1, 231.2, 232.3,
			7, 8, 9,
			10, 11, 12,
			13, 14, 15,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if storeErr := store.StoreGridTelegram(context.Background(), telegram); storeErr != nil {
		t.Errorf("StoreGridTelegram: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func newGasStore(t *testing.T, dc dialectCase) (*sqlsink.GasStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testDB(t, dc.dialect)
	expectMigrationAlreadyApplied(mock, dc, dc.prefix+"_gas_gas")
	store, err := sqlsink.NewGasStore(context.Background(), db, "gas", testLogger())
	if err != nil {
		t.Fatalf("NewGasStore: %v", err)
	}
	return store, mock
}

// TestGasStore_InsertArgs asserts the exact argument order of the gas insert
// for both placeholder styles, so a column/value transposition fails the test.
func TestGasStore_InsertArgs(t *testing.T) {
	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	receivedAt := capturedAt.Add(2 * time.Minute)
	reading := domain.GasReading{
		CapturedAt: capturedAt,
		ReceivedAt: receivedAt,
		Channel:    1,
		DeviceType: domain.DeviceTypeGas,
		SerialNo:   "sn-gas-1",
		ReadingM3:  1234.567,
	}
	for _, dc := range []dialectCase{dialectCases()[0], dialectCases()[1]} {
		t.Run(dc.prefix, func(t *testing.T) {
			store, mock := newGasStore(t, dc)
			mock.ExpectExec("INSERT INTO gas").
				WithArgs(capturedAt, receivedAt, 1, domain.DeviceTypeGas, "sn-gas-1", 1234.567).
				WillReturnResult(sqlmock.NewResult(1, 1))
			if err := store.StoreGasReading(context.Background(), reading); err != nil {
				t.Errorf("StoreGasReading: %v", err)
			}
			if metErr := mock.ExpectationsWereMet(); metErr != nil {
				t.Error(metErr)
			}
		})
	}
}

func TestGasStore_FlushAndCloseAreNoOps(t *testing.T) {
	store, mock := newGasStore(t, dialectCases()[0])
	if err := store.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestSolarStore_InsertArgs(t *testing.T) {
	dc := dialectCases()[0]
	db, mock := testDB(t, dc.dialect)
	expectMigrationAlreadyApplied(mock, dc, "postgres_solar_solar")
	store, err := sqlsink.NewSolarStore(context.Background(), db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reportTS := ts.Add(time.Minute)
	data := domain.EnvoySolarData{
		ReadingTime: ts, ProductionWh: 123.4, Watt: 567.8, PanelCount: 10, EnvoySerial: "e1",
		Inverters: []domain.InverterDetails{{
			SerialNumber: "inv1", Chaneid: 3, Producing: true, Operating: true,
			Phase: "L1", Communicating: false, ReportTime: reportTS,
			LastReportedWatts: 250, MaxReportWatts: 300,
		}},
	}
	mock.ExpectExec("INSERT INTO solar ").
		WithArgs(ts, "e1", 123.4, 567.8, 10).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO solar_inverters").
		WithArgs(reportTS, "e1", "inv1", 3, true, false, true, "L1", 250, 300).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if storeErr := store.StoreEnvoySolarData(context.Background(), data); storeErr != nil {
		t.Errorf("StoreEnvoySolarData: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestSolarStore_InverterErrorContinues(t *testing.T) {
	dc := dialectCases()[0]
	db, mock := testDB(t, dc.dialect)
	expectMigrationAlreadyApplied(mock, dc, "postgres_solar_solar")
	store, err := sqlsink.NewSolarStore(context.Background(), db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	data := domain.EnvoySolarData{
		EnvoySerial: "e1",
		Inverters: []domain.InverterDetails{
			{SerialNumber: "inv1"},
			{SerialNumber: "inv2"},
		},
	}
	mock.ExpectExec("INSERT INTO solar ").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnError(errTest)
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnResult(sqlmock.NewResult(1, 1))
	if storeErr := store.StoreEnvoySolarData(context.Background(), data); storeErr == nil {
		t.Error("expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDucoStore_BoxStatusInsertArgs(t *testing.T) {
	store, mock := newDucoStore(t, dialectCases()[0])
	status := domain.DucoBoxStatus{
		EnergyFan: domain.EnergyFan{
			ExhaustFanSpeed: 1200, SupplyFanSpeed: 1100,
			ExhaustFanPwmPercentage: 45, SupplyFanPwmPercentage: 40,
		},
		EnergyInfo: domain.EnergyInfo{
			BypassStatus: 1, FilterRemainingTime: 90, FrostProtState: true,
			TempEHA: 18, TempETA: 20, TempODA: 5, TempSUP: 17,
		},
		General:        domain.General{InstallerState: "ok", RFHomeID: "rf1"},
		WeatherStation: domain.WeatherStation{Present: true},
	}
	mock.ExpectExec("INSERT INTO duco_box_general").
		WithArgs(
			sqlmock.AnyArg(), "rf1",
			1200, 1100, 45, 40,
			1, 90, true,
			18, 20, 5, 17,
			"ok", true,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := store.StoreBoxStatus(context.Background(), status); err != nil {
		t.Errorf("StoreBoxStatus: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDucoStore_NodeInsertArgs(t *testing.T) {
	store, mock := newDucoStore(t, dialectCases()[0])
	base := domain.BaseDucoNodeStatus{
		Node: 2, DevType: "SENS", Netw: "rf", Location: "hall", State: "auto",
		Cntdwn: 1, Mode: "AUTO", Ovrl: 3, Snsr: 4, Cerr: 5,
		Swversion: "1.2", Serialnb: "sn2", Show: 6, Link: 7,
	}

	t.Run("rf sensor", func(t *testing.T) {
		node := domain.DucoRFSensorStatus{
			BaseDucoNodeStatus: base,
			Temp:               21.5, Co2: 450.0, Rh: 55.5, RssiN2M: -60, HopVia: 1, RssiN2H: -70,
		}
		mock.ExpectExec("INSERT INTO duco_node").
			WithArgs(
				sqlmock.AnyArg(), 2, "hall", "SENS", "rf", "sn2", "1.2",
				"AUTO", "auto", 450.0, 21.5, 55.5, -60, -70, 1,
				4, 5, 3, 1, 6, 7,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		if err := store.StoreNodeData(context.Background(), node); err != nil {
			t.Errorf("StoreNodeData: %v", err)
		}
	})

	t.Run("box node", func(t *testing.T) {
		node := domain.DucoNodeBoxStatus{
			BaseDucoNodeStatus: base,
			Trgt:               50, Actl: 48, Rh: 51.0, Temp: 20.5, Co2: 400.0,
		}
		mock.ExpectExec("INSERT INTO duco_box_node").
			WithArgs(
				sqlmock.AnyArg(), 2, "hall", "SENS", "rf", "sn2", "1.2",
				"AUTO", "auto", 50, 48, 400.0, 20.5, 51.0,
				4, 5, 3, 1, 6, 7,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		if err := store.StoreNodeData(context.Background(), node); err != nil {
			t.Errorf("StoreNodeData: %v", err)
		}
	})

	t.Run("valve node", func(t *testing.T) {
		node := domain.DucoNodeBoxValveStatus{BaseDucoNodeStatus: base, Trgt: 30, Actl: 28}
		mock.ExpectExec("INSERT INTO duco_valve").
			WithArgs(
				sqlmock.AnyArg(), 2, "hall", "SENS", "rf", "sn2", "1.2",
				"AUTO", "auto", 30, 28,
				4, 5, 3, 1, 6, 7,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))
		if err := store.StoreNodeData(context.Background(), node); err != nil {
			t.Errorf("StoreNodeData: %v", err)
		}
	})

	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDucoStore_UnknownNodeTypeSkipped(t *testing.T) {
	store, mock := newDucoStore(t, dialectCases()[0])
	if err := store.StoreNodeData(context.Background(), unknownNode{}); err != nil {
		t.Errorf("StoreNodeData: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

type unknownNode struct{}

func (unknownNode) NodeDevType() string { return "UNKN" }

func TestStore_ErrorPaths(t *testing.T) {
	dc := dialectCases()[0]

	t.Run("heat", func(t *testing.T) {
		store, mock := newHeatStore(t, dc)
		mock.ExpectExec("INSERT INTO heat").WillReturnError(errTest)
		if err := store.StoreHeatTelegram(context.Background(), domain.HeatTelegram{}); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("grid", func(t *testing.T) {
		db, mock := testDB(t, dc.dialect)
		expectMigrationAlreadyApplied(mock, dc, "postgres_grid_grid")
		store, err := sqlsink.NewGridStore(context.Background(), db, "grid", testLogger())
		if err != nil {
			t.Fatalf("NewGridStore: %v", err)
		}
		mock.ExpectExec("INSERT INTO grid").WillReturnError(errTest)
		if storeErr := store.StoreGridTelegram(context.Background(), domain.GridTelegram{}); storeErr == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("gas", func(t *testing.T) {
		store, mock := newGasStore(t, dc)
		mock.ExpectExec("INSERT INTO gas").WillReturnError(errTest)
		if err := store.StoreGasReading(context.Background(), domain.GasReading{}); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("solar main", func(t *testing.T) {
		db, mock := testDB(t, dc.dialect)
		expectMigrationAlreadyApplied(mock, dc, "postgres_solar_solar")
		store, err := sqlsink.NewSolarStore(context.Background(), db, "solar", testLogger())
		if err != nil {
			t.Fatalf("NewSolarStore: %v", err)
		}
		mock.ExpectExec("INSERT INTO solar").WillReturnError(errTest)
		if storeErr := store.StoreEnvoySolarData(context.Background(), domain.EnvoySolarData{}); storeErr == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("duco box", func(t *testing.T) {
		store, mock := newDucoStore(t, dc)
		mock.ExpectExec("INSERT INTO duco_box_general").WillReturnError(errTest)
		if err := store.StoreBoxStatus(context.Background(), domain.DucoBoxStatus{}); err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("duco nodes", func(t *testing.T) {
		store, mock := newDucoStore(t, dc)
		nodes := []domain.DucoNodeStatus{
			domain.DucoRFSensorStatus{},
			domain.DucoNodeBoxStatus{},
			domain.DucoNodeBoxValveStatus{},
		}
		for _, node := range nodes {
			mock.ExpectExec("INSERT INTO duco_").WillReturnError(errTest)
			if err := store.StoreNodeData(context.Background(), node); err == nil {
				t.Errorf("expected error for %T, got nil", node)
			}
		}
	})
}

func TestStores_FlushAndCloseAreNoOps(t *testing.T) {
	dc := dialectCases()[0]
	store, mock := newHeatStore(t, dc)
	if err := store.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}
