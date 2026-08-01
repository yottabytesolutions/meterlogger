package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/viper"

	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

// Load reads configuration from file, environment variables, and any flags
// already bound to viper. When path is empty it looks for $HOME/.meterlogger.
// A missing config file is not an error; defaults, environment variables, and
// flags still apply. Load uses the global viper instance so cobra flag
// bindings made by the caller keep working.
func Load(path string, logger *slog.Logger) (Config, error) {
	viper.SetDefault("Debug", false)
	viper.SetDefault("HTTPServer.Port", 8080) //nolint:mnd // well-known default port
	viper.SetDefault("HTTPServer.LivenessFailureThreshold", healthserver.DefaultLivenessFailureThreshold)

	setSourceDefaults()
	setSinkDefaults()

	if path != "" {
		viper.SetConfigFile(path)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("get home directory: %w", err)
		}
		viper.AddConfigPath(home)
		viper.SetConfigName(".meterlogger")
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	if err := bindEnvKeys(); err != nil {
		return Config{}, fmt.Errorf("bind environment variables: %w", err)
	}

	if err := viper.ReadInConfig(); err != nil {
		if !isConfigFileNotFound(err) {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		logger.Debug("no config file found; using defaults, environment variables, and flags")
	} else {
		logger.Info("using config file", slog.String("file", viper.ConfigFileUsed()))
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

// setSinkDefaults sets viper defaults for sink fields documented as having a
// default value in documentation/configuration.md. QuestDB.Enabled is
// intentionally NOT defaulted to true here; see the breaking-change note in
// that document.
func setSourceDefaults() {
	// The reader polled M-Bus address 1 unconditionally before the address
	// became configurable; keep that as the default so existing configs
	// without the key keep working.
	viper.SetDefault("Heat.MbusAddress", 1)
	viper.SetDefault("Heat.Reader", HeatReaderMbus)

	// optical401 scaling defaults for common Dutch district heating installs;
	// see documentation/configuration.md.
	viper.SetDefault("Heat.Optical401.EnergyUnit", "GJ")
	viper.SetDefault("Heat.Optical401.EnergyDecimals", 3) //nolint:mnd // documented default: raw/1000 GJ
	viper.SetDefault("Heat.Optical401.VolumeDecimals", 3) //nolint:mnd // documented default: raw/1000 m3
	viper.SetDefault("Heat.Optical401.PowerDecimals", 1)
	viper.SetDefault("Heat.Optical401.FlowDecimals", 1)

	viper.SetDefault("Grid.Gas.Measurement", "gas_meter")

	// The fixed authentication key used by all Luxembourgish Smarty meters.
	// Austrian users override it with the GAK from their grid operator.
	// The fixed Luxembourg AK is a public spec constant.
	viper.SetDefault("Grid.AuthenticationKey", "00112233445566778899AABBCCDDEEFF") // gitleaks:allow
}

func setSinkDefaults() {
	viper.SetDefault("QuestDB.Port", 9009) //nolint:mnd // documented default ILP port

	viper.SetDefault("Postgres.Port", 5432) //nolint:mnd // documented default PostgreSQL port
	viper.SetDefault("Postgres.SSLMode", "disable")

	viper.SetDefault("MySQL.Port", 3306) //nolint:mnd // documented default MySQL port

	viper.SetDefault("TimescaleDB.Port", 5432) //nolint:mnd // documented default PostgreSQL port
	viper.SetDefault("TimescaleDB.SSLMode", "disable")

	viper.SetDefault("ClickHouse.Port", 9000) //nolint:mnd // documented default ClickHouse native port
	viper.SetDefault("ClickHouse.User", "default")

	viper.SetDefault("TDEngine.Port", 6041) //nolint:mnd // documented default TDEngine REST port
	viper.SetDefault("TDEngine.User", "root")
	viper.SetDefault("TDEngine.Password", "taosdata")
}

// isConfigFileNotFound reports whether err indicates that no config file was
// found at all, as opposed to a config file that exists but failed to parse
// (malformed YAML, permission denied, etc). The former is a normal fallback
// to defaults/env/flags; the latter is a footgun that must abort startup
// rather than silently ignore the user's broken config.
func isConfigFileNotFound(err error) bool {
	var notFoundErr viper.ConfigFileNotFoundError
	return errors.As(err, &notFoundErr)
}
