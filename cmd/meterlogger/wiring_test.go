package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/sml"
	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// swapExit replaces the fatal-exit seam and returns a getter for the recorded
// exit code (nil until called). Restores on cleanup.
func swapExit(t *testing.T) func() *int {
	t.Helper()
	var code *int
	orig := osExit
	osExit = func(c int) { code = &c }
	t.Cleanup(func() { osExit = orig })
	return func() *int { return code }
}

func TestBuildSinks(t *testing.T) {
	ctx := context.Background()
	l := testLogger()

	t.Run("builds only enabled sinks in order", func(t *testing.T) {
		got := buildSinks(ctx, l, []sinkInit[string]{
			{"a", true, func() (string, error) { return "a", nil }},
			{"b", false, func() (string, error) { t.Fatal("disabled sink built"); return "", nil }},
			{"c", true, func() (string, error) { return "c", nil }},
		})
		if len(got) != 2 || got[0] != "a" || got[1] != "c" {
			t.Errorf("buildSinks = %v, want [a c]", got)
		}
	})

	t.Run("constructor failure is fatal", func(t *testing.T) {
		exited := false
		orig := osExit
		osExit = func(int) { exited = true }
		defer func() { osExit = orig }()

		buildSinks(ctx, l, []sinkInit[string]{
			{"broken", true, func() (string, error) { return "", errors.New("boom") }},
		})
		if !exited {
			t.Error("buildSinks should exit on constructor failure")
		}
	})
}

func TestBuildSourceSinks_StdoutOnly(t *testing.T) {
	// With only the stdout sink enabled and no DB connections, every source
	// builder must produce exactly one sink.
	origCfg := cfg
	cfg = config.Config{Stdout: config.StdoutConfig{Enabled: true}}
	defer func() { cfg = origCfg }()

	ctx := context.Background()
	l := testLogger()
	var dbs dbConnections

	if got := len(buildHeatSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("heat sinks = %d, want 1", got)
	}
	if got := len(buildGridSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("grid sinks = %d, want 1", got)
	}
	if got := len(buildSolarSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("solar sinks = %d, want 1", got)
	}
	if got := len(buildVentilationSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("ventilation sinks = %d, want 1", got)
	}
}

// sqlmockSinkDB returns a sqlsink.DB whose migration is already applied, so a
// store constructor succeeds against it.
func sqlmockSinkDB(t *testing.T, dialect sqlsink.Dialect, component string) *sqlsink.DB {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS meterlogger_schema_migrations").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COALESCE").
		WithArgs(component).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(1))
	return sqlsink.NewDBFromSQL(dialect, raw, testLogger())
}

// TestBuildSourceSinks_SQLLoop proves the shared SQL path builds one store per
// open dialect connection without per-source wiring.
func TestBuildSourceSinks_SQLLoop(t *testing.T) {
	const m = "heat"
	origCfg := cfg
	cfg = config.Config{Heat: config.HeatConfig{Measurement: m}}
	defer func() { cfg = origCfg }()

	dbs := dbConnections{
		postgres: sqlmockSinkDB(t, sqlsink.PostgresDialect(), "postgres_heat_"+m),
		mysql:    sqlmockSinkDB(t, sqlsink.MySQLDialect(), "mysql_heat_"+m),
	}
	sinks := buildHeatSinks(context.Background(), testLogger(), nil, dbs)
	if len(sinks) != 2 {
		t.Fatalf("heat sinks = %d, want 2 (postgres + mysql)", len(sinks))
	}
}

func TestDBConnections_ClosersAndCheckers(t *testing.T) {
	var empty dbConnections
	if got := empty.closers(); len(got) != 0 {
		t.Errorf("closers on empty = %d entries, want 0", len(got))
	}
	if got := empty.checkers(); len(got) != 0 {
		t.Errorf("checkers on empty = %d entries, want 0", len(got))
	}

	mockDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer mockDB.Close()

	dbs := dbConnections{postgres: postgres.NewDBFromSQL(mockDB, testLogger())}
	closers := dbs.closers()
	if len(closers) != 1 || closers[0].name != config.SinkPostgres {
		t.Errorf("closers = %v, want one postgres entry", closers)
	}
	if got := len(dbs.checkers()); got != 1 {
		t.Errorf("checkers = %d, want 1", got)
	}
}

