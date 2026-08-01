package clickhouse_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func testDB(t *testing.T) (*clickhouse.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	return clickhouse.NewDBFromSQL(db, testLogger()), mock
}

func expectMigrationAlreadyApplied(mock sqlmock.Sqlmock, component string) {
	expectMigrationAppliedAt(mock, component, 1)
}

// expectMigrationAppliedAt reports version as already applied so no DDL runs.
// The grid store is at version 2 (peak demand columns).
func expectMigrationAppliedAt(mock sqlmock.Sqlmock, component string, version int) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(version))
}

func TestHeatStore_StoreHeatTelegram(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_heat_heat")

	store, storeErr := clickhouse.NewHeatStore(context.Background(), db, "heat", testLogger())
	if storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}

	tel := domain.HeatTelegram{
		Timestamp:      time.Now(),
		MeterID:        "m1",
		SerialNo:       "s1",
		ActualPower:    100,
		Joules:         1_000_000_000,
		Tforward:       80.5,
		Treturn:        60.25,
		Tdiff:          20.25,
		VolumeCm3:      1234,
		SecondsCounter: 99,
		MaxFlow:        2.5,
		MaxPower:       150,
	}
	if writeErr := store.StoreHeatTelegram(context.Background(), tel); writeErr != nil {
		t.Errorf("StoreHeatTelegram: %v", writeErr)
	}

	// Buffered, no exec yet
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO heat")
	mock.ExpectExec("INSERT INTO heat").
		WithArgs(
			tel.Timestamp, tel.MeterID, tel.SerialNo, tel.ActualPower, 1.0,
			tel.Tforward, tel.Treturn, tel.Tdiff, tel.VolumeCm3, tel.SecondsCounter,
			tel.MaxFlow, tel.MaxPower,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if flushErr := store.Flush(context.Background()); flushErr != nil {
		t.Errorf("Flush: %v", flushErr)
	}

	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestHeatStore_FlushAndClose(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_heat_heat")

	store, storeErr := clickhouse.NewHeatStore(context.Background(), db, "heat", testLogger())
	if storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}
	if flushErr := store.Flush(context.Background()); flushErr != nil {
		t.Errorf("Flush: %v", flushErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestHeatStore_StoreError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_heat_heat")

	store, storeErr := clickhouse.NewHeatStore(context.Background(), db, "heat", testLogger())
	if storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}

	_ = store.StoreHeatTelegram(context.Background(), domain.HeatTelegram{Timestamp: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	flushErr := store.Flush(context.Background())
	if flushErr == nil {
		t.Error("expected error but got nil")
	}

	// The failed batch is re-queued: the next flush inserts it.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestHeatStore_CloseFlushesPending(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_heat_heat")

	store, storeErr := clickhouse.NewHeatStore(context.Background(), db, "heat", testLogger())
	if storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}

	_ = store.StoreHeatTelegram(context.Background(), domain.HeatTelegram{Timestamp: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}
