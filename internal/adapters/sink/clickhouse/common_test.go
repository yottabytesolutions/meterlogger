package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
)

func testPingDB(t *testing.T) (*clickhouse.DB, sqlmock.Sqlmock) {
	t.Helper()
	rawDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	return clickhouse.NewDBFromSQL(rawDB, testLogger()), mock
}

func TestDB_Name(t *testing.T) {
	db, _ := testDB(t)
	if name := db.Name(); name != "clickhouse" {
		t.Errorf("Name() = %q, want clickhouse", name)
	}
}

func TestDB_Close(t *testing.T) {
	db, mock := testDB(t)
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDB_Check_Success(t *testing.T) {
	db, mock := testPingDB(t)
	mock.ExpectPing()
	if err := db.Check(context.Background()); err != nil {
		t.Errorf("Check() error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDB_Check_Error(t *testing.T) {
	db, mock := testPingDB(t)
	mock.ExpectPing().WillReturnError(errors.New("ping failed"))
	if err := db.Check(context.Background()); err == nil {
		t.Error("Check() expected error, got nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// ClickHouse migrator uses SELECT COALESCE and CREATE TABLE IF NOT EXISTS (ClickHouse DDL).
func expectCHMigrationFull(mock sqlmock.Sqlmock, component string, createCount int) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	for range createCount {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestNewHeatStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectCHMigrationFull(mock, "clickhouse_heat_heat", 1)
	_, err := clickhouse.NewHeatStore(context.Background(), db, "heat", testLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewGridStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectCHMigrationFull(mock, "clickhouse_grid_grid", 1)
	_, err := clickhouse.NewGridStore(context.Background(), db, "grid", testLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewGasStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectCHMigrationFull(mock, "clickhouse_gas_gas", 1)
	_, err := clickhouse.NewGasStore(context.Background(), db, "gas", testLogger())
	if err != nil {
		t.Fatalf("NewGasStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewSolarStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectCHMigrationFull(mock, "clickhouse_solar_solar", 2)
	_, err := clickhouse.NewSolarStore(context.Background(), db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewDucoStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectCHMigrationFull(mock, "clickhouse_duco_duco", 4)
	_, err := clickhouse.NewDucoStore(context.Background(), db, "duco", testLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}
