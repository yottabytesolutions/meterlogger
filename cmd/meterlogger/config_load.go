package main

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/viper"

	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/tracedslog"
)

// initConfig loads configuration from file, environment, and flags, then
// rebuilds the logger at the configured level. Runs via cobra.OnInitialize
// before any command body.
func initConfig() {
	viper.SetDefault("Debug", false)
	viper.SetDefault("HTTPServer.Port", 8080) //nolint:mnd // well-known default port
	viper.SetDefault("HTTPServer.LivenessFailureThreshold", healthserver.DefaultLivenessFailureThreshold)

	setSinkDefaults()

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			logger.Error("failed to get home directory", slog.Any("error", err))
			os.Exit(1)
		}
		viper.AddConfigPath(home)
		viper.SetConfigName(".meterlogger")
	}

	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
	if err := bindEnvKeys(); err != nil {
		logger.Error("failed to bind environment variables", slog.Any("error", err))
		os.Exit(1)
	}

	if err := viper.ReadInConfig(); err != nil {
		if isConfigFileNotFound(err) {
			logger.Debug("no config file found; using defaults, environment variables, and flags")
		} else {
			logger.Error("failed to read config file", slog.Any("error", err))
			os.Exit(1)
		}
	} else {
		logger.Info("using config file", slog.String("file", viper.ConfigFileUsed()))
	}

	if err := viper.Unmarshal(&config); err != nil {
		logger.Error("failed to unmarshal config", slog.Any("error", err))
		os.Exit(1)
	}

	// Adjust log level based on config.
	level := slog.LevelInfo
	if config.Debug {
		level = slog.LevelDebug
	}
	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	logger = slog.New(tracedslog.New(base)).With(
		slog.String("version", CommitSHA),
		slog.String("buildTime", BuildDate),
	)
}

// setSinkDefaults sets viper defaults for sink fields documented as having a
// default value in documentation/configuration.md. QuestDB.Enabled is
// intentionally NOT defaulted to true here; see the breaking-change note in
// that document.
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
