package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
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

	if err := viper.ReadInConfig(); err == nil {
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

	pgDB, myDB, tsDB, chDB, tdDB := initDBs(ctx)
	if pgDB != nil {
		defer closeDB("postgres", pgDB.Close)
	}
	if myDB != nil {
		defer closeDB("mysql", myDB.Close)
	}
	if tsDB != nil {
		defer closeDB("timescaledb", tsDB.Close)
	}
	if chDB != nil {
		defer closeDB("clickhouse", chDB.Close)
	}
	if tdDB != nil {
		defer closeDB("tdengine", tdDB.Close)
	}

	appMetrics := metrics.New()

	healthSrv := buildHealthServer(appMetrics, pgDB, myDB, tsDB, chDB, tdDB)
	addr, err := healthSrv.Start(ctx)
	if err != nil {
		logger.Error("failed to start health server", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("health server listening", slog.String("addr", addr))

	startSources(ctx, healthSrv, appMetrics, pgDB, myDB, tsDB, chDB, tdDB)

	// Block until the health server has finished its graceful shutdown so
	// in-flight readiness probes complete before deferred DB Close calls run.
	healthSrv.Wait()

	logger.Info("all services shut down")
}

func validateConfig() {
	if !config.QuestDB.Enabled && !config.Postgres.Enabled && !config.MySQL.Enabled &&
		!config.TimescaleDB.Enabled && !config.ClickHouse.Enabled && !config.TDEngine.Enabled {
		logger.Error("no sinks enabled; set Enabled: true for at least one sink")
		os.Exit(1)
	}

	validSources := map[string]bool{"heat": true, "grid": true, "solar": true, "ventilation": true}
	if sourceFilter != "" && !validSources[sourceFilter] {
		logger.Error(
			"invalid --source value",
			slog.String("source", sourceFilter),
			slog.String("valid", "heat, grid, solar, ventilation"),
		)
		os.Exit(1)
	}

	noSourceEnabled := !config.Heat.Enabled &&
		!config.Grid.Enabled &&
		!config.Enphase.Enabled &&
		!config.Ventilation.Enabled
	if noSourceEnabled && sourceFilter == "" {
		logger.Error("no sources enabled in configuration; set Enabled: true for at least one source or use --source")
		os.Exit(1)
	}
}

// buildHealthServer constructs the health server and registers every
// long-lived DB client that implements healthserver.Checker. The QuestDB
// client is created lazily by the source builders (one sender per source);
// each source registers its client with the server at creation time so the
// readiness probe reuses the existing TCP connection rather than opening a
// fresh one per probe.
func buildHealthServer(
	appMetrics *metrics.Metrics,
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) *healthserver.Server {
	srv := healthserver.New(fmt.Sprintf(":%d", config.HTTPServer.Port), logger, appMetrics.Registry)
	if pgDB != nil {
		srv.Register(pgDB)
	}
	if myDB != nil {
		srv.Register(myDB)
	}
	if tsDB != nil {
		srv.Register(tsDB)
	}
	if chDB != nil {
		srv.Register(chDB)
	}
	if tdDB != nil {
		srv.Register(tdDB)
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
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) {
	var wg sync.WaitGroup

	if sourceEnabled("heat") {
		wg.Go(func() { runHeatMeter(ctx, logger, healthSrv, appMetrics, pgDB, myDB, tsDB, chDB, tdDB) })
	}
	if sourceEnabled("grid") {
		wg.Go(func() { runGridMeter(ctx, logger, healthSrv, appMetrics, pgDB, myDB, tsDB, chDB, tdDB) })
	}
	if sourceEnabled("solar") {
		wg.Go(func() { runSolarMeter(ctx, logger, healthSrv, appMetrics, pgDB, myDB, tsDB, chDB, tdDB) })
	}
	if sourceEnabled("ventilation") {
		wg.Go(func() { runVentilation(ctx, logger, healthSrv, appMetrics, pgDB, myDB, tsDB, chDB, tdDB) })
	}

	wg.Wait()
}
