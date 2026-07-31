package tdengine

import (
	"context"
	"log/slog"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/taosdata/driver-go/v3/taosRestful"
)

const testHost = "td.local"

//nolint:gosec // G101: not a credential, exercises DSN escaping of special characters.
const trickyPassword = `p @ss/w:ord?&=()`

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func checkDSN(t *testing.T, cfg Config) {
	t.Helper()
	parsed, err := taosRestful.ParseDSN(buildDSN(cfg))
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if parsed.User != cfg.User {
		t.Errorf("user = %q, want %q", parsed.User, cfg.User)
	}
	if parsed.Passwd != cfg.Password {
		t.Errorf("password = %q, want %q", parsed.Passwd, cfg.Password)
	}
	if parsed.Net != "http" {
		t.Errorf("net = %q, want http", parsed.Net)
	}
	if parsed.Addr != testHost || parsed.Port != 6041 {
		t.Errorf("addr = %q:%d, want %s:6041", parsed.Addr, parsed.Port, testHost)
	}
	if parsed.DbName != cfg.Database {
		t.Errorf("dbname = %q, want %q", parsed.DbName, cfg.Database)
	}
	if parsed.ReadBufferSize != readBufferSize {
		t.Errorf("readBufferSize = %d, want %d", parsed.ReadBufferSize, readBufferSize)
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "plain credentials",
			cfg:  Config{Host: testHost, Port: 6041, User: "root", Password: "taosdata", Database: "meters"},
		},
		{
			name: "password with special characters",
			cfg: Config{
				Host: testHost, Port: 6041, User: "user@corp",
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

// TDEngine's migrator tracks versions via SELECT MAX(version).
func expectApplied(mock sqlmock.Sqlmock, component string) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT MAX").
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
}

// TestWiring exercises the delegation into sqlsink with the tdengine dialect:
// health check name and the migration ledger component keys.
func TestWiring(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	db := NewDBFromSQL(raw, testLogger())
	if name := db.Name(); name != "tdengine" {
		t.Errorf("Name() = %q, want tdengine", name)
	}

	ctx := context.Background()
	expectApplied(mock, "tdengine_heat_m")
	if _, storeErr := NewHeatStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewHeatStore: %v", storeErr)
	}
	expectApplied(mock, "tdengine_grid_m")
	if _, storeErr := NewGridStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewGridStore: %v", storeErr)
	}
	expectApplied(mock, "tdengine_solar_m")
	if _, storeErr := NewSolarStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewSolarStore: %v", storeErr)
	}
	expectApplied(mock, "tdengine_duco_m")
	if _, storeErr := NewDucoStore(ctx, db, "m", testLogger()); storeErr != nil {
		t.Fatalf("NewDucoStore: %v", storeErr)
	}
	if metErr := mock.ExpectationsWereMet(); metErr != nil {
		t.Error(metErr)
	}
}
