package main

import (
	"fmt"
	"reflect"
	"time"

	"github.com/spf13/viper"
)

//nolint:gochecknoglobals // build info globals, set at link time
var (
	CommitSHA string
	BuildDate string
)

// Config is the top-level application configuration.
type Config struct {
	Debug         bool
	FlushInterval time.Duration
	HTTPServer    HTTPServerConfig
	Enphase       EnphaseConfig
	Heat          HeatConfig
	Grid          GridConfig
	QuestDB       QuestDBConfig
	Stdout        StdoutConfig
	Postgres      PostgresConfig
	MySQL         MySQLConfig
	TimescaleDB   TimescaleDBConfig
	ClickHouse    ClickHouseConfig
	TDEngine      TDEngineConfig
	Ventilation   VentilationConfig
	OTEL          OTELConfig
	Profiling     ProfilingConfig
}

// StdoutConfig configures the stdout debug sink, which logs readings instead
// of persisting them. Not for production use.
type StdoutConfig struct {
	Enabled bool
}

// OTELConfig configures OpenTelemetry.
type OTELConfig struct {
	Enabled       bool
	CollectorAddr string // e.g. "localhost:4317"
	ServiceName   string
}

// ProfilingConfig configures Grafana Pyroscope continuous profiling.
type ProfilingConfig struct {
	Enabled           bool
	ServerAddr        string // e.g. "http://pyroscope:4040"
	BasicAuthUser     string
	BasicAuthPassword string
	ServiceName       string
}

// HTTPServerConfig configures the health/metrics HTTP server.
type HTTPServerConfig struct {
	Port int
	// LivenessFailureThreshold is the duration any registered health check
	// must be continuously failing before /healthz starts returning 503.
	// /readyz still flips on the first failure; /healthz only flips once
	// the failure has persisted long enough that the kubelet should
	// restart the container instead of waiting for self-recovery.
	LivenessFailureThreshold time.Duration
}

// EnphaseConfig configures the Enphase Envoy solar reader.
type EnphaseConfig struct {
	Enabled        bool
	Measurement    string
	User           string
	Password       string
	Serial         string
	EnvoyURL       string
	ScrapeInterval time.Duration
}

// HeatConfig configures the heat meter reader.
type HeatConfig struct {
	Enabled         bool
	Measurement     string
	SerialInterface string
	MbusAddress     int
	ScrapeInterval  time.Duration
}

// GridConfig configures the grid meter reader.
type GridConfig struct {
	Enabled         bool
	Measurement     string
	SerialInterface string
}

// VentilationConfig configures the DucoBox ventilation reader.
type VentilationConfig struct {
	Enabled             bool
	MeasurementBaseName string
	ScrapeInterval      time.Duration
	HostURL             string
	Nodes               []int
}

// QuestDBConfig configures the QuestDB sink.
type QuestDBConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
}

// PostgresConfig configures the PostgreSQL sink.
type PostgresConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// MySQLConfig configures the MySQL sink.
type MySQLConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// TimescaleDBConfig configures the TimescaleDB sink.
type TimescaleDBConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// ClickHouseConfig configures the ClickHouse sink.
type ClickHouseConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// TDEngineConfig configures the TDEngine sink.
type TDEngineConfig struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// bindEnvKeys registers every Config key with viper so environment variables
// such as QUESTDB_ENABLED work without a config file. AutomaticEnv only
// resolves keys viper already knows about.
func bindEnvKeys() error {
	return bindStructEnvKeys(reflect.TypeFor[Config](), "")
}

func bindStructEnvKeys(t reflect.Type, prefix string) error {
	for field := range t.Fields() {
		key := field.Name
		if prefix != "" {
			key = prefix + "." + field.Name
		}
		if field.Type.Kind() == reflect.Struct {
			if err := bindStructEnvKeys(field.Type, key); err != nil {
				return err
			}
			continue
		}
		if err := viper.BindEnv(key); err != nil {
			return fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return nil
}
