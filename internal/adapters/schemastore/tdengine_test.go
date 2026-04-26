package schemastore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

func TestTDEngineMigrator_NoOutstandingMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT MAX\\(version\\)").
		WithArgs("tdengine_heat").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(1))

	m := schemastore.NewTDEngineMigrator(db, testLogger())

	mg := schemastore.Migration{
		Version:     1,
		Description: "already applied",
		Up: func(_ context.Context) error {
			t.Error("should not be called")
			return nil
		},
	}

	if migrateErr := m.Migrate(context.Background(), "tdengine_heat", []schemastore.Migration{mg}); migrateErr != nil {
		t.Errorf("Migrate: %v", migrateErr)
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

func TestTDEngineMigrator_NullMaxVersionReturnsZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Return NULL from MAX(version) when table is empty.
	mock.ExpectQuery("SELECT MAX\\(version\\)").
		WithArgs("tdengine_heat").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(nil))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WithArgs("tdengine_heat", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	called := false
	mg := schemastore.Migration{
		Version:     1,
		Description: "create table",
		Up: func(_ context.Context) error {
			called = true
			return nil
		},
	}

	m := schemastore.NewTDEngineMigrator(db, testLogger())
	if migrateErr := m.Migrate(context.Background(), "tdengine_heat", []schemastore.Migration{mg}); migrateErr != nil {
		t.Errorf("Migrate: %v", migrateErr)
	}
	if !called {
		t.Error("migration Up was not called")
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

func TestTDEngineMigrator_AppliesNewMigration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT MAX\\(version\\)").
		WithArgs("tdengine_heat").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))
	mock.ExpectExec("CREATE TABLE").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WithArgs("tdengine_heat", 1).
		WillReturnResult(sqlmock.NewResult(1, 1))

	called := false
	capturedDB := db
	mg := schemastore.Migration{
		Version:     1,
		Description: "create table",
		Up: func(ctx context.Context) error {
			called = true
			_, execErr := capturedDB.ExecContext(ctx, "CREATE TABLE foo(id INT)")
			return execErr
		},
	}

	m := schemastore.NewTDEngineMigrator(db, testLogger())
	if migrateErr := m.Migrate(context.Background(), "tdengine_heat", []schemastore.Migration{mg}); migrateErr != nil {
		t.Errorf("Migrate: %v", migrateErr)
	}
	if !called {
		t.Error("migration Up was not called")
	}
	if expectErr := mock.ExpectationsWereMet(); expectErr != nil {
		t.Errorf("unfulfilled expectations: %v", expectErr)
	}
}

//nolint:dupl // mirrors ClickHouse error test; different migrator type under test
func TestTDEngineMigrator_UpErrorStopsFurtherMigrations(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT MAX\\(version\\)").
		WithArgs("tdengine_heat").
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(0))

	upErr := errors.New("migration failed")
	secondCalled := false
	migrations := []schemastore.Migration{
		{
			Version:     1,
			Description: "failing migration",
			Up:          func(_ context.Context) error { return upErr },
		},
		{
			Version:     2,
			Description: "should not run",
			Up: func(_ context.Context) error {
				secondCalled = true
				return nil
			},
		},
	}

	m := schemastore.NewTDEngineMigrator(db, testLogger())
	migrateErr := m.Migrate(context.Background(), "tdengine_heat", migrations)
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
