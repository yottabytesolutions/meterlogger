// Package config defines the application configuration, loads it from file,
// environment, and flags via viper, and validates it before startup.
package config

import (
	"fmt"
	"reflect"
	"time"

	"github.com/spf13/viper"
)

// Sink names as used in log messages, validation errors, and sink wiring.
const (
	SinkQuestDB     = "questdb"
	SinkStdout      = "stdout"
	SinkPostgres    = "postgres"
	SinkMySQL       = "mysql"
	SinkTimescaleDB = "timescaledb"
	SinkClickHouse  = "clickhouse"
	SinkTDEngine    = "tdengine"
)

// Source names as used by the --source flag.
const (
	SourceHeat        = "heat"
	SourceGrid        = "grid"
	SourceSolar       = "solar"
	SourceVentilation = "ventilation"
)

// Heat.Reader values selecting the physical interface to the heat meter.
const (
	HeatReaderMbus       = "mbus"
	HeatReaderOptical    = "optical"
	HeatReaderOptical401 = "optical401"
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

// HeatConfig configures the heat meter reader. Reader selects the physical
// interface: "mbus" (default), "optical" for the Kamstrup KMP IR eye, or
// "optical401" for the pre-KMP Multical 401 and 66C IR eye. MbusAddress only
// applies to the mbus reader; Optical401 only applies to the optical401
// reader.
type HeatConfig struct {
	Enabled         bool
	Measurement     string
	Reader          string
	SerialInterface string
	MbusAddress     int
	ScrapeInterval  time.Duration
	Optical401      Optical401Config
}

// Optical401Config sets the value scaling for the optical401 reader. The
// Multical 401/66C sends plain digit fields whose unit and decimal position
// depend on the meter's CCC configuration code, so they must be configured.
// The defaults match common Dutch district heating installs: energy in GJ
// with 3 decimals, volume in m3 with 3 decimals, power in kW with 1 decimal,
// flow in l/h with 1 decimal. Verify one reading against the meter LCD.
type Optical401Config struct {
	EnergyUnit     string // GJ (default), kWh, or MWh
	EnergyDecimals int
	VolumeDecimals int
	PowerDecimals  int
	FlowDecimals   int
}

// GridConfig configures the grid meter reader.
// DecryptionKey enables decryption of DLMS-encrypted P1 telegrams
// (Luxembourg Smarty, Austrian Sagemcom T210-D); empty means plaintext only.
// AuthenticationKey defaults to the fixed Luxembourg AK; Austrian users
// override it with the GAK from their grid operator. Both are 32 hex chars.
type GridConfig struct {
	Enabled           bool
	Measurement       string
	SerialInterface   string
	DecryptionKey     string
	AuthenticationKey string
	Gas               GasConfig
}

// GasConfig configures storage of gas meter readings carried in the grid
// meter's P1 telegrams. Gas is not a standalone source: readings only flow
// when the grid source runs.
type GasConfig struct {
	Enabled     bool
	Measurement string
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
