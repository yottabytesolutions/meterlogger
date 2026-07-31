package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func expectDucoMigrationApplied(mock sqlmock.Sqlmock) {
	expectMigrationAlreadyApplied(mock, "clickhouse_duco_duco")
}

func newDucoStore(t *testing.T) (*clickhouse.DucoStore, sqlmock.Sqlmock) {
	t.Helper()
	db, mock := testDB(t)
	expectDucoMigrationApplied(mock)
	store, storeErr := clickhouse.NewDucoStore(context.Background(), db, "duco", testLogger())
	if storeErr != nil {
		t.Fatalf("NewDucoStore: %v", storeErr)
	}
	return store, mock
}

func testBoxStatus() domain.DucoBoxStatus {
	return domain.DucoBoxStatus{
		EnergyFan: domain.EnergyFan{
			ExhaustFanSpeed:         1200,
			SupplyFanSpeed:          1100,
			ExhaustFanPwmPercentage: 55,
			SupplyFanPwmPercentage:  50,
		},
		EnergyInfo: domain.EnergyInfo{
			BypassStatus:        1,
			FilterRemainingTime: 90,
			FrostProtState:      true,
			TempEHA:             10,
			TempETA:             20,
			TempODA:             5,
			TempSUP:             18,
		},
		General:        domain.General{InstallerState: "operational", RFHomeID: "home1"},
		WeatherStation: domain.WeatherStation{Present: true},
	}
}

func TestDucoStore_StoreBuffersUntilFlush(t *testing.T) {
	store, mock := newDucoStore(t)

	if err := store.StoreBoxStatus(context.Background(), testBoxStatus()); err != nil {
		t.Errorf("StoreBoxStatus: %v", err)
	}
	if err := store.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{}); err != nil {
		t.Errorf("StoreNodeData RFSensor: %v", err)
	}
	if err := store.StoreNodeData(context.Background(), domain.DucoNodeBoxStatus{}); err != nil {
		t.Errorf("StoreNodeData BoxNode: %v", err)
	}
	if err := store.StoreNodeData(context.Background(), domain.DucoNodeBoxValveStatus{}); err != nil {
		t.Errorf("StoreNodeData Valve: %v", err)
	}

	// Buffered, no exec yet.
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_FlushBatchesAllTables(t *testing.T) {
	store, mock := newDucoStore(t)
	box := testBoxStatus()

	_ = store.StoreBoxStatus(context.Background(), box)
	_ = store.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{})
	_ = store.StoreNodeData(context.Background(), domain.DucoNodeBoxStatus{})
	_ = store.StoreNodeData(context.Background(), domain.DucoNodeBoxValveStatus{})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO duco_box_general")
	mock.ExpectExec("INSERT INTO duco_box_general").
		WithArgs(
			sqlmock.AnyArg(), // ts is captured at store time
			box.General.RFHomeID,
			box.EnergyFan.ExhaustFanSpeed, box.EnergyFan.SupplyFanSpeed,
			box.EnergyFan.ExhaustFanPwmPercentage, box.EnergyFan.SupplyFanPwmPercentage,
			box.EnergyInfo.BypassStatus, box.EnergyInfo.FilterRemainingTime, box.EnergyInfo.FrostProtState,
			box.EnergyInfo.TempEHA, box.EnergyInfo.TempETA, box.EnergyInfo.TempODA, box.EnergyInfo.TempSUP,
			box.General.InstallerState, box.WeatherStation.Present,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectPrepare("INSERT INTO duco_node")
	mock.ExpectExec("INSERT INTO duco_node").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectPrepare("INSERT INTO duco_box_node")
	mock.ExpectExec("INSERT INTO duco_box_node").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectPrepare("INSERT INTO duco_valve")
	mock.ExpectExec("INSERT INTO duco_valve").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if flushErr := store.Flush(context.Background()); flushErr != nil {
		t.Errorf("Flush: %v", flushErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_FlushErrorRequeues(t *testing.T) {
	store, mock := newDucoStore(t)

	_ = store.StoreBoxStatus(context.Background(), testBoxStatus())
	_ = store.StoreNodeData(context.Background(), domain.DucoNodeBoxValveStatus{})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO duco_box_general")
	mock.ExpectExec("INSERT INTO duco_box_general").WillReturnError(errors.New("insert failed"))
	mock.ExpectRollback()

	if flushErr := store.Flush(context.Background()); flushErr == nil {
		t.Error("expected error but got nil")
	}

	// The failed batches are re-queued: the next flush inserts them all.
	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO duco_box_general")
	mock.ExpectExec("INSERT INTO duco_box_general").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectPrepare("INSERT INTO duco_valve")
	mock.ExpectExec("INSERT INTO duco_valve").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if retryErr := store.Flush(context.Background()); retryErr != nil {
		t.Errorf("second Flush: %v", retryErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

type unknownNodeStatus struct{ domain.BaseDucoNodeStatus }

func TestDucoStore_StoreNodeData_Unknown(t *testing.T) {
	store, mock := newDucoStore(t)

	if writeErr := store.StoreNodeData(context.Background(), unknownNodeStatus{}); writeErr != nil {
		t.Errorf("StoreNodeData Unknown: %v", writeErr)
	}
	// Unknown node types are skipped, so a flush has nothing to insert.
	if flushErr := store.Flush(context.Background()); flushErr != nil {
		t.Errorf("Flush: %v", flushErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_CloseFlushesPending(t *testing.T) {
	store, mock := newDucoStore(t)

	_ = store.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{})

	mock.ExpectBegin()
	mock.ExpectPrepare("INSERT INTO duco_node")
	mock.ExpectExec("INSERT INTO duco_node").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if closeErr := store.Close(); closeErr != nil {
		t.Errorf("Close: %v", closeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_EmptyFlushAndClose(t *testing.T) {
	store, mock := newDucoStore(t)
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
