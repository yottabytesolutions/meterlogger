package clickhouse_test

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func TestGridStore_StoreGridTelegram(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_grid_grid")

	store, storeErr := clickhouse.NewGridStore(context.Background(), db, "grid", testLogger())
	if storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}

	writeErr := store.StoreGridTelegram(
		context.Background(), domain.GridTelegram{
			Time:          time.Now(),
			MeterMerkType: "ISK",
			Serienummer:   "123",
		},
	)
	if writeErr != nil {
		t.Errorf("StoreGridTelegram: %v", writeErr)
	}

	// It should only be buffered, so no Exec yet.
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	// Now expect the batch insert on Flush
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO grid")
	mock.ExpectExec("INSERT INTO grid").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if flushErr := store.Flush(context.Background()); flushErr != nil {
		t.Errorf("Flush: %v", flushErr)
	}

	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestGridStore_FlushAndClose(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_grid_grid")

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
