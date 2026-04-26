package tdengine_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testDB(t *testing.T) (*tdengine.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return tdengine.NewDBFromSQL(db, testLogger()), mock
}

func expectMigrationAlreadyApplied(mock sqlmock.Sqlmock, component string) {
	// TDEngine migrator creates table and uses MAX(version) with NullInt32
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT MAX").
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
}

func TestHeatStore_StoreHeatTelegram(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "tdengine_heat_heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnResult(sqlmock.NewResult(1, 1))

	store, storeErr := tdengine.NewHeatStore(context.Background(), db, "heat", testLogger())
	if storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}

	writeErr := store.StoreHeatTelegram(
		context.Background(), domain.HeatTelegram{
			Timestamp:   time.Now(),
			MeterID:     "m1",
			SerialNo:    "s1",
			ActualPower: 100,
			Joules:      1_000_000_000,
		},
	)
	if writeErr != nil {
		t.Errorf("StoreHeatTelegram: %v", writeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestHeatStore_StoreError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "tdengine_heat_heat")
	mock.ExpectExec("INSERT INTO heat").WillReturnError(errTest)

	store, storeErr := tdengine.NewHeatStore(context.Background(), db, "heat", testLogger())
	if storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}

	writeErr := store.StoreHeatTelegram(context.Background(), domain.HeatTelegram{Timestamp: time.Now()})
	if writeErr == nil {
		t.Error("expected error, got nil")
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestHeatStore_FlushAndClose(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "tdengine_heat_heat")

	store, storeErr := tdengine.NewHeatStore(context.Background(), db, "heat", testLogger())
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
