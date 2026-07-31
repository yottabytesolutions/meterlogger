package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// resetViper clears global viper state and points $HOME at an empty temp
// directory, so a real ~/.meterlogger.yaml on the machine running the tests
// cannot leak into them.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(viper.Reset)
}

func TestLoad_DefaultsNoFile(t *testing.T) {
	resetViper(t)

	cfg, err := Load("", testLogger())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Debug {
		t.Error("Load() should default Debug to false")
	}
	// A non-zero default proves the defaults were actually registered; an
	// unset key would also read as false above.
	if cfg.QuestDB.Port != 9009 {
		t.Errorf("QuestDB.Port default = %d, want 9009", cfg.QuestDB.Port)
	}
	if cfg.HTTPServer.Port != 8080 {
		t.Errorf("HTTPServer.Port default = %d, want 8080", cfg.HTTPServer.Port)
	}
}

func TestLoad_EnvOnlyOverride(t *testing.T) {
	resetViper(t)
	t.Setenv("QUESTDB_ENABLED", "true")
	t.Setenv("QUESTDB_PORT", "9123")

	cfg, err := Load("", testLogger())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.QuestDB.Enabled {
		t.Error("QUESTDB_ENABLED=true should enable the QuestDB sink without a config file")
	}
	if cfg.QuestDB.Port != 9123 {
		t.Errorf("QuestDB.Port = %d, want 9123 from QUESTDB_PORT", cfg.QuestDB.Port)
	}
}

func TestLoad_WithExplicitFile(t *testing.T) {
	resetViper(t)
	path := writeTempConfig(t, "Debug: true\n")

	cfg, err := Load(path, testLogger())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Debug {
		t.Error("Load() should read Debug=true from the config file")
	}
}

func TestLoad_MalformedFileFails(t *testing.T) {
	resetViper(t)
	path := writeTempConfig(t, "Debug: [unclosed\n")

	if _, err := Load(path, testLogger()); err == nil {
		t.Error("Load() should fail on a malformed config file")
	}
}

func TestLoad_EnabledFields(t *testing.T) {
	yamlContent := `
Heat:
  Enabled: true
  Measurement: heat_meter
  SerialInterface: /dev/ttyUSB0
  MbusAddress: 1
  ScrapeInterval: 30s
Grid:
  Enabled: false
  Measurement: grid_meter
  SerialInterface: /dev/ttyUSB1
Enphase:
  Enabled: false
  Measurement: solar
Ventilation:
  Enabled: true
  MeasurementBaseName: ventilation
  ScrapeInterval: 1m
  HostUrl: http://192.168.1.200
`
	resetViper(t)
	path := writeTempConfig(t, yamlContent)

	cfg, err := Load(path, testLogger())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Heat.Enabled {
		t.Error("Heat.Enabled should be true")
	}
	if cfg.Grid.Enabled {
		t.Error("Grid.Enabled should be false")
	}
	if cfg.Enphase.Enabled {
		t.Error("Enphase.Enabled should be false")
	}
	if !cfg.Ventilation.Enabled {
		t.Error("Ventilation.Enabled should be true")
	}
}

func TestSetSinkDefaults(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	setSinkDefaults()

	tests := []struct {
		key  string
		want any
	}{
		{"QuestDB.Port", 9009},
		{"Postgres.Port", 5432},
		{"Postgres.SSLMode", "disable"},
		{"MySQL.Port", 3306},
		{"TimescaleDB.Port", 5432},
		{"TimescaleDB.SSLMode", "disable"},
		{"ClickHouse.Port", 9000},
		{"ClickHouse.User", "default"},
		{"TDEngine.Port", 6041},
		{"TDEngine.User", "root"},
		{"TDEngine.Password", "taosdata"},
	}

	for _, tt := range tests {
		switch want := tt.want.(type) {
		case int:
			if got := viper.GetInt(tt.key); got != want {
				t.Errorf("%s = %d, want %d", tt.key, got, want)
			}
		case string:
			if got := viper.GetString(tt.key); got != want {
				t.Errorf("%s = %q, want %q", tt.key, got, want)
			}
		}
	}
}

func TestIsConfigFileNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"not found error", viper.ConfigFileNotFoundError{}, true},
		{"wrapped not found error", fmt.Errorf("read config: %w", viper.ConfigFileNotFoundError{}), true},
		{"other error", errors.New("permission denied"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConfigFileNotFound(tt.err); got != tt.want {
				t.Errorf("isConfigFileNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meterlogger-test.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}