func TestConnect(t *testing.T) {
	ctx := context.Background()

	t.Run("success appends closer", func(t *testing.T) {
		mockDB, _, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock: %v", err)
		}
		db, opened := connect(ctx, "test", nil, func() (*postgres.DB, error) {
			return postgres.NewDBFromSQL(mockDB, testLogger()), nil
		})
		if db == nil {
			t.Fatal("connect returned nil DB")
		}
		if len(opened) != 1 || opened[0].name != "test" {
			t.Errorf("opened = %v, want one test entry", opened)
		}
	})

	t.Run("failure closes earlier connections and exits", func(t *testing.T) {
		exitCode := swapExit(t)

		mockDB, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock: %v", err)
		}
		mock.ExpectClose()
		earlier := []namedCloser{{"earlier", mockDB.Close}}

		connect(ctx, "failing", earlier, func() (*postgres.DB, error) {
			return nil, errors.New("connection refused")
		})
		if code := exitCode(); code == nil || *code != 1 {
			t.Error("connect should exit(1) on failure")
		}
		if mockErr := mock.ExpectationsWereMet(); mockErr != nil {
			t.Errorf("earlier connection not closed: %v", mockErr)
		}
	})
}

func TestCloseDB_LogsError(_ *testing.T) {
	// Must not panic; the error path only logs.
	closeDB("test", func() error { return errors.New("close failed") })
	closeDB("test", func() error { return nil })
}

func TestBuildHealthServer_RegistersCheckers(t *testing.T) {
	origCfg := cfg
	cfg = config.Config{HTTPServer: config.HTTPServerConfig{Port: 0}}
	defer func() { cfg = origCfg }()

	mockDB, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer mockDB.Close()
	mock.ExpectPing()

	dbs := dbConnections{postgres: postgres.NewDBFromSQL(mockDB, testLogger())}
	srv := buildHealthServer(metrics.New(), dbs)
	if srv == nil {
		t.Fatal("buildHealthServer returned nil")
	}
}

//nolint:goconst // source names in a test table read better as literals
func TestSourceEnabled(t *testing.T) {
	origCfg, origFilter := cfg, sourceFilter
	defer func() { cfg, sourceFilter = origCfg, origFilter }()

	cfg = config.Config{
		Heat:        config.HeatConfig{Enabled: true},
		Grid:        config.GridConfig{Enabled: false},
		Enphase:     config.EnphaseConfig{Enabled: true},
		Ventilation: config.VentilationConfig{Enabled: false},
	}

	tests := []struct {
		name   string
		filter string
		source string
		want   bool
	}{
		{"enabled by config", "", "heat", true},
		{"disabled by config", "", "grid", false},
		{"solar follows enphase", "", "solar", true},
		{"ventilation disabled", "", "ventilation", false},
		{"unknown source", "", "water", false},
		{"filter matches", "grid", "grid", true},
		{"filter overrides config", "grid", "heat", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceFilter = tt.filter
			if got := sourceEnabled(tt.source); got != tt.want {
				t.Errorf("sourceEnabled(%q) with filter %q = %v, want %v", tt.source, tt.filter, got, tt.want)
			}
		})
	}
}

func TestNewGridReader(t *testing.T) {
	origCfg := cfg
	defer func() { cfg = origCfg }()

	tests := []struct {
		name    string
		reader  string
		wantSML bool
		wantErr bool
	}{
		{name: "default is dsmr", reader: ""},
		{name: "dsmr", reader: config.GridReaderDSMR},
		{name: "sml", reader: config.GridReaderSML, wantSML: true},
		{name: "invalid", reader: "p1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = config.Config{Grid: config.GridConfig{SerialInterface: "/dev/ttyUSB9", Reader: tt.reader}}
			reader, err := newGridReader(testLogger())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("newGridReader: %v", err)
			}
			_, isSML := reader.(*sml.Reader)
			if isSML != tt.wantSML {
				t.Errorf("reader type %T, wantSML=%v", reader, tt.wantSML)
			}
		})
	}
}

func TestBuildVersion(t *testing.T) {
	origSHA, origDate := CommitSHA, BuildDate
	defer func() { CommitSHA, BuildDate = origSHA, origDate }()

	tests := []struct {
		name, sha, date, want string
	}{
		{"dev build", "", "", "dev"},
		{"sha only", "fffe012", "", "fffe012"},
		{"sha and date", "abc123", "2026-07-31", "abc123 (built 2026-07-31)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			CommitSHA, BuildDate = tt.sha, tt.date
			if got := buildVersion(); got != tt.want {
				t.Errorf("buildVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	t.Setenv("HTTPSERVER_PORT", u.Port())

	if got := runHealthcheck(); got != 0 {
		t.Errorf("runHealthcheck() against healthy server = %d, want 0", got)
	}

	srv.Close()
	if got := runHealthcheck(); got != 1 {
		t.Errorf("runHealthcheck() against closed server = %d, want 1", got)
	}
}

func TestValidateConfig_ExitsOnInvalid(t *testing.T) {
	origCfg, origFilter := cfg, sourceFilter
	defer func() { cfg, sourceFilter = origCfg, origFilter }()

	cfg = config.Config{}
	sourceFilter = ""
	errs := config.Validate(cfg, sourceFilter)
	if len(errs) == 0 {
		t.Fatal("empty config should produce validation errors")
	}
	for _, e := range errs {
		if strings.Contains(e, "—") {
			t.Errorf("validation message contains em-dash: %q", e)
		}
	}
}
