package timescaledb

import (
	"context"
	"log/slog"
	"net/url"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

//nolint:gosec // G101: not a credential, exercises DSN escaping of special characters.
const trickyPassword = `p @ss'w"ord/with:chars?&=`

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func checkDSN(t *testing.T, cfg Config, wantPassword, wantSSLMode string) {
	t.Helper()
	u, err := url.Parse(buildDSN(cfg))
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if u.Scheme != "postgres" {
		t.Errorf("scheme = %q, want postgres", u.Scheme)
	}
	if got := u.User.Username(); got != cfg.User {
		t.Errorf("user = %q, want %q", got, cfg.User)
	}
	password, ok := u.User.Password()
	if !ok || password != wantPassword {
		t.Errorf("password = %q (set=%v), want %q", password, ok, wantPassword)
	}
	if u.Host != "ts.local:5432" {
		t.Errorf("host = %q, want ts.local:5432", u.Host)
	}
	if got := u.Query().Get("sslmode"); got != wantSSLMode {
		t.Errorf("sslmode = %q, want %q", got, wantSSLMode)
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name         string
		cfg          Config
		wantPassword string
		wantSSLMode  string
	}{
		{
			name: "defaults sslmode to disable",
			cfg: Config{
				Host: "ts.local", Port: 5432, User: "u", Password: "p", Database: "meters",
			},
			wantPassword: "p",
			wantSSLMode:  "disable",
		},
		{
			name: "password with special characters",
			cfg: Config{
				Host: "ts.local", Port: 5432, User: "user@corp",
				Password: trickyPassword, Database: "meters", SSLMode: "verify-full",
			},
			wantPassword: trickyPassword,
			wantSSLMode:  "verify-full",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkDSN(t, tt.cfg, tt.wantPassword, tt.wantSSLMode)
		})
	}
}

// expectApplied reports the highest schema version of any store (grid v2)
// as already applied so no DDL runs.
func expectApplied(mock sqlmock.Sqlmock, component string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(2))
}

// TestWiring exercises the delegation into sqlsink with the timescaledb
// dialect: health check name and the migration ledger component keys.
func TestWiring(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db := NewDBFromSQL(raw, testLogger())
	if name := db.Name(); name != "timescaledb" {
		t.Errorf("Name() = %q, want timescaledb", name)
	}

	ctx := context.Background()
	expectApplied(mock, "timescaledb_heat_m")
	if _, storeErr := NewHeatStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}
	expectApplied(mock, "timescaledb_grid_m")
	if _, storeErr := NewGridStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}
	expectApplied(mock, "timescaledb_solar_m")
	if _, storeErr := NewSolarStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}
	expectApplied(mock, "timescaledb_duco_m")
	if _, storeErr := NewDucoStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewDucoStore: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}

// TestFreshMigrationCreatesHypertables verifies each created table is turned
// into a hypertable on a fresh database.
func TestFreshMigrationCreatesHypertables(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db := NewDBFromSQL(raw, testLogger())

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs("timescaledb_solar_m").
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
	for range 2 { // solar table and its inverters table
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("SELECT create_hypertable").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("INSERT INTO meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if _, storeErr := NewSolarStore(context.Background(), db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}
