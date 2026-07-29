package schemastore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

func TestClickHouseMigrator_NoOutstandingMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("clickhouse_heat").
		WillReturnRows(sqlmock.NewRows([]string{versionColumn}).AddRow(1))

	m := schemastore.NewClickHouseMigrator(db, testLogger())

	mg := schemastore.Migration{
		Version:     1,
		Description: descriptionAlreadyApplied,
		Up: func(_ context.Context) error {
			t.Error("should not be called")
			return nil
		},
	}

	migrateErr := m.Migrate(context.Background(), "clickhouse_heat", []schemastore.Migration{mg})
	if migrateErr != nil {
		t.Errorf("Migrate: %v", migrateErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

func TestClickHouseMigrator_AppliesNewMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("clickhouse_heat").
		WillReturnRows(sqlmock.NewRows([]string{versionColumn}).AddRow(0))
	mock.ExpectExec("CREATE TABLE").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WithArgs("clickhouse_heat", 1).
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

	m := schemastore.NewClickHouseMigrator(db, testLogger())
	migrateErr2 := m.Migrate(context.Background(), "clickhouse_heat", []schemastore.Migration{mg})
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

//nolint:dupl // mirrors TDEngine error test; different migrator type under test
func TestClickHouseMigrator_UpErrorStopsFurtherMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("clickhouse_heat").
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

	m := schemastore.NewClickHouseMigrator(db, testLogger())
	migrateErr := m.Migrate(context.Background(), "clickhouse_heat", migrations)
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
