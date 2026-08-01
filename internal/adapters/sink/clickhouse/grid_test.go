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

func TestGridStore_StoreGridTelegram(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAppliedAt(mock, "clickhouse_grid_grid", 2)

	store, storeErr := clickhouse.NewGridStore(context.Background(), db, "grid", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}

	tel := domain.GridTelegram{
		Time:             time.Now(),
		AvgDemand:        2351,
		MaxDemandMonth:   2589,
		MaxDemandMonthAt: time.Now().Add(-time.Hour),
		MeterMerkType:    "ISK",
		Serienummer:      "123",
		UsageCounter1:    1.1,
		UsageCounter2:    2.2,
		OutputCounter1:   3.3,
		OutputCounter2:   4.4,
		TotalPowerUsage:  500,
		TotalPowerOutput: 600,
		VoltageP1:        230.1,
		VoltageP2:        231.2,
		VoltageP3:        229.9,
		CurrentP1:        1,
		CurrentP2:        2,
		CurrentP3:        3,
		PowerUsageP1:     100,
		PowerUsageP2:     200,
		PowerUsageP3:     300,
	}
	if writeErr := store.StoreGridTelegram(context.Background(), tel); writeErr != nil {
		t.Errorf("StoreGridTelegram: %v", writeErr)
	}

	// It should only be buffered, so no Exec yet.
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	// Now expect the batch insert on Flush
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO grid")
	mock.ExpectExec("INSERT INTO grid").
		WithArgs(
			tel.Time, tel.MeterMerkType, tel.Serienummer,
			tel.UsageCounter1, tel.UsageCounter2, tel.OutputCounter1, tel.OutputCounter2,
			tel.TotalPowerUsage, tel.TotalPowerOutput,
			tel.BrownoutsP1, tel.BrownoutsP2, tel.BrownoutsP3,
			tel.SpikesP1, tel.SpikesP2, tel.SpikesP3,
			tel.VoltageP1, tel.VoltageP2, tel.VoltageP3,
			tel.CurrentP1, tel.CurrentP2, tel.CurrentP3,
			tel.PowerUsageP1, tel.PowerUsageP2, tel.PowerUsageP3,
			tel.PowerOutputP1, tel.PowerOutputP2, tel.PowerOutputP3,
			tel.AvgDemand, tel.MaxDemandMonth, tel.MaxDemandMonthAt,
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

func TestGridStore_FlushErrorRequeues(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAppliedAt(mock, "clickhouse_grid_grid", 2)

	store, storeErr := clickhouse.NewGridStore(context.Background(), db, "grid", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}

	_ = store.StoreGridTelegram(context.Background(), domain.GridTelegram{Time: time.Now()})

	mock.ExpectBegin().WillReturnError(errors.New("connection refused"))

	if flushErr := store.Flush(context.Background()); flushErr == nil {
		t.Error("expected error but got nil")
	}

	// The failed batch is re-queued: the next flush inserts it.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO grid")
	mock.ExpectExec("INSERT INTO grid").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestGridStore_CloseFlushesPending(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAppliedAt(mock, "clickhouse_grid_grid", 2)

	store, storeErr := clickhouse.NewGridStore(context.Background(), db, "grid", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}

	_ = store.StoreGridTelegram(context.Background(), domain.GridTelegram{Time: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO grid")
	mock.ExpectExec("INSERT INTO grid").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestGridStore_FlushAndClose(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAppliedAt(mock, "clickhouse_grid_grid", 2)

	store, storeErr := clickhouse.NewGridStore(context.Background(), db, "grid", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
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
