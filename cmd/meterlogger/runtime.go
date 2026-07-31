package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/telemetry"
)

// app is the composition root: every long-lived component of a running
// meterlogger process. newRuntime assembles it in dependency order, serve
// runs it until shutdown, and shutdown tears it down in reverse.
type app struct {
	dbs           dbConnections
	metrics       *metrics.Metrics
	health        *healthserver.Server
	stopTracing   func(context.Context) error
	stopProfiling func() error
}

// newRuntime assembles the process: telemetry first so later steps are
// traced, then config validation, sink connections, and the health server.
// Any failure exits; a partially assembled runtime never serves.
func newRuntime(ctx context.Context) *app {
	rt := &app{}
	rt.initTelemetry(ctx)
	validateConfig()
	rt.dbs = initDBs(ctx)
	rt.metrics = metrics.New()
	rt.health = buildHealthServer(rt.metrics, rt.dbs)
	return rt
}

func (rt *app) initTelemetry(ctx context.Context) {
	stopTracing, err := telemetry.InitTracing(ctx, cfg.OTEL)
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize OpenTelemetry", slog.Any("error", err))
		os.Exit(1)
	}
	rt.stopTracing = stopTracing

	stopProfiling, err := telemetry.InitProfiling(cfg.Profiling)
	if err != nil {
		logger.ErrorContext(ctx, "failed to initialize profiling", slog.Any("error", err))
		os.Exit(1)
	}
	rt.stopProfiling = stopProfiling
}

// serve starts the health server, runs every enabled source until the context
// is cancelled, then waits for the health server to drain in-flight probes so
// readiness checks complete before the DB connections close.
func (rt *app) serve(ctx context.Context) {
	addr, err := rt.health.Start(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to start health server", slog.Any("error", err))
		os.Exit(1)
	}
	logger.InfoContext(ctx, "health server listening", slog.String("addr", addr))

	rt.startSources(ctx)

	rt.health.Wait()
	logger.InfoContext(ctx, "all services shut down")
}

// startSources runs every enabled source concurrently and blocks until all
// of them have returned.
func (rt *app) startSources(ctx context.Context) {
	var wg sync.WaitGroup

	if sourceEnabled(config.SourceHeat) {
		wg.Go(func() { runHeatMeter(ctx, logger, rt.health, rt.metrics, rt.dbs) })
	}
	if sourceEnabled(config.SourceGrid) {
		wg.Go(func() { runGridMeter(ctx, logger, rt.health, rt.metrics, rt.dbs) })
	}
	if sourceEnabled(config.SourceSolar) {
		wg.Go(func() { runSolarMeter(ctx, logger, rt.health, rt.metrics, rt.dbs) })
	}
	if sourceEnabled(config.SourceVentilation) {
		wg.Go(func() { runVentilation(ctx, logger, rt.health, rt.metrics, rt.dbs) })
	}

	wg.Wait()
}

// shutdown releases everything newRuntime assembled, in reverse order: sink
// connections first, then profiling, then tracing.
func (rt *app) shutdown() {
	closeAll(rt.dbs.closers())
	if err := rt.stopProfiling(); err != nil {
		logger.Error("failed to stop profiling", slog.Any("error", err))
	}
	if err := rt.stopTracing(context.Background()); err != nil {
		logger.Error("failed to shutdown OpenTelemetry", slog.Any("error", err))
	}
}

// buildHealthServer constructs the health server and registers every
// long-lived DB client that implements healthserver.Checker. The QuestDB
// client is created lazily by the source builders (one sender per source);
// each source registers its client with the server at creation time so the
// readiness probe reuses the existing TCP connection rather than opening a
// fresh one per probe.
func buildHealthServer(appMetrics *metrics.Metrics, dbs dbConnections) *healthserver.Server {
	srv := healthserver.New(
		fmt.Sprintf(":%d", cfg.HTTPServer.Port),
		logger,
		appMetrics.Registry,
		cfg.HTTPServer.LivenessFailureThreshold,
	)
	for _, checker := range dbs.checkers() {
		srv.Register(checker)
	}
	return srv
}

// sourceEnabled reports whether a source should run, honoring the --source
// filter over the per-source Enabled flags.
func sourceEnabled(name string) bool {
	if sourceFilter != "" {
		return sourceFilter == name
	}
	switch name {
	case config.SourceHeat:
		return cfg.Heat.Enabled
	case config.SourceGrid:
		return cfg.Grid.Enabled
	case config.SourceSolar:
		return cfg.Enphase.Enabled
	case config.SourceVentilation:
		return cfg.Ventilation.Enabled
	}
	return false
}

// interruptAwareContext returns a context cancelled on SIGINT, SIGTERM, or
// SIGQUIT.
func interruptAwareContext() context.Context {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
	)
	go func() {
		<-ctx.Done()
		logger.Info("received interrupt signal")
		stop()
	}()

	return ctx
}
