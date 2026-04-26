package timescaledb_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
)

func TestDB_Name(t *testing.T) {
	db, _ := testDB(t)
	if name := db.Name(); name != "timescaledb" {
		t.Errorf("Name() = %q, want timescaledb", name)
	}
}

func TestDB_Close(t *testing.T) {
	db, mock := testDB(t)
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func testPingDB(t *testing.T) (*timescaledb.DB, sqlmock.Sqlmock) {
	t.Helper()
	// MonitorPingsOption required for ExpectPing to be registered.
	rawDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	return timescaledb.NewDBFromSQL(rawDB, panicLogger()), mock
}

func TestDB_Check_Success(t *testing.T) {
	db, mock := testPingDB(t)
	mock.ExpectPing()
	if err := db.Check(context.Background()); err != nil {
		t.Errorf("Check() error: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDB_Check_Error(t *testing.T) {
	db, mock := testPingDB(t)
	pingErr := errors.New("ping failed")
	mock.ExpectPing().WillReturnError(pingErr)
	if err := db.Check(context.Background()); err == nil {
		t.Error("Check() expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

// expectDucoMigrationFull sets up mock expectations for a full duco migration
// (version 0 -> 1): creates schema table, queries version (returns 0),
// executes 4 DDLs + 4 hypertable calls, then inserts version record.
func expectDucoMigrationFull(mock sqlmock.Sqlmock, base string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_duco_" + base).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	// 4 tables × (CREATE + hypertable)
	for range 4 {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectHeatMigrationFull(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_heat_" + table).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectGridMigrationFull(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_grid_" + table).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestNewHeatStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectHeatMigrationFull(mock, "heat")

	_, err := timescaledb.NewHeatStore(context.Background(), db, "heat", panicLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewGridStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectGridMigrationFull(mock, "grid")

	_, err := timescaledb.NewGridStore(context.Background(), db, "grid", panicLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewDucoStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectDucoMigrationFull(mock, "duco")

	_, err := timescaledb.NewDucoStore(context.Background(), db, "duco", panicLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func expectSolarMigrationFull(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_solar_" + table).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	// solar table + hypertable + inverters table + hypertable
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestNewSolarStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectSolarMigrationFull(mock, "solar")

	_, err := timescaledb.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreHeatTelegram_Error(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_heat_heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewHeatStore(context.Background(), db, "heat", panicLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	if writeErr := store.StoreHeatTelegram(context.Background(), timescaleDummyHeat()); writeErr == nil {
		t.Error("expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreGridTelegram_Error(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_grid_grid")
	mock.ExpectExec("INSERT INTO grid").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewGridStore(context.Background(), db, "grid", panicLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if writeErr := store.StoreGridTelegram(context.Background(), timescaleDummyGrid()); writeErr == nil {
		t.Error("expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreSolar_MainInsertError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_solar_solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	if writeErr := store.StoreEnvoySolarData(context.Background(), timescaleDummySolar()); writeErr == nil {
		t.Error("expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreSolar_InverterInsertError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_solar_solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnError(errors.New("inverter insert failed"))

	store, err := timescaledb.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	if writeErr := store.StoreEnvoySolarData(context.Background(), timescaleDummySolarWithInverter()); writeErr == nil {
		t.Error("expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreDuco_StoreBoxStatusError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_duco_duco")
	mock.ExpectExec("INSERT INTO duco_box_general").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewDucoStore(context.Background(), db, "duco", panicLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if writeErr := store.StoreBoxStatus(context.Background(), timescaleDummyBoxStatus()); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_StoreRFSensorError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_duco_duco")
	mock.ExpectExec("INSERT INTO duco_node").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewDucoStore(context.Background(), db, "duco", panicLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if writeErr := store.StoreNodeData(context.Background(), timescaleDummyRFSensor()); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_StoreBoxNodeError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_duco_duco")
	mock.ExpectExec("INSERT INTO duco_box_node").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewDucoStore(context.Background(), db, "duco", panicLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if writeErr := store.StoreNodeData(context.Background(), timescaleDummyBoxNode()); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_StoreValveNodeError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "timescaledb_duco_duco")
	mock.ExpectExec("INSERT INTO duco_valve").WillReturnError(errors.New("insert failed"))

	store, err := timescaledb.NewDucoStore(context.Background(), db, "duco", panicLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if writeErr := store.StoreNodeData(context.Background(), timescaleDummyValveNode()); writeErr == nil {
		t.Error("expected error, got nil")
	}
}
