package mysql_test

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func TestSolarStore_StoreEnvoySolarData(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "mysql_solar_solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO solar_inverters").WillReturnResult(sqlmock.NewResult(1, 1))

	store, storeErr := mysql.NewSolarStore(context.Background(), db, "solar", panicLogger())
	if storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}

	writeErr := store.StoreEnvoySolarData(
		context.Background(), domain.EnvoySolarData{
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
		},
	)
	if writeErr != nil {
		t.Errorf("StoreEnvoySolarData: %v", writeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestSolarStore_FlushAndClose(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "mysql_solar_solar")

	store, storeErr := mysql.NewSolarStore(context.Background(), db, "solar", panicLogger())
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
