package config

import "fmt"

// Validate returns every configuration problem found in cfg, given the
// --source filter in effect. An empty slice means the configuration is valid.
func Validate(cfg Config, sourceFilter string) []string {
	var errs []string

	if !cfg.QuestDB.Enabled && !cfg.Postgres.Enabled && !cfg.MySQL.Enabled &&
		!cfg.TimescaleDB.Enabled && !cfg.ClickHouse.Enabled && !cfg.TDEngine.Enabled &&
		!cfg.MQTT.Enabled {
		errs = append(errs, "no sinks enabled; set Enabled: true for at least one sink")
	}

	validSources := map[string]bool{SourceHeat: true, SourceGrid: true, SourceSolar: true, SourceVentilation: true}
	if sourceFilter != "" && !validSources[sourceFilter] {
		errs = append(errs, fmt.Sprintf(
			"invalid --source value %q; valid values are heat, grid, solar, ventilation", sourceFilter,
		))
	}

	noSourceEnabled := !cfg.Heat.Enabled &&
		!cfg.Grid.Enabled &&
		!cfg.Enphase.Enabled &&
		!cfg.Ventilation.Enabled
	if noSourceEnabled && sourceFilter == "" {
		errs = append(
			errs,
			"no sources enabled in configuration; set Enabled: true for at least one source or use --source",
		)
	}

	errs = append(errs, sinkFieldErrors(cfg)...)
	errs = append(errs, sourceFieldErrors(cfg)...)

	return errs
}

// sqlSinkFields is a uniform view over the SQL-compatible sink configs
// (everything except QuestDB, which is validated separately since it has no
// Database field) used to drive table-driven field validation.
type sqlSinkFields struct {
	name     string
	enabled  bool
	host     string
	user     string
	database string
}

// sinkFieldErrors checks that every enabled SQL sink has the fields it needs
// to connect, failing fast with an actionable message instead of a confusing
// connection error later.
func sinkFieldErrors(cfg Config) []string {
	sinks := []sqlSinkFields{
		{SinkPostgres, cfg.Postgres.Enabled, cfg.Postgres.Host, cfg.Postgres.User, cfg.Postgres.Database},
		{SinkMySQL, cfg.MySQL.Enabled, cfg.MySQL.Host, cfg.MySQL.User, cfg.MySQL.Database},
		{
			SinkTimescaleDB,
			cfg.TimescaleDB.Enabled,
			cfg.TimescaleDB.Host,
			cfg.TimescaleDB.User,
			cfg.TimescaleDB.Database,
		},
		{SinkClickHouse, cfg.ClickHouse.Enabled, cfg.ClickHouse.Host, cfg.ClickHouse.User, cfg.ClickHouse.Database},
		{SinkTDEngine, cfg.TDEngine.Enabled, cfg.TDEngine.Host, cfg.TDEngine.User, cfg.TDEngine.Database},
	}

	var errs []string
	for _, s := range sinks {
		if !s.enabled {
			continue
		}
		if s.host == "" {
			errs = append(errs, fmt.Sprintf("%s sink enabled but Host is empty", s.name))
		}
		if s.user == "" {
			errs = append(errs, fmt.Sprintf("%s sink enabled but User is empty", s.name))
		}
		if s.database == "" {
			errs = append(errs, fmt.Sprintf("%s sink enabled but Database is empty", s.name))
		}
	}
	errs = append(errs, mqttFieldErrors(cfg.MQTT)...)
	return errs
}

// mqttFieldErrors checks the fields the MQTT sink needs to connect and
// publish. Only evaluated when the sink is enabled.
func mqttFieldErrors(cfg MQTTConfig) []string {
	if !cfg.Enabled {
		return nil
	}
	var errs []string
	if cfg.BrokerURL == "" {
		errs = append(errs, "mqtt sink enabled but BrokerURL is empty")
	}
	if cfg.QoS != 0 && cfg.QoS != 1 {
		errs = append(errs, fmt.Sprintf("invalid MQTT.QoS %d; valid values are 0 and 1", cfg.QoS))
	}
	return errs
}

