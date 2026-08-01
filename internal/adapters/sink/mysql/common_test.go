package mysql

import (
	"context"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	gomysql "github.com/go-sql-driver/mysql"
)

//nolint:gosec // G101: not a credential, exercises DSN escaping of special characters.
const trickyPassword = `p @ss/w:ord?&=()`

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func checkDSN(t *testing.T, cfg Config) {
	t.Helper()
	parsed, err := gomysql.ParseDSN(buildDSN(cfg))
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if parsed.User != cfg.User {
		t.Errorf("user = %q, want %q", parsed.User, cfg.User)
	}
	if parsed.Passwd != cfg.Password {
		t.Errorf("password = %q, want %q", parsed.Passwd, cfg.Password)
	}
	if parsed.Addr != "db.local:3306" {
		t.Errorf("addr = %q, want db.local:3306", parsed.Addr)
	}
	if parsed.DBName != cfg.Database {
		t.Errorf("dbname = %q, want %q", parsed.DBName, cfg.Database)
	}
	if !parsed.ParseTime {
		t.Error("ParseTime not set")
	}
	if !parsed.MultiStatements {
		t.Error("MultiStatements not set")
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "plain credentials",
			cfg:  Config{Host: "db.local", Port: 3306, User: "u", Password: "p", Database: "meters"},
		},
		{
			name: "password with special characters",
			cfg: Config{
				Host: "db.local", Port: 3306, User: "user@corp",
				Password: trickyPassword, Database: "meters",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkDSN(t, tt.cfg)
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

// TestWiring exercises the delegation into sqlsink with the mysql dialect:
// health check name and the migration ledger component keys.
func TestWiring(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db := NewDBFromSQL(raw, testLogger())
	if name := db.Name(); name != "mysql" {
		t.Errorf("Name() = %q, want mysql", name)
	}

	ctx := context.Background()
	expectApplied(mock, "mysql_heat_m")
	if _, storeErr := NewHeatStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}
	expectApplied(mock, "mysql_grid_m")
	if _, storeErr := NewGridStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}
	expectApplied(mock, "mysql_solar_m")
	if _, storeErr := NewSolarStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}
	expectApplied(mock, "mysql_duco_m")
	if _, storeErr := NewDucoStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewDucoStore: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}
