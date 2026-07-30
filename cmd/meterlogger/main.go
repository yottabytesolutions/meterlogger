package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	_ "time/tzdata" // embed IANA timezone data so the image needs no /usr/share/zoneinfo

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/tracedslog"
)

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var cfgFile string

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var sourceFilter string

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var config Config

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var logger *slog.Logger

//nolint:gochecknoinits // init() is required by the cobra CLI pattern
func init() {
	// Base handler - level will be adjusted in initConfig() once config is loaded.
	base := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger = slog.New(tracedslog.New(base)).With(
		slog.String("version", CommitSHA),
		slog.String("buildTime", BuildDate),
	)

	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(
		&cfgFile,
		"config",
		"",
		"config file (default is $HOME/.meterlogger.yaml)",
	)
	rootCmd.Flags().StringP(
		"envoy-url",
		"e",
		"",
		"Envoy URL",
	)

	rootCmd.Flags().StringVarP(
		&sourceFilter,
		"source",
		"s",
		"",
		"Run only this source, ignoring Enabled flags in config (heat, grid, solar, ventilation)",
	)

	rootCmd.Flags().BoolP(
		"debug",
		"d",
		false,
		"Enable debug logging",
	)

	if err := viper.BindPFlag("Debug", rootCmd.Flags().Lookup("debug")); err != nil {
		fmt.Fprintln(os.Stderr, "failed to bind debug flag:", err)
		os.Exit(1)
	}
	if err := viper.BindPFlag("Enphase.EnvoyURL", rootCmd.Flags().Lookup("envoy-url")); err != nil {
		fmt.Fprintln(os.Stderr, "failed to bind envoy-url flag:", err)
		os.Exit(1)
	}
}

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var rootCmd = &cobra.Command{
	Use:   "meterlogger",
	Short: "MeterLogger is a tool to log utility meter data",
	Run: func(_ *cobra.Command, _ []string) {
		run()
	},
}

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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() {
	ctx := interruptAwareContext()

	logger.Info("starting MeterLogger", slog.String("version", CommitSHA))

	shutdown, err := initOTEL(ctx, config.OTEL)
	if err != nil {
		logger.Error("failed to initialize OpenTelemetry", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if shutdownErr := shutdown(context.Background()); shutdownErr != nil {
			logger.Error("failed to shutdown OpenTelemetry", slog.Any("error", shutdownErr))
		}
	}()

	stopProfiling, err := initProfiling(config.Profiling)
	if err != nil {
		logger.Error("failed to initialize profiling", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if profilingErr := stopProfiling(); profilingErr != nil {
			logger.Error("failed to stop profiling", slog.Any("error", profilingErr))
		}
	}()

	validateConfig()

	dbs := initDBs(ctx)
	if dbs.postgres != nil {
		defer closeDB(sinkNamePostgres, dbs.postgres.Close)
	}
	if dbs.mysql != nil {
		defer closeDB(sinkNameMySQL, dbs.mysql.Close)
	}
	if dbs.timescaledb != nil {
		defer closeDB(sinkNameTimescaleDB, dbs.timescaledb.Close)
	}
	if dbs.clickhouse != nil {
		defer closeDB(sinkNameClickHouse, dbs.clickhouse.Close)
	}
	if dbs.tdengine != nil {
		defer closeDB(sinkNameTDEngine, dbs.tdengine.Close)
	}

	appMetrics := metrics.New()

	healthSrv := buildHealthServer(appMetrics, dbs)
	addr, err := healthSrv.Start(ctx)
	if err != nil {
		logger.Error("failed to start health server", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("health server listening", slog.String("addr", addr))

	startSources(ctx, healthSrv, appMetrics, dbs)

	// Block until the health server has finished its graceful shutdown so
	// in-flight readiness probes complete before deferred DB Close calls run.
	healthSrv.Wait()

	logger.Info("all services shut down")
}

func validateConfig() {
	errs := configValidationErrors(config, sourceFilter)
	for _, msg := range errs {
		logger.Error(msg)
	}
	if len(errs) > 0 {
		os.Exit(1)
	}
}

// configValidationErrors returns every configuration problem found in cfg,
// given the --source filter in effect. An empty slice means the
// configuration is valid. Split out from validateConfig so the checks can be
// exercised without triggering os.Exit.
func configValidationErrors(cfg Config, sourceFilter string) []string {
	var errs []string

	if !cfg.QuestDB.Enabled && !cfg.Postgres.Enabled && !cfg.MySQL.Enabled &&
		!cfg.TimescaleDB.Enabled && !cfg.ClickHouse.Enabled && !cfg.TDEngine.Enabled {
		errs = append(errs, "no sinks enabled; set Enabled: true for at least one sink")
	}

	//nolint:goconst // source names are self-documenting here; a shared constant adds indirection, not clarity
	validSources := map[string]bool{"heat": true, "grid": true, "solar": true, "ventilation": true}
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
		errs = append(errs, "no sources enabled in configuration; set Enabled: true for at least one source or use --source")
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
		{sinkNamePostgres, cfg.Postgres.Enabled, cfg.Postgres.Host, cfg.Postgres.User, cfg.Postgres.Database},
		{sinkNameMySQL, cfg.MySQL.Enabled, cfg.MySQL.Host, cfg.MySQL.User, cfg.MySQL.Database},
		{sinkNameTimescaleDB, cfg.TimescaleDB.Enabled, cfg.TimescaleDB.Host, cfg.TimescaleDB.User, cfg.TimescaleDB.Database},
		{sinkNameClickHouse, cfg.ClickHouse.Enabled, cfg.ClickHouse.Host, cfg.ClickHouse.User, cfg.ClickHouse.Database},
		{sinkNameTDEngine, cfg.TDEngine.Enabled, cfg.TDEngine.Host, cfg.TDEngine.User, cfg.TDEngine.Database},
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
	return errs
}

// sourceFieldErrors checks that every enabled source has the fields it needs
// to read from its device or endpoint.
func sourceFieldErrors(cfg Config) []string {
	var errs []string

	if cfg.Heat.Enabled && cfg.Heat.SerialInterface == "" {
		errs = append(errs, "heat source enabled but SerialInterface is empty")
	}
	if cfg.Grid.Enabled && cfg.Grid.SerialInterface == "" {
		errs = append(errs, "grid source enabled but SerialInterface is empty")
	}
	if cfg.Enphase.Enabled {
		errs = append(errs, enphaseFieldErrors(cfg.Enphase)...)
	}
	if cfg.Ventilation.Enabled && cfg.Ventilation.HostURL == "" {
		errs = append(errs, "ventilation source enabled but HostURL is empty")
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

// buildHealthServer constructs the health server and registers every
// long-lived DB client that implements healthserver.Checker. The QuestDB
// client is created lazily by the source builders (one sender per source);
// each source registers its client with the server at creation time so the
// readiness probe reuses the existing TCP connection rather than opening a
// fresh one per probe.
func buildHealthServer(appMetrics *metrics.Metrics, dbs dbConnections) *healthserver.Server {
	srv := healthserver.New(
		fmt.Sprintf(":%d", config.HTTPServer.Port),
		logger,
		appMetrics.Registry,
		config.HTTPServer.LivenessFailureThreshold,
	)
	if dbs.postgres != nil {
		srv.Register(dbs.postgres)
	}
	if dbs.mysql != nil {
		srv.Register(dbs.mysql)
	}
	if dbs.timescaledb != nil {
		srv.Register(dbs.timescaledb)
	}
	if dbs.clickhouse != nil {
		srv.Register(dbs.clickhouse)
	}
	if dbs.tdengine != nil {
		srv.Register(dbs.tdengine)
	}
	return srv
}

func sourceEnabled(name string) bool {
	if sourceFilter != "" {
		return sourceFilter == name
	}
	switch name {
	case "heat":
		return config.Heat.Enabled
	case "grid":
		return config.Grid.Enabled
	case "solar":
		return config.Enphase.Enabled
	case "ventilation":
		return config.Ventilation.Enabled
	}
	return false
}

func startSources(
	ctx context.Context,
	healthSrv *healthserver.Server,
	appMetrics *metrics.Metrics,
	dbs dbConnections,
) {
	var wg sync.WaitGroup

	if sourceEnabled("heat") {
		wg.Go(func() { runHeatMeter(ctx, logger, healthSrv, appMetrics, dbs) })
	}
	if sourceEnabled("grid") {
		wg.Go(func() { runGridMeter(ctx, logger, healthSrv, appMetrics, dbs) })
	}
	if sourceEnabled("solar") {
		wg.Go(func() { runSolarMeter(ctx, logger, healthSrv, appMetrics, dbs) })
	}
	if sourceEnabled("ventilation") {
		wg.Go(func() { runVentilation(ctx, logger, healthSrv, appMetrics, dbs) })
	}

	wg.Wait()
}
