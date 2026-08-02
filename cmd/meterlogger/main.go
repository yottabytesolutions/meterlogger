// Command meterlogger reads utility meter data from the configured sources
// and writes every reading to every enabled sink. This file is the CLI entry
// point; the assembly of the running process lives in runtime.go.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/service"
	"github.com/yottabytesolutions/meterlogger/internal/tracedslog"
)

//nolint:gochecknoglobals // build info globals, set at link time
var (
	CommitSHA string
	BuildDate string
)

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var cfgFile string

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var sourceFilter string

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var cfg config.Config

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var logger *slog.Logger

//nolint:gochecknoinits // init() is required by the cobra CLI pattern
func init() {
	// Base handler - level will be adjusted in initConfig() once config is loaded.
	logger = newLogger(slog.LevelInfo)

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

// initConfig loads the configuration and rebuilds the logger at the configured
// level. Runs via cobra.OnInitialize before any command body.
func initConfig() {
	// Rebuild the logger now that every subcommand is registered, so
	// logWriter can resolve the invoked command.
	logger = newLogger(slog.LevelInfo)

	loaded, err := config.Load(cfgFile, logger)
	if err != nil {
		logger.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}
	cfg = loaded

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger = newLogger(level)
}

// logWriter returns stderr when the probe command is invoked, keeping stdout
// free for the probe's JSON reading. Every other command logs to stdout.
func logWriter() io.Writer {
	if cmd, _, err := rootCmd.Find(os.Args[1:]); err == nil && cmd.Name() == "probe" {
		return os.Stderr
	}
	return os.Stdout
}

// newLogger builds the process logger with trace correlation and build info.
func newLogger(level slog.Level) *slog.Logger {
	base := slog.NewTextHandler(logWriter(), &slog.HandlerOptions{Level: level})
	return slog.New(tracedslog.New(base)).With(
		slog.String("version", CommitSHA),
		slog.String("buildTime", BuildDate),
	)
}

// validateConfig logs every configuration problem and exits when any exist.
func validateConfig() {
	errs := config.Validate(cfg, sourceFilter)
	for _, msg := range errs {
		logger.Error(msg)
	}
	if len(errs) > 0 {
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

// buildVersion renders the link-time build info for --version and the startup
// log. Both values are empty in a plain `go build`.
func buildVersion() string {
	version := CommitSHA
	if version == "" {
		version = "dev"
	}
	if BuildDate != "" {
		version += " (built " + BuildDate + ")"
	}
	return version
}

func main() {
	rootCmd.Version = buildVersion()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() {
	ctx := interruptAwareContext()
	logger.Info("starting MeterLogger", slog.String("version", CommitSHA))

	rt := newRuntime(ctx)
	rt.serve(ctx)
	rt.shutdown()

	// A service that hit its consecutive-error threshold terminated the process
	// via SIGTERM, which drained through the normal shutdown path above. Exit
	// non-zero so the failure is visible to Kubernetes and alerting rather than
	// looking like a clean stop.
	if service.FatalOccurred() {
		logger.Error("exiting non-zero: a service terminated on an unrecoverable error")
		os.Exit(1)
	}
}
