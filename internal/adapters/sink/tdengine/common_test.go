package tdengine_test

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testPingDB(t *testing.T) (*tdengine.DB, sqlmock.Sqlmock) {
	t.Helper()
	rawDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	return tdengine.NewDBFromSQL(rawDB, testLogger()), mock
}

func TestDB_Name(t *testing.T) {
	db, _ := testDB(t)
	if name := db.Name(); name != "tdengine" {
		t.Errorf("Name() = %q, want tdengine", name)
	}
}

func TestDB_Close(t *testing.T) {
	db, mock := testDB(t)
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDB_Check_Success(t *testing.T) {
	db, mock := testPingDB(t)
	mock.ExpectPing()
	if err := db.Check(context.Background()); err != nil {
		t.Errorf("Check() error: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDB_Check_Error(t *testing.T) {
	db, mock := testPingDB(t)
	mock.ExpectPing().WillReturnError(errTest)
	if err := db.Check(context.Background()); err == nil {
		t.Error("Check() expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

// TDEngine migrator uses SELECT MAX(version) (NullInt32 scan).
func expectTDEMigrationFull(mock sqlmock.Sqlmock, component string, createCount int) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT MAX").
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
	expectTDEMigrationFull(mock, "tdengine_heat_heat", 1)
	_, err := tdengine.NewHeatStore(context.Background(), db, "heat", testLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewGridStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectTDEMigrationFull(mock, "tdengine_grid_grid", 1)
	_, err := tdengine.NewGridStore(context.Background(), db, "grid", testLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewSolarStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectTDEMigrationFull(mock, "tdengine_solar_solar", 2)
	_, err := tdengine.NewSolarStore(context.Background(), db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestNewDucoStore_MigrationRuns(t *testing.T) {
	db, mock := testDB(t)
	expectTDEMigrationFull(mock, "tdengine_duco_duco", 4)
	_, err := tdengine.NewDucoStore(context.Background(), db, "duco", testLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestStoreGridTelegram_Error(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "tdengine_grid_grid")
	mock.ExpectExec("INSERT INTO grid").WillReturnError(errTest)

	store, err := tdengine.NewGridStore(context.Background(), db, "grid", testLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	if writeErr := store.StoreGridTelegram(
		context.Background(),
		domain.GridTelegram{Time: time.Now()},
	); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreSolar_MainInsertError(t *testing.T) {
	db, mock := testDB(t)
	expectMigrationAlreadyApplied(mock, "tdengine_solar_solar")
	mock.ExpectExec("INSERT INTO solar").WillReturnError(errTest)

	store, err := tdengine.NewSolarStore(context.Background(), db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	data := domain.EnvoySolarData{ReadingTime: time.Now()}
	if writeErr := store.StoreEnvoySolarData(context.Background(), data); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_BoxStatusError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_box_general").WillReturnError(errTest)
	if writeErr := store.StoreBoxStatus(context.Background(), domain.DucoBoxStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_RFSensorError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_node").WillReturnError(errTest)
	if writeErr := store.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_BoxNodeError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_box_node").WillReturnError(errTest)
	if writeErr := store.StoreNodeData(context.Background(), domain.DucoNodeBoxStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}

func TestStoreDuco_ValveNodeError(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_valve").WillReturnError(errTest)
	if writeErr := store.StoreNodeData(context.Background(), domain.DucoNodeBoxValveStatus{}); writeErr == nil {
		t.Error("expected error, got nil")
	}
}
