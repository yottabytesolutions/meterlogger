//nolint:dupl // thermal and gas tests share the same shape but cover distinct domain types
package clickhouse_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func TestThermalStore_StoreThermalReading(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_thermal_thermal")

	store, storeErr := clickhouse.NewThermalStore(context.Background(), db, "thermal", testLogger())
	if storeErr != nil {
		t.Fatalf("NewThermalStore: %v", storeErr)
	}

	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reading := domain.ThermalReading{
		CapturedAt: capturedAt,
		ReceivedAt: capturedAt.Add(time.Minute),
		Channel:    3,
		DeviceType: domain.DeviceTypeHeat,
		SerialNo:   "sn-thermal-1",
		ReadingGJ:  12.345,
	}
	if writeErr := store.StoreThermalReading(context.Background(), reading); writeErr != nil {
		t.Errorf("StoreThermalReading: %v", writeErr)
	}

	// It should only be buffered, so no Exec yet.
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	// Now expect the batch insert on Flush
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO thermal")
	mock.ExpectExec("INSERT INTO thermal").
		WithArgs(
			reading.CapturedAt, reading.ReceivedAt,
			reading.Channel, reading.DeviceType,
			reading.SerialNo, reading.ReadingGJ,
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

func TestThermalStore_FlushErrorRequeues(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_thermal_thermal")

	store, storeErr := clickhouse.NewThermalStore(context.Background(), db, "thermal", testLogger())
	if storeErr != nil {
		t.Fatalf("NewThermalStore: %v", storeErr)
	}

	_ = store.StoreThermalReading(context.Background(), domain.ThermalReading{CapturedAt: time.Now()})

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if flushErr := store.Flush(context.Background()); flushErr == nil {
		t.Error("expected error but got nil")
	}

	// The failed batch is re-queued: the next flush inserts it.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO thermal")
	mock.ExpectExec("INSERT INTO thermal").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestThermalStore_CloseFlushesPending(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_thermal_thermal")

	store, storeErr := clickhouse.NewThermalStore(context.Background(), db, "thermal", testLogger())
	if storeErr != nil {
		t.Fatalf("NewThermalStore: %v", storeErr)
	}

	_ = store.StoreThermalReading(context.Background(), domain.ThermalReading{CapturedAt: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO thermal")
	mock.ExpectExec("INSERT INTO thermal").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestThermalStore_FlushAndCloseEmpty(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_thermal_thermal")

	store, storeErr := clickhouse.NewThermalStore(context.Background(), db, "thermal", testLogger())
	if storeErr != nil {
		t.Fatalf("NewThermalStore: %v", storeErr)
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
