package config

import (
	"strings"
	"testing"
)

const (
	testQuestDBHost = "questdb"
	testAdminUser   = "admin"
	testSerialUSB0  = "/dev/ttyUSB0"
	testEnvoyURL    = "url"
	testBrokerURL   = "tcp://broker:1883"
)

func TestValidate_Valid(t *testing.T) {
	cfg := Config{
		QuestDB: QuestDBConfig{Enabled: true, Host: testQuestDBHost, User: testAdminUser},
		Heat:    HeatConfig{Enabled: true, SerialInterface: testSerialUSB0},
	}

	if errs := Validate(cfg, ""); len(errs) != 0 {
		t.Errorf("Validate() = %v, want empty", errs)
	}
}

func TestValidate_NoSinks(t *testing.T) {
	cfg := Config{Heat: HeatConfig{Enabled: true, SerialInterface: testSerialUSB0}}

	errs := Validate(cfg, "")
	if !containsSubstring(errs, "no sinks enabled") {
		t.Errorf("Validate() = %v, want a no-sinks error", errs)
	}
}

func TestValidate_MQTTCountsAsSink(t *testing.T) {
	cfg := Config{
		MQTT: MQTTConfig{Enabled: true, BrokerURL: testBrokerURL, QoS: 1},
		Heat: HeatConfig{Enabled: true, SerialInterface: testSerialUSB0},
	}

	if errs := Validate(cfg, ""); len(errs) != 0 {
		t.Errorf("Validate() = %v, want empty; MQTT alone should satisfy the sink requirement", errs)
	}
}

func TestValidate_NoSources(t *testing.T) {
	cfg := Config{QuestDB: QuestDBConfig{Enabled: true, Host: testQuestDBHost, User: testAdminUser}}

	errs := Validate(cfg, "")
	if !containsSubstring(errs, "no sources enabled") {
		t.Errorf("Validate() = %v, want a no-sources error", errs)
	}
}

func TestValidate_InvalidSourceFilter(t *testing.T) {
	cfg := Config{QuestDB: QuestDBConfig{Enabled: true, Host: testQuestDBHost, User: testAdminUser}}

	errs := Validate(cfg, "bogus")
	if !containsSubstring(errs, "invalid --source value") {
		t.Errorf("Validate() = %v, want an invalid --source error", errs)
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
			name:    "mqtt enabled without broker URL",
			cfg:     Config{MQTT: MQTTConfig{Enabled: true, QoS: 1}},
			wantErr: "mqtt sink enabled but BrokerURL is empty",
		},
		{
			name:    "mqtt enabled with invalid QoS",
			cfg:     Config{MQTT: MQTTConfig{Enabled: true, BrokerURL: testBrokerURL, QoS: 2}},
			wantErr: "invalid MQTT.QoS 2",
		},
		{
			name:    "mqtt disabled with invalid QoS produces no error",
			cfg:     Config{MQTT: MQTTConfig{QoS: 7}},
			wantErr: "",
		},
		{
			name:    "mqtt enabled with broker URL and QoS 0 produces no error",
			cfg:     Config{MQTT: MQTTConfig{Enabled: true, BrokerURL: testBrokerURL, QoS: 0}},
			wantErr: "",
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
			name:    "heat with invalid reader",
			cfg:     Config{Heat: HeatConfig{Enabled: true, SerialInterface: testSerialUSB0, Reader: "infrared"}},
			wantErr: `invalid Heat.Reader "infrared"`,
		},
		{
			name:    "heat optical reader without serial interface",
			cfg:     Config{Heat: HeatConfig{Enabled: true, Reader: HeatReaderOptical}},
			wantErr: "heat source enabled but SerialInterface is empty",
		},
		{
			name: "heat optical reader with serial interface produces no error",
			cfg: Config{
				Heat: HeatConfig{Enabled: true, SerialInterface: testSerialUSB0, Reader: HeatReaderOptical},
			},
			wantErr: "",
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

func TestSourceFieldErrors_Optical401(t *testing.T) {
	heat := func(o Optical401Config) Config {
		return Config{Heat: HeatConfig{
			Enabled: true, SerialInterface: testSerialUSB0, Reader: HeatReaderOptical401, Optical401: o,
		}}
	}
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "zero-value scaling produces no error",
			cfg:     heat(Optical401Config{}),
			wantErr: "",
		},
		{
			name: "valid scaling produces no error",
			cfg: heat(Optical401Config{
				EnergyUnit: "kWh", EnergyDecimals: 2, VolumeDecimals: 3, PowerDecimals: 1, FlowDecimals: 4,
			}),
			wantErr: "",
		},
		{
			name:    "invalid energy unit",
			cfg:     heat(Optical401Config{EnergyUnit: "cal"}),
			wantErr: `invalid Heat.Optical401.EnergyUnit "cal"`,
		},
		{
			name:    "negative decimals",
			cfg:     heat(Optical401Config{EnergyUnit: "GJ", VolumeDecimals: -1}),
			wantErr: "invalid Heat.Optical401.VolumeDecimals -1",
		},
		{
			name:    "too many decimals",
			cfg:     heat(Optical401Config{EnergyUnit: "GJ", FlowDecimals: 5}),
			wantErr: "invalid Heat.Optical401.FlowDecimals 5",
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

func TestGasFieldErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "gas enabled without measurement",
			cfg: Config{
				Grid: GridConfig{
					Enabled:         true,
					SerialInterface: testSerialUSB0,
					Gas:             GasConfig{Enabled: true},
				},
			},
			wantErr: "grid gas readings enabled but Grid.Gas.Measurement is empty",
		},
		{
			name: "gas enabled with measurement produces no error",
			cfg: Config{
				Grid: GridConfig{
					Enabled:         true,
					SerialInterface: testSerialUSB0,
					Gas:             GasConfig{Enabled: true, Measurement: "gas_meter"},
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