// sourceFieldErrors checks that every enabled source has the fields it needs
// to read from its device or endpoint.
func sourceFieldErrors(cfg Config) []string {
	var errs []string

	if cfg.Heat.Enabled && cfg.Heat.SerialInterface == "" {
		errs = append(errs, "heat source enabled but SerialInterface is empty")
	}
	// An empty Reader is accepted as mbus so a Config built without Load
	// (tests, embedding) keeps the documented default.
	switch cfg.Heat.Reader {
	case "", HeatReaderMbus, HeatReaderOptical:
	case HeatReaderOptical401:
		errs = append(errs, optical401FieldErrors(cfg.Heat.Optical401)...)
	default:
		errs = append(errs, fmt.Sprintf(
			"invalid Heat.Reader %q; valid values are %s, %s, %s",
			cfg.Heat.Reader, HeatReaderMbus, HeatReaderOptical, HeatReaderOptical401,
		))
	}
	if cfg.Grid.Enabled && cfg.Grid.SerialInterface == "" {
		errs = append(errs, "grid source enabled but SerialInterface is empty")
	}
	// Gas rides on the grid source; it only flows when the grid source runs.
	// That dependency is not validated here because the --source filter can
	// still select grid at runtime regardless of Grid.Enabled.
	if cfg.Grid.Gas.Enabled && cfg.Grid.Gas.Measurement == "" {
		errs = append(errs, "grid gas readings enabled but Grid.Gas.Measurement is empty")
	}
	if cfg.Enphase.Enabled {
		errs = append(errs, enphaseFieldErrors(cfg.Enphase)...)
	}
	if cfg.Ventilation.Enabled && cfg.Ventilation.HostURL == "" {
		errs = append(errs, "ventilation source enabled but HostURL is empty")
	}

	return errs
}

// optical401FieldErrors checks the scaling settings for the optical401
// reader. An empty EnergyUnit is accepted as GJ so a Config built without
// Load (tests, embedding) keeps the documented default.
func optical401FieldErrors(cfg Optical401Config) []string {
	var errs []string
	switch cfg.EnergyUnit {
	case "", "GJ", "kWh", "MWh":
	default:
		errs = append(errs, fmt.Sprintf(
			"invalid Heat.Optical401.EnergyUnit %q; valid values are GJ, kWh, MWh", cfg.EnergyUnit,
		))
	}
	decimals := []struct {
		name  string
		value int
	}{
		{"EnergyDecimals", cfg.EnergyDecimals},
		{"VolumeDecimals", cfg.VolumeDecimals},
		{"PowerDecimals", cfg.PowerDecimals},
		{"FlowDecimals", cfg.FlowDecimals},
	}
	const maxDecimals = 4
	for _, d := range decimals {
		if d.value < 0 || d.value > maxDecimals {
			errs = append(errs, fmt.Sprintf(
				"invalid Heat.Optical401.%s %d; must be between 0 and %d", d.name, d.value, maxDecimals,
			))
		}
	}
	return errs
}

// enphaseFieldErrors checks the fields required to talk to an Enphase Envoy
// gateway. Called only when Enphase.Enabled is true.
func enphaseFieldErrors(cfg EnphaseConfig) []string {
	var errs []string
	if cfg.EnvoyURL == "" {
		errs = append(errs, "solar (enphase) source enabled but EnvoyURL is empty")
	}
	if cfg.User == "" {
		errs = append(errs, "solar (enphase) source enabled but User is empty")
	}
	if cfg.Password == "" {
		errs = append(errs, "solar (enphase) source enabled but Password is empty")
	}
	if cfg.Serial == "" {
		errs = append(errs, "solar (enphase) source enabled but Serial is empty")
	}
	return errs
}
