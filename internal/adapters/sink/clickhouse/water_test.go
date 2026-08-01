//nolint:dupl // water and gas tests share the same shape but cover distinct domain types
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

func TestWaterStore_StoreWaterReading(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_water_water")

	store, storeErr := clickhouse.NewWaterStore(context.Background(), db, "water", testLogger())
	if storeErr != nil {
		t.Fatalf("NewWaterStore: %v", storeErr)
	}

	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reading := domain.WaterReading{
		CapturedAt: capturedAt,
		ReceivedAt: capturedAt.Add(time.Minute),
		Channel:    2,
		DeviceType: domain.DeviceTypeWater,
		SerialNo:   "sn-water-1",
		ReadingM3:  872.234,
	}
	if writeErr := store.StoreWaterReading(context.Background(), reading); writeErr != nil {
		t.Errorf("StoreWaterReading: %v", writeErr)
	}

	// It should only be buffered, so no Exec yet.
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	// Now expect the batch insert on Flush
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO water")
	mock.ExpectExec("INSERT INTO water").
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

func TestWaterStore_FlushErrorRequeues(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_water_water")

	store, storeErr := clickhouse.NewWaterStore(context.Background(), db, "water", testLogger())
	if storeErr != nil {
		t.Fatalf("NewWaterStore: %v", storeErr)
	}

	_ = store.StoreWaterReading(context.Background(), domain.WaterReading{CapturedAt: time.Now()})

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if flushErr := store.Flush(context.Background()); flushErr == nil {
		t.Error("expected error but got nil")
	}

	// The failed batch is re-queued: the next flush inserts it.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO water")
	mock.ExpectExec("INSERT INTO water").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestWaterStore_CloseFlushesPending(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_water_water")

	store, storeErr := clickhouse.NewWaterStore(context.Background(), db, "water", testLogger())
	if storeErr != nil {
		t.Fatalf("NewWaterStore: %v", storeErr)
	}

	_ = store.StoreWaterReading(context.Background(), domain.WaterReading{CapturedAt: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO water")
	mock.ExpectExec("INSERT INTO water").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestWaterStore_FlushAndCloseEmpty(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_water_water")

	store, storeErr := clickhouse.NewWaterStore(context.Background(), db, "water", testLogger())
	if storeErr != nil {
		t.Fatalf("NewWaterStore: %v", storeErr)
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
