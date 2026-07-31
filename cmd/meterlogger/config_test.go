package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestInitConfig_DefaultsNoFile(t *testing.T) {
	// Reset viper state
	viper.Reset()
	cfgFile = ""

	// Run initConfig with no config file present
	// It should not panic; it sets defaults and tries to read config (harmlessly failing)
	initConfig()

	if viper.GetBool("Debug") {
		t.Error("initConfig() should set Debug default to false")
	}
	// A non-zero default proves the defaults were actually registered; an
	// unset key would also read as false above.
	if got := viper.GetInt("QuestDB.Port"); got != 9009 {
		t.Errorf("QuestDB.Port default = %d, want 9009", got)
	}
}

func TestInitConfig_EnvOnlyOverride(t *testing.T) {
	viper.Reset()
	cfgFile = ""
	t.Setenv("QUESTDB_ENABLED", "true")
	t.Setenv("QUESTDB_PORT", "9123")

	initConfig()

	if !config.QuestDB.Enabled {
		t.Error("QUESTDB_ENABLED=true should enable the QuestDB sink without a config file")
	}
	if config.QuestDB.Port != 9123 {
		t.Errorf("QuestDB.Port = %d, want 9123 from QUESTDB_PORT", config.QuestDB.Port)
	}
}

func TestInitConfig_WithExplicitFile(t *testing.T) {
	// Create a temp config file
	f, err := os.CreateTemp(t.TempDir(), "meterlogger-test-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	_, _ = f.WriteString("Debug: true\n")
	_ = f.Close()

	viper.Reset()
	cfgFile = f.Name()
	defer func() { cfgFile = "" }()

	initConfig()

	if !viper.GetBool("Debug") {
		t.Error("initConfig() should load Debug=true from config file")
	}
}

func TestInitConfig_EnabledFields(t *testing.T) {
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
	f, err := os.CreateTemp(t.TempDir(), "meterlogger-enabled-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())

	_, _ = f.WriteString(yamlContent)
	_ = f.Close()

	viper.Reset()
	cfgFile = f.Name()
	defer func() { cfgFile = "" }()

	initConfig()

	if !config.Heat.Enabled {
		t.Error("Heat.Enabled should be true")
	}
	if config.Grid.Enabled {
		t.Error("Grid.Enabled should be false")
	}
	if config.Enphase.Enabled {
		t.Error("Enphase.Enabled should be false")
	}
	if !config.Ventilation.Enabled {
		t.Error("Ventilation.Enabled should be true")
	}
}

func TestSetSinkDefaults(t *testing.T) {
	viper.Reset()
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

const (
	testQuestDBHost = "questdb"
	testAdminUser   = "admin"
	testSerialUSB0  = "/dev/ttyUSB0"
	testEnvoyURL    = "url"
)

func TestConfigValidationErrors_Valid(t *testing.T) {
	cfg := Config{
		QuestDB: QuestDBConfig{Enabled: true, Host: testQuestDBHost, User: testAdminUser},
		Heat:    HeatConfig{Enabled: true, SerialInterface: testSerialUSB0},
	}

	if errs := configValidationErrors(cfg, ""); len(errs) != 0 {
		t.Errorf("configValidationErrors() = %v, want empty", errs)
	}
}

func TestConfigValidationErrors_NoSinks(t *testing.T) {
	cfg := Config{Heat: HeatConfig{Enabled: true, SerialInterface: testSerialUSB0}}

	errs := configValidationErrors(cfg, "")
	if !containsSubstring(errs, "no sinks enabled") {
		t.Errorf("configValidationErrors() = %v, want a no-sinks error", errs)
	}
}

func TestConfigValidationErrors_NoSources(t *testing.T) {
	cfg := Config{QuestDB: QuestDBConfig{Enabled: true, Host: testQuestDBHost, User: testAdminUser}}

	errs := configValidationErrors(cfg, "")
	if !containsSubstring(errs, "no sources enabled") {
		t.Errorf("configValidationErrors() = %v, want a no-sources error", errs)
	}
}

func TestConfigValidationErrors_InvalidSourceFilter(t *testing.T) {
	cfg := Config{QuestDB: QuestDBConfig{Enabled: true, Host: testQuestDBHost, User: testAdminUser}}

	errs := configValidationErrors(cfg, "bogus")
	if !containsSubstring(errs, "invalid --source value") {
		t.Errorf("configValidationErrors() = %v, want an invalid --source error", errs)
	}
}

func TestSinkFieldErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "postgres enabled without host",
			cfg:     Config{Postgres: PostgresConfig{Enabled: true, User: "u", Database: "d"}},
			wantErr: "postgres sink enabled but Host is empty",
		},
		{
			name:    "postgres enabled without user",
			cfg:     Config{Postgres: PostgresConfig{Enabled: true, Host: "h", Database: "d"}},
			wantErr: "postgres sink enabled but User is empty",
		},
		{
			name:    "postgres enabled without database",
			cfg:     Config{Postgres: PostgresConfig{Enabled: true, Host: "h", User: "u"}},
			wantErr: "postgres sink enabled but Database is empty",
		},
		{
			name:    "mysql enabled without host",
			cfg:     Config{MySQL: MySQLConfig{Enabled: true, User: "u", Database: "d"}},
			wantErr: "mysql sink enabled but Host is empty",
		},
		{
			name:    "timescaledb enabled without host",
			cfg:     Config{TimescaleDB: TimescaleDBConfig{Enabled: true, User: "u", Database: "d"}},
			wantErr: "timescaledb sink enabled but Host is empty",
		},
		{
			name:    "clickhouse enabled without database",
			cfg:     Config{ClickHouse: ClickHouseConfig{Enabled: true, Host: "h", User: "u"}},
			wantErr: "clickhouse sink enabled but Database is empty",
		},
		{
			name:    "tdengine enabled without user",
			cfg:     Config{TDEngine: TDEngineConfig{Enabled: true, Host: "h", Database: "d"}},
			wantErr: "tdengine sink enabled but User is empty",
		},
		{
			name:    "postgres disabled with empty fields produces no error",
			cfg:     Config{Postgres: PostgresConfig{Enabled: false}},
			wantErr: "",
		},
		{
			name: "fully populated sink produces no error",
			cfg: Config{
				Postgres: PostgresConfig{Enabled: true, Host: "h", User: "u", Database: "d"},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := sinkFieldErrors(tt.cfg)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Errorf("sinkFieldErrors() = %v, want empty", errs)
				}
				return
			}
			if !containsSubstring(errs, tt.wantErr) {
				t.Errorf("sinkFieldErrors() = %v, want to contain %q", errs, tt.wantErr)
			}
		})
	}
}

func TestSourceFieldErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "heat enabled without serial interface",
			cfg:     Config{Heat: HeatConfig{Enabled: true}},
			wantErr: "heat source enabled but SerialInterface is empty",
		},
		{
			name:    "grid enabled without serial interface",
			cfg:     Config{Grid: GridConfig{Enabled: true}},
			wantErr: "grid source enabled but SerialInterface is empty",
		},
		{
			name:    "ventilation enabled without host URL",
			cfg:     Config{Ventilation: VentilationConfig{Enabled: true}},
			wantErr: "ventilation source enabled but HostURL is empty",
		},
		{
			name:    "enphase enabled without envoy URL",
			cfg:     Config{Enphase: EnphaseConfig{Enabled: true, User: "u", Password: "p", Serial: "s"}},
			wantErr: "solar (enphase) source enabled but EnvoyURL is empty",
		},
		{
			name:    "enphase enabled without user",
			cfg:     Config{Enphase: EnphaseConfig{Enabled: true, EnvoyURL: testEnvoyURL, Password: "p", Serial: "s"}},
			wantErr: "solar (enphase) source enabled but User is empty",
		},
		{
			name:    "enphase enabled without password",
			cfg:     Config{Enphase: EnphaseConfig{Enabled: true, EnvoyURL: testEnvoyURL, User: "u", Serial: "s"}},
			wantErr: "solar (enphase) source enabled but Password is empty",
		},
		{
			name:    "enphase enabled without serial",
			cfg:     Config{Enphase: EnphaseConfig{Enabled: true, EnvoyURL: testEnvoyURL, User: "u", Password: "p"}},
			wantErr: "solar (enphase) source enabled but Serial is empty",
		},
		{
			name: "fully populated sources produce no error",
			cfg: Config{
				Heat:        HeatConfig{Enabled: true, SerialInterface: "/dev/ttyUSB0"},
				Grid:        GridConfig{Enabled: true, SerialInterface: "/dev/ttyUSB1"},
				Ventilation: VentilationConfig{Enabled: true, HostURL: "http://duco.local"},
				Enphase: EnphaseConfig{
					Enabled: true, EnvoyURL: testEnvoyURL, User: "u", Password: "p", Serial: "s",
				},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := sourceFieldErrors(tt.cfg)
			if tt.wantErr == "" {
				if len(errs) != 0 {
					t.Errorf("sourceFieldErrors() = %v, want empty", errs)
				}
				return
			}
			if !containsSubstring(errs, tt.wantErr) {
				t.Errorf("sourceFieldErrors() = %v, want to contain %q", errs, tt.wantErr)
			}
		})
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
