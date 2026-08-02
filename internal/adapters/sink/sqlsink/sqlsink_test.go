package sqlsink_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
)

var errTest = errors.New("test error")

const coalesceQuery = "SELECT COALESCE"

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// dialectCase holds hardcoded per-dialect expectations. The prefix strings are
// the migration ledger component key prefixes and must never change.
type dialectCase struct {
	prefix       string
	dialect      sqlsink.Dialect
	versionQuery string
	hypertable   bool
}

func dialectCases() []dialectCase {
	return []dialectCase{
		{prefix: "postgres", dialect: sqlsink.PostgresDialect(), versionQuery: coalesceQuery},
		{prefix: "mysql", dialect: sqlsink.MySQLDialect(), versionQuery: coalesceQuery},
		{
			prefix: "timescaledb", dialect: sqlsink.TimescaleDBDialect(),
			versionQuery: coalesceQuery, hypertable: true,
		},
		{prefix: "tdengine", dialect: sqlsink.TDEngineDialect(), versionQuery: "SELECT MAX"},
	}
}

func testDB(t *testing.T, d sqlsink.Dialect) (*sqlsink.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	return sqlsink.NewDBFromSQL(d, db, testLogger()), mock
}

func testPingDB(t *testing.T, d sqlsink.Dialect) (*sqlsink.DB, sqlmock.Sqlmock) {
	t.Helper()
	rawDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })
	return sqlsink.NewDBFromSQL(d, rawDB, testLogger()), mock
}

// expectMigrationFull expects a full migration run: ledger table create,
// version query for the exact component key, table creates, version record.
// alterCount is the number of ALTER TABLE statements run by later migration
// versions (grid version 2); each extra version records once more.
func expectMigrationFull(mock sqlmock.Sqlmock, dc dialectCase, component string, tableCount, alterCount int) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(dc.versionQuery).
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	for range tableCount {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
		if dc.hypertable {
			mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
	if alterCount == 0 {
		return
	}
	for range alterCount {
		mock.ExpectExec("ALTER TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))
}

// expectMigrationAppliedAt expects the ledger to report version as already
// applied, so no DDL runs.
func expectMigrationAppliedAt(mock sqlmock.Sqlmock, dc dialectCase, component string, version int) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(dc.versionQuery).
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(version))
}

func expectMigrationAlreadyApplied(mock sqlmock.Sqlmock, dc dialectCase, component string) {
	expectMigrationAppliedAt(mock, dc, component, 1)
}

// latestGridVersion is the current grid schema version (peak demand columns).
const latestGridVersion = 2

func TestDB_Name(t *testing.T) {
	for _, dc := range dialectCases() {
		t.Run(dc.prefix, func(t *testing.T) {
			db, _ := testDB(t, dc.dialect)
			if name := db.Name(); name != dc.prefix {
				t.Errorf("Name() = %q, want %q", name, dc.prefix)
			}
		})
	}
}

func TestDB_Close(t *testing.T) {
	db, mock := testDB(t, sqlsink.PostgresDialect())
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

func TestDB_Check(t *testing.T) {
	db, mock := testPingDB(t, sqlsink.PostgresDialect())
	mock.ExpectPing()
	if err := db.Check(context.Background()); err != nil {
		t.Errorf("Check() error: %v", err)
	}
	mock.ExpectPing().WillReturnError(errTest)
	if err := db.Check(context.Background()); err == nil {
		t.Error("Check() expected error, got nil")
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

// TestMigrationComponentKeys locks down the migration ledger component keys.
// Existing deployments track applied migrations under these exact strings.
func TestMigrationComponentKeys(t *testing.T) {
	type storeCase struct {
		kind       string
		tableCount int
		alterCount int
		construct  func(ctx context.Context, db *sqlsink.DB) error
	}
	stores := []storeCase{
		{kind: "heat", tableCount: 1, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewHeatStore(ctx, db, "m", testLogger())
			return err
		}},
		{kind: "grid", tableCount: 1, alterCount: 3, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewGridStore(ctx, db, "m", testLogger())
			return err
		}},
		{kind: "gas", tableCount: 1, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewGasStore(ctx, db, "m", testLogger())
			return err
		}},
		{kind: "water", tableCount: 1, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewWaterStore(ctx, db, "m", testLogger())
			return err
		}},
		{kind: "thermal", tableCount: 1, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewThermalStore(ctx, db, "m", testLogger())
			return err
		}},
		{kind: "solar", tableCount: 2, alterCount: 15, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewSolarStore(ctx, db, "m", testLogger())
			return err
		}},
		{kind: "duco", tableCount: 4, construct: func(ctx context.Context, db *sqlsink.DB) error {
			_, err := sqlsink.NewDucoStore(ctx, db, "m", testLogger())
			return err
		}},
	}
	for _, dc := range dialectCases() {
		for _, sc := range stores {
			t.Run(dc.prefix+"_"+sc.kind, func(t *testing.T) {
				db, mock := testDB(t, dc.dialect)
				component := dc.prefix + "_" + sc.kind + "_m"
				expectMigrationFull(mock, dc, component, sc.tableCount, sc.alterCount)
				if err := sc.construct(context.Background(), db); err != nil {
					t.Fatalf("construct: %v", err)
				}
				if metErr := mock.ExpectationsWereMet(); metErr != nil {
					t.Error(metErr)
				}
			})
		}
	}
}

func TestNewStore_MigrationError(t *testing.T) {
	db, mock := testDB(t, sqlsink.PostgresDialect())
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnError(errTest)
	if _, err := sqlsink.NewHeatStore(context.Background(), db, "m", testLogger()); err == nil {
		t.Error("NewHeatStore expected error, got nil")
	}
}

func TestNewStore_HypertableError(t *testing.T) {
	dc := dialectCases()[2]
	db, mock := testDB(t, dc.dialect)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(dc.versionQuery).
		WithArgs("timescaledb_heat_m").
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SELECT create_hypertable").WillReturnError(errTest)
	if _, err := sqlsink.NewHeatStore(context.Background(), db, "m", testLogger()); err == nil {
		t.Error("NewHeatStore expected hypertable error, got nil")
	}
}
