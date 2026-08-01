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

func TestGasStore_StoreGasReading(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_gas_gas")

	store, storeErr := clickhouse.NewGasStore(context.Background(), db, "gas", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGasStore: %v", storeErr)
	}

	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reading := domain.GasReading{
		CapturedAt: capturedAt,
		ReceivedAt: capturedAt.Add(time.Minute),
		Channel:    1,
		DeviceType: domain.DeviceTypeGas,
		SerialNo:   "sn-gas-1",
		ReadingM3:  1234.567,
	}
	if writeErr := store.StoreGasReading(context.Background(), reading); writeErr != nil {
		t.Errorf("StoreGasReading: %v", writeErr)
	}

	// It should only be buffered, so no Exec yet.
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	// Now expect the batch insert on Flush
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO gas")
	mock.ExpectExec("INSERT INTO gas").
		WithArgs(
			reading.CapturedAt, reading.ReceivedAt,
			reading.Channel, reading.DeviceType,
			reading.SerialNo, reading.ReadingM3,
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

func TestGasStore_FlushErrorRequeues(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_gas_gas")

	store, storeErr := clickhouse.NewGasStore(context.Background(), db, "gas", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGasStore: %v", storeErr)
	}

	_ = store.StoreGasReading(context.Background(), domain.GasReading{CapturedAt: time.Now()})

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if flushErr := store.Flush(context.Background()); flushErr == nil {
		t.Error("expected error but got nil")
	}

	// The failed batch is re-queued: the next flush inserts it.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO gas")
	mock.ExpectExec("INSERT INTO gas").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestGasStore_CloseFlushesPending(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_gas_gas")

	store, storeErr := clickhouse.NewGasStore(context.Background(), db, "gas", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGasStore: %v", storeErr)
	}

	_ = store.StoreGasReading(context.Background(), domain.GasReading{CapturedAt: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO gas")
	mock.ExpectExec("INSERT INTO gas").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestGasStore_FlushAndCloseEmpty(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_gas_gas")

	store, storeErr := clickhouse.NewGasStore(context.Background(), db, "gas", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGasStore: %v", storeErr)
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
