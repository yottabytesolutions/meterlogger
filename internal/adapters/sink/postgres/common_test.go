package postgres

import (
	"context"
	"log/slog"
	"net/url"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const dialectName = "postgres"

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
	if u.Scheme != dialectName {
		t.Errorf("scheme = %q, want %q", u.Scheme, dialectName)
	}
	if got := u.User.Username(); got != cfg.User {
		t.Errorf("user = %q, want %q", got, cfg.User)
	}
	password, ok := u.User.Password()
	if !ok || password != wantPassword {
		t.Errorf("password = %q (set=%v), want %q", password, ok, wantPassword)
	}
	if u.Host != "db.local:5432" {
		t.Errorf("host = %q, want db.local:5432", u.Host)
	}
	if u.Path != "/meters" {
		t.Errorf("path = %q, want /meters", u.Path)
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
				Host: "db.local", Port: 5432, User: "u", Password: "p", Database: "meters",
			},
			wantPassword: "p",
			wantSSLMode:  "disable",
		},
		{
			name: "password with special characters",
			cfg: Config{
				Host: "db.local", Port: 5432, User: "user@corp",
				Password: trickyPassword, Database: "meters", SSLMode: "require",
			},
			wantPassword: trickyPassword,
			wantSSLMode:  "require",
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

// TestWiring exercises the delegation into sqlsink with the postgres dialect:
// health check name and the migration ledger component keys.
func TestWiring(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db := NewDBFromSQL(raw, testLogger())
	if name := db.Name(); name != dialectName {
		t.Errorf("Name() = %q, want %q", name, dialectName)
	}

	ctx := context.Background()
	expectApplied(mock, "postgres_heat_m")
	if _, storeErr := NewHeatStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}
	expectApplied(mock, "postgres_grid_m")
	if _, storeErr := NewGridStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}
	expectApplied(mock, "postgres_solar_m")
	if _, storeErr := NewSolarStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}
	expectApplied(mock, "postgres_duco_m")
	if _, storeErr := NewDucoStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewDucoStore: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}
