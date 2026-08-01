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

func TestSolarStore_StoreEnvoySolarData(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_solar_solar")

	store, storeErr := clickhouse.NewSolarStore(context.Background(), db, "solar", testLogger())
	if storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}

	data := domain.EnvoySolarData{
		ReadingTime:  time.Now(),
		EnvoySerial:  "env1",
		ProductionWh: 5000,
		Watt:         300,
		PanelCount:   10,
		Inverters: []domain.InverterDetails{
			{
				SerialNumber:      "inv1",
				ReportTime:        time.Now(),
				LastReportedWatts: 30,
				MaxReportWatts:    35,
			},
		},
	}
	if writeErr := store.StoreEnvoySolarData(context.Background(), data); writeErr != nil {
		t.Errorf("StoreEnvoySolarData: %v", writeErr)
	}

	// Buffered, no exec yet
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}

	// One transaction per table: the driver allows a single prepared batch
	// per transaction.
	inv := data.Inverters[0]
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO solar")
	mock.ExpectExec("INSERT INTO solar").
		WithArgs(data.ReadingTime, data.EnvoySerial, data.ProductionWh, data.Watt, data.PanelCount).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO solar_inverters")
	mock.ExpectExec("INSERT INTO solar_inverters").
		WithArgs(
			inv.ReportTime, data.EnvoySerial, inv.SerialNumber, inv.Chaneid,
			inv.Operating, inv.Communicating, inv.Producing,
			inv.Phase, inv.LastReportedWatts, inv.MaxReportWatts,
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

func TestSolarStore_FlushAndClose(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_solar_solar")

	store, storeErr := clickhouse.NewSolarStore(context.Background(), db, "solar", testLogger())
	if storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
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

func TestSolarStore_InverterError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_solar_solar")

	store, storeErr := clickhouse.NewSolarStore(context.Background(), db, "solar", testLogger())
	if storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}

	_ = store.StoreEnvoySolarData(
		context.Background(), domain.EnvoySolarData{
			ReadingTime: time.Now(),
			EnvoySerial: "env1",
			Inverters: []domain.InverterDetails{
				{SerialNumber: "inv1", ReportTime: time.Now()},
			},
		},
	)

	// The main batch commits in its own transaction; the inverter batch
	// fails and is re-queued alone.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO solar_inverters")
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnError(errors.New("inverter insert failed"))
	mock.ExpectRollback()

	flushErr := store.Flush(context.Background())
	if flushErr == nil {
		t.Error("expected error but got nil")
	}

	// Only the re-queued inverter batch flushes on retry.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO solar_inverters")
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestSolarStore_CloseFlushesPending(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "clickhouse_solar_solar")

	store, storeErr := clickhouse.NewSolarStore(context.Background(), db, "solar", testLogger())
	if storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}

	_ = store.StoreEnvoySolarData(context.Background(), domain.EnvoySolarData{ReadingTime: time.Now()})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}
