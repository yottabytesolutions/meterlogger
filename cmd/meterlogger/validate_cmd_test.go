package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/config"
)

// swapCfg replaces the package config global and restores it on cleanup.
func swapCfg(t *testing.T, c config.Config) {
	t.Helper()
	orig := cfg
	cfg = c
	t.Cleanup(func() { cfg = orig })
}

func TestRunValidate(t *testing.T) {
	origPing := pingSinks
	t.Cleanup(func() { pingSinks = origPing })
	pingSinks = false

	tests := []struct {
		name string
		cfg  config.Config
		want int
	}{
		{"empty config is invalid", config.Config{}, 1},
		{
			"valid config",
			config.Config{
				QuestDB: config.QuestDBConfig{Enabled: true},
				Grid:    config.GridConfig{Enabled: true, SerialInterface: "/dev/ttyUSB0"},
			},
			0,
		},
		{
			"enabled sink with missing fields is invalid",
			config.Config{
				Postgres: config.PostgresConfig{Enabled: true},
				Grid:     config.GridConfig{Enabled: true, SerialInterface: "/dev/ttyUSB0"},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			swapCfg(t, tt.cfg)
			if got := runValidate(context.Background()); got != tt.want {
				t.Errorf("runValidate() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRunPings(t *testing.T) {
	ok := func(context.Context) error { return nil }
	fail := func(context.Context) error { return errors.New("connection refused") }

	tests := []struct {
		name     string
		pingers  []sinkPinger
		want     int
		wantOut  []string
		notInOut []string
	}{
		{"no pingers", nil, 0, nil, nil},
		{
			"all ok",
			[]sinkPinger{{"questdb", ok}, {"postgres", ok}},
			0,
			[]string{"questdb: ok\n", "postgres: ok\n"},
			nil,
		},
		{
			"one failure fails the run but pings the rest",
			[]sinkPinger{{"questdb", fail}, {"postgres", ok}},
			1,
			[]string{"questdb: connection refused\n", "postgres: ok\n"},
			[]string{"questdb: ok"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			if got := runPings(context.Background(), &out, tt.pingers); got != tt.want {
				t.Errorf("runPings() = %d, want %d", got, tt.want)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q missing %q", out.String(), want)
				}
			}
			for _, notWant := range tt.notInOut {
				if strings.Contains(out.String(), notWant) {
					t.Errorf("output %q should not contain %q", out.String(), notWant)
				}
			}
		})
	}
}

func TestBuildSinkPingers(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		want []string
	}{
		{"no sinks enabled", config.Config{}, nil},
		{
			"only enabled sinks, in runtime order",
			config.Config{
				QuestDB:  config.QuestDBConfig{Enabled: true},
				Postgres: config.PostgresConfig{Enabled: true},
				TDEngine: config.TDEngineConfig{Enabled: true},
			},
			[]string{config.SinkQuestDB, config.SinkPostgres, config.SinkTDEngine},
		},
		{
			"all sinks enabled",
			config.Config{
				QuestDB:     config.QuestDBConfig{Enabled: true},
				Postgres:    config.PostgresConfig{Enabled: true},
				MySQL:       config.MySQLConfig{Enabled: true},
				TimescaleDB: config.TimescaleDBConfig{Enabled: true},
				ClickHouse:  config.ClickHouseConfig{Enabled: true},
				TDEngine:    config.TDEngineConfig{Enabled: true},
			},
			[]string{
				config.SinkQuestDB, config.SinkPostgres, config.SinkMySQL,
				config.SinkTimescaleDB, config.SinkClickHouse, config.SinkTDEngine,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			swapCfg(t, tt.cfg)
			pingers := buildSinkPingers()
			var names []string
			for _, p := range pingers {
				names = append(names, p.name)
				if p.ping == nil {
					t.Errorf("pinger %s has nil ping func", p.name)
				}
			}
			if len(names) != len(tt.want) {
				t.Fatalf("pinger names = %v, want %v", names, tt.want)
			}
			for i, want := range tt.want {
				if names[i] != want {
					t.Errorf("pinger[%d] = %s, want %s", i, names[i], want)
				}
			}
		})
	}
}

func TestPingSQLSink(t *testing.T) {
	ctx := context.Background()

	t.Run("open failure is returned", func(t *testing.T) {
		wantErr := errors.New("dial failed")
		err := pingSQLSink(ctx, func() (*fakeCheckCloser, error) { return nil, wantErr })
		if !errors.Is(err, wantErr) {
			t.Errorf("pingSQLSink() = %v, want %v", err, wantErr)
		}
	})

	t.Run("check failure is returned and connection closed", func(t *testing.T) {
		db := &fakeCheckCloser{checkErr: errors.New("ping failed")}
		err := pingSQLSink(ctx, func() (*fakeCheckCloser, error) { return db, nil })
		if !errors.Is(err, db.checkErr) {
			t.Errorf("pingSQLSink() = %v, want %v", err, db.checkErr)
		}
		if !db.closed {
			t.Error("connection not closed after check failure")
		}
	})

	t.Run("close failure is returned when check succeeds", func(t *testing.T) {
		db := &fakeCheckCloser{closeErr: errors.New("close failed")}
		err := pingSQLSink(ctx, func() (*fakeCheckCloser, error) { return db, nil })
		if !errors.Is(err, db.closeErr) {
			t.Errorf("pingSQLSink() = %v, want %v", err, db.closeErr)
		}
	})

	t.Run("success", func(t *testing.T) {
		db := &fakeCheckCloser{}
		if err := pingSQLSink(ctx, func() (*fakeCheckCloser, error) { return db, nil }); err != nil {
			t.Errorf("pingSQLSink() = %v, want nil", err)
		}
		if !db.closed {
			t.Error("connection not closed on success")
		}
	})
}

type fakeCheckCloser struct {
	checkErr error
	closeErr error
	closed   bool
}

func (f *fakeCheckCloser) Check(_ context.Context) error { return f.checkErr }

func (f *fakeCheckCloser) Close() error {
	f.closed = true
	return f.closeErr
}
