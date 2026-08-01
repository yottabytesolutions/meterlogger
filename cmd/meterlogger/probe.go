package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/ducobox"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/enphase"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus"
	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const defaultProbeTimeout = 30 * time.Second

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var (
	probeSource  string
	probeTimeout time.Duration
)

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var probeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Take one reading from a source and print it as JSON",
	Long: "Take a single reading from one source for commissioning and hardware validation. " +
		"The reading is printed as indented JSON on stdout; diagnostics go to stderr. " +
		"No sink is required. Exit 0 on success, 1 on failure.",
	Run: func(cmd *cobra.Command, _ []string) {
		// The global logger writes to stderr for this command (see
		// logWriter in main.go), so stdout carries only the JSON reading.
		os.Exit(runProbe(cmd.Context(), probeSource, probeTimeout, os.Stdout, logger))
	},
}

//nolint:gochecknoinits // init() is required by the cobra CLI pattern
func init() {
	probeCmd.Flags().StringVarP(
		&probeSource,
		"source",
		"s",
		"",
		"Source to probe (heat, grid, solar, ventilation)",
	)
	probeCmd.Flags().DurationVar(
		&probeTimeout,
		"timeout",
		defaultProbeTimeout,
		"Bound on the whole probe, including connect and read",
	)
	if err := probeCmd.MarkFlagRequired("source"); err != nil {
		fmt.Fprintln(os.Stderr, "failed to mark --source required:", err)
		os.Exit(1)
	}
	rootCmd.AddCommand(probeCmd)
}

// probeFunc takes one reading from a source and returns it as indented JSON.
type probeFunc func(ctx context.Context, l *slog.Logger) (json.RawMessage, error)

// probeFuncFor maps a --source value to its probe implementation.
func probeFuncFor(source string) (probeFunc, error) {
	switch source {
	case config.SourceHeat:
		return probeHeat, nil
	case config.SourceGrid:
		return probeGrid, nil
	case config.SourceSolar:
		return probeSolar, nil
	case config.SourceVentilation:
		return probeVentilation, nil
	}
	return nil, fmt.Errorf(
		"invalid --source value %q; valid values are heat, grid, solar, ventilation", source,
	)
}

// runProbe takes one reading from the selected source and writes it to out.
// It returns the process exit code.
func runProbe(ctx context.Context, source string, timeout time.Duration, out io.Writer, l *slog.Logger) int {
	fn, err := probeFuncFor(source)
	if err != nil {
		l.ErrorContext(ctx, err.Error())
		return 1
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	l.InfoContext(ctx, "probing source", slog.String("source", source), slog.Duration("timeout", timeout))
	reading, err := fn(ctx, l)
	if err != nil {
		l.ErrorContext(ctx, "probe failed", slog.String("source", source), slog.Any("error", err))
		return 1
	}
	fmt.Fprintln(out, string(reading))
	return 0
}

// marshalReading renders one reading as indented JSON.
func marshalReading[T any](v T) (json.RawMessage, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal reading: %w", err)
	}
	return data, nil
}

// probeHeat opens the MBus serial port and takes one heat telegram.
func probeHeat(ctx context.Context, l *slog.Logger) (json.RawMessage, error) {
	if cfg.Heat.SerialInterface == "" {
		return nil, errors.New("heat source not configured: Heat.SerialInterface is empty")
	}
	addr := byte(cfg.Heat.MbusAddress) //nolint:gosec // G115: intentional conversion of config value
	reader, err := serialmbus.NewReader(ctx, cfg.Heat.SerialInterface, addr, l)
	if err != nil {
		return nil, fmt.Errorf("open heat mbus reader: %w", err)
	}
	telegram, err := reader.ReadHeatTelegram(ctx)
	if err != nil {
		return nil, fmt.Errorf("read heat telegram: %w", err)
	}
	return marshalReading(telegram)
}

// probeGrid opens the P1 serial port and takes one grid telegram.
func probeGrid(ctx context.Context, l *slog.Logger) (json.RawMessage, error) {
	if cfg.Grid.SerialInterface == "" {
		return nil, errors.New("grid source not configured: Grid.SerialInterface is empty")
	}
	telegram, err := readOneGridTelegram(ctx, gridmeter.NewGridReader(cfg.Grid.SerialInterface, l))
	if err != nil {
		return nil, err
	}
	return marshalReading(telegram)
}

// readOneGridTelegram runs the streaming grid reader until the first telegram
// arrives, then cancels the reader and waits for it to finish. The reader
// closes its telegram channel when ReadGridTelegrams returns, so a closed
// channel means the reader stopped before delivering anything.
func readOneGridTelegram(ctx context.Context, reader domain.GridTelegramReader) (domain.GridTelegram, error) {
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	readErr := make(chan error, 1)
	go func() { readErr <- reader.ReadGridTelegrams(readCtx) }()

	telegram, ok := <-reader.Telegrams()
	cancel()
	err := <-readErr
	if ok {
		return telegram, nil
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = errors.New("reader stopped before delivering a telegram")
	}
	return domain.GridTelegram{}, fmt.Errorf("read grid telegram: %w", err)
}

// probeSolar takes one production reading from the Enphase Envoy gateway.
func probeSolar(ctx context.Context, l *slog.Logger) (json.RawMessage, error) {
	if missing := missingEnphaseFields(cfg.Enphase); len(missing) > 0 {
		return nil, fmt.Errorf(
			"solar source not configured: Enphase %s empty", strings.Join(missing, ", "),
		)
	}
	reader := enphase.NewEnvoyReader(enphase.Config{
		EnvoyURL: cfg.Enphase.EnvoyURL,
		User:     cfg.Enphase.User,
		Password: cfg.Enphase.Password,
		Serial:   cfg.Enphase.Serial,
	}, l)
	data, err := reader.ReadEnvoySolarData(ctx)
	if err != nil {
		return nil, fmt.Errorf("read envoy solar data: %w", err)
	}
	return marshalReading(data)
}

// missingEnphaseFields lists the Enphase config fields required for a probe
// that are empty.
func missingEnphaseFields(c config.EnphaseConfig) []string {
	var missing []string
	if c.EnvoyURL == "" {
		missing = append(missing, "EnvoyURL")
	}
	if c.User == "" {
		missing = append(missing, "User")
	}
	if c.Password == "" {
		missing = append(missing, "Password")
	}
	if c.Serial == "" {
		missing = append(missing, "Serial")
	}
	return missing
}

// ventilationProbeResult combines the DucoBox status with, when nodes are
// configured, the status of the first configured node.
type ventilationProbeResult struct {
	Box  domain.DucoBoxStatus
	Node domain.DucoNodeStatus `json:",omitempty"`
}

// probeVentilation reads the DucoBox status and, if any nodes are configured,
// the first node's status.
func probeVentilation(ctx context.Context, l *slog.Logger) (json.RawMessage, error) {
	if cfg.Ventilation.HostURL == "" {
		return nil, errors.New("ventilation source not configured: Ventilation.HostURL is empty")
	}
	reader := ducobox.NewDucoReader(cfg.Ventilation.HostURL, l)
	box, err := reader.ReadBoxStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("read box status: %w", err)
	}
	result := ventilationProbeResult{Box: box}
	if len(cfg.Ventilation.Nodes) > 0 {
		nodeID := cfg.Ventilation.Nodes[0]
		node, nodeErr := reader.ReadNodeStatus(ctx, nodeID)
		if nodeErr != nil {
			return nil, fmt.Errorf("read node %d status: %w", nodeID, nodeErr)
		}
		result.Node = node
	}
	return marshalReading(result)
}
