// Command meterlogger reads utility meter data from the configured sources
// and writes every reading to every enabled sink. This file is the CLI entry
// point; the assembly of the running process lives in runtime.go.
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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
	defer rt.shutdown()

	rt.serve(ctx)
}
