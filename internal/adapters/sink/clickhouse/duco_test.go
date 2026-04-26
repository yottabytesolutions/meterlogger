package clickhouse_test

import (
	"context"
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

func TestDucoStore_StoreBoxStatus(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_box_general").WillReturnResult(sqlmock.NewResult(1, 1))

	if writeErr := store.StoreBoxStatus(context.Background(), domain.DucoBoxStatus{}); writeErr != nil {
		t.Errorf("StoreBoxStatus: %v", writeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_StoreNodeData_RFSensor(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_node").WillReturnResult(sqlmock.NewResult(1, 1))

	if writeErr := store.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{}); writeErr != nil {
		t.Errorf("StoreNodeData RFSensor: %v", writeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_StoreNodeData_BoxNode(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_box_node").WillReturnResult(sqlmock.NewResult(1, 1))

	if writeErr := store.StoreNodeData(context.Background(), domain.DucoNodeBoxStatus{}); writeErr != nil {
		t.Errorf("StoreNodeData BoxNode: %v", writeErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_StoreNodeData_Valve(t *testing.T) {
	store, mock := newDucoStore(t)
	mock.ExpectExec("INSERT INTO duco_valve").WillReturnResult(sqlmock.NewResult(1, 1))

	if writeErr := store.StoreNodeData(context.Background(), domain.DucoNodeBoxValveStatus{}); writeErr != nil {
		t.Errorf("StoreNodeData Valve: %v", writeErr)
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
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Error(expectErr)
	}
}

func TestDucoStore_FlushAndClose(t *testing.T) {
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
