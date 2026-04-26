package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testPingDB(t *testing.T) (*postgres.DB, sqlmock.Sqlmock) {
	t.Helper()
	rawDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	return postgres.NewDBFromSQL(rawDB, panicLogger()), mock
}

func TestDB_Name(t *testing.T) {
	db, _ := testDB(t)
	if name := db.Name(); name != "postgres" {
		t.Errorf("Name() = %q, want postgres", name)
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
	mock.ExpectPing().WillReturnError(errors.New("ping failed"))
	if err := db.Check(context.Background()); err == nil {
		t.Error("Check() expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func expectHeatMigrationFull(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("postgres_heat_" + table).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectGridMigrationFull(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("postgres_grid_" + table).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectSolarMigrationFull(mock sqlmock.Sqlmock, table string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("postgres_solar_" + table).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	// solar table + inverters table (2 CREATE TABLE stmts)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func expectDucoMigrationFull(mock sqlmock.Sqlmock, base string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("postgres_duco_" + base).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	// 4 tables
	for range 4 {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestNewHeatStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectHeatMigrationFull(mock, "heat")
	_, err := postgres.NewHeatStore(context.Background(), db, "heat", panicLogger())
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
	_, err := postgres.NewGridStore(context.Background(), db, "grid", panicLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewSolarStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectSolarMigrationFull(mock, "solar")
	_, err := postgres.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewDucoStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectDucoMigrationFull(mock, "duco")
	_, err := postgres.NewDucoStore(context.Background(), db, "duco", panicLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreHeatTelegram_Error(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "postgres_heat_heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnError(errors.New("insert failed"))

	store, err := postgres.NewHeatStore(context.Background(), db, "heat", panicLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	if writeErr := store.StoreHeatTelegram(
		context.Background(),
		domain.HeatTelegram{Timestamp: time.Now()},
	); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreGridTelegram_Error(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "postgres_grid_grid")
	mock.ExpectExec("INSERT INTO grid").WillReturnError(errors.New("insert failed"))

	store, err := postgres.NewGridStore(context.Background(), db, "grid", panicLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if writeErr := store.StoreGridTelegram(
		context.Background(),
		domain.GridTelegram{Time: time.Now()},
	); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreSolar_MainInsertError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "postgres_solar_solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnError(errors.New("insert failed"))

	store, err := postgres.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	data := domain.EnvoySolarData{ReadingTime: time.Now()}
	if writeErr := store.StoreEnvoySolarData(context.Background(), data); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreSolar_InverterInsertError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "postgres_solar_solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnError(errors.New("inverter insert failed"))

	store, err := postgres.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	data := domain.EnvoySolarData{
		ReadingTime: time.Now(),
		EnvoySerial: "e1",
		Inverters:   []domain.InverterDetails{{SerialNumber: "inv1", ReportTime: time.Now()}},
	}
	if writeErr := store.StoreEnvoySolarData(context.Background(), data); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_BoxStatusError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_box_general").WillReturnError(errors.New("insert failed"))
	if writeErr := store.StoreBoxStatus(context.Background(), domain.DucoBoxStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_RFSensorError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_node").WillReturnError(errors.New("insert failed"))
	if writeErr := store.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_BoxNodeError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_box_node").WillReturnError(errors.New("insert failed"))
	if writeErr := store.StoreNodeData(context.Background(), domain.DucoNodeBoxStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_ValveNodeError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_valve").WillReturnError(errors.New("insert failed"))
	if writeErr := store.StoreNodeData(context.Background(), domain.DucoNodeBoxValveStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}
