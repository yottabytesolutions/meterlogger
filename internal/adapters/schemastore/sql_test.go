package schemastore_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// Shared test fixtures for migrator tests across clickhouse_test.go, sql_test.go, and tdengine_test.go.
const (
	versionColumn               = "version"
	descriptionAlreadyApplied   = "already applied"
	descriptionCreateTable      = "create table"
	descriptionFailingMigration = "failing migration"
	descriptionShouldNotRun     = "should not run"
)

func TestSQLMigrator_NoOutstandingMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_heat").
		WillReturnRows(sqlmock.NewRows([]string{versionColumn}).AddRow(1))

	m := schemastore.NewSQLMigrator(db, schemastore.DollarPlaceholder, testLogger())

	mg := schemastore.Migration{
		Version:     1,
		Description: descriptionAlreadyApplied,
		Up: func(_ context.Context) error {
			t.Error("should not be called")
			return nil
		},
	}

	migrateErr := m.Migrate(context.Background(), "timescaledb_heat", []schemastore.Migration{mg})
	if migrateErr != nil {
		t.Errorf("Migrate: %v", migrateErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

func TestSQLMigrator_AppliesNewMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_heat").
		WillReturnRows(sqlmock.NewRows([]string{versionColumn}).AddRow(0))
	mock.ExpectExec("CREATE TABLE").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WithArgs("timescaledb_heat", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	called := false
	capturedDB := db
	mg := schemastore.Migration{
		Version:     1,
		Description: descriptionCreateTable,
		Up: func(ctx context.Context) error {
			called = true
			_, execErr := capturedDB.ExecContext(ctx, "CREATE TABLE foo(id INT)")
			return execErr
		},
	}

	m := schemastore.NewSQLMigrator(db, schemastore.DollarPlaceholder, testLogger())
	migrateErr2 := m.Migrate(context.Background(), "timescaledb_heat", []schemastore.Migration{mg})
	if migrateErr2 != nil {
		t.Errorf("Migrate: %v", migrateErr2)
	}
	if !called {
		t.Error("migration Up was not called")
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

func TestSQLMigrator_UpErrorStopsFurtherMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_heat").
		WillReturnRows(sqlmock.NewRows([]string{versionColumn}).AddRow(0))

	upErr := errors.New("migration failed")
	secondCalled := false
	migrations := []schemastore.Migration{
		{
			Version:     1,
			Description: descriptionFailingMigration,
			Up:          func(_ context.Context) error { return upErr },
		},
		{
			Version:     2,
			Description: descriptionShouldNotRun,
			Up: func(_ context.Context) error {
				secondCalled = true
				return nil
			},
		},
	}

	m := schemastore.NewSQLMigrator(db, schemastore.DollarPlaceholder, testLogger())
	migrateErr := m.Migrate(context.Background(), "timescaledb_heat", migrations)
	if migrateErr == nil {
		t.Error("expected error but got nil")
	}
	if secondCalled {
		t.Error("second migration should not have been called")
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

func TestSQLMigrator_QuestionPlaceholder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("mysql_heat").
		WillReturnRows(sqlmock.NewRows([]string{versionColumn}).AddRow(0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WithArgs("mysql_heat", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mg := schemastore.Migration{
		Version:     1,
		Description: descriptionCreateTable,
		Up:          func(_ context.Context) error { return nil },
	}

	m := schemastore.NewSQLMigrator(db, schemastore.QuestionPlaceholder, testLogger())
	if migrateErr := m.Migrate(context.Background(), "mysql_heat", []schemastore.Migration{mg}); migrateErr != nil {
		t.Errorf("Migrate: %v", migrateErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}
