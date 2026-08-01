package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/sml"
	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

// newGridReader constructs the grid meter reader selected by
// cfg.Grid.Reader. Shared by runGridMeter and the grid probe so both
// exercise the same wiring.
//
//nolint:ireturn // factory selects between reader implementations behind the domain port
func newGridReader(l *slog.Logger) (domain.GridTelegramReader, error) {
	switch cfg.Grid.Reader {
	case config.GridReaderSML:
		return sml.NewReader(cfg.Grid.SerialInterface, l), nil
	case "", config.GridReaderDSMR:
		return gridmeter.NewGridReader(cfg.Grid.SerialInterface, l), nil
	}
	return nil, fmt.Errorf("invalid Grid.Reader %q; valid values are dsmr, sml", cfg.Grid.Reader)
}

//nolint:dupl // per-source constructor sets are parallel by design, distinct domain types
func buildGridSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.GridTelegramRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Grid.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.GridTelegramRepository {
			return qdb.NewQuestDBGridWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.GridTelegramRepository, error) {
			return sqlsink.NewGridStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.GridTelegramRepository, error) {
			return clickhouse.NewGridStore(ctx, db, m, l)
		},
	)
}

func buildGasSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.GasRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Grid.Gas.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.GasRepository {
			return qdb.NewQuestDBGasWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.GasRepository, error) {
			return sqlsink.NewGasStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.GasRepository, error) {
			return clickhouse.NewGasStore(ctx, db, m, l)
		},
	)
}

func runGridMeter(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	appMetrics *metrics.Metrics,
	dbs dbConnections,
) {
	sinks := buildGridSinks(ctx, l, healthSrv, dbs)
	if len(sinks) == 0 {
		l.WarnContext(ctx, "grid source enabled but no sinks available; skipping")
		return
	}
	repo := multisink.NewGridRepository(sinks, l)
	defer func() {
		if err := repo.Close(); err != nil {
			l.ErrorContext(ctx, "grid repo close error", slog.Any("error", err))
		}
	}()

	reader, err := newGridReader(l)
	if err != nil {
		l.ErrorContext(ctx, "failed to create grid reader", slog.Any("error", err))
		os.Exit(1)
	}

	svc := service.NewGridLoggingService(
		reader,
		repo,
		cfg.FlushInterval,
		l,
	)

	if cfg.Grid.Gas.Enabled {
		gasSinks := buildGasSinks(ctx, l, healthSrv, dbs)
		gasRepo := multisink.NewGasRepository(gasSinks, l)
		defer func() {
			if closeErr := gasRepo.Close(); closeErr != nil {
				l.ErrorContext(ctx, "gas repo close error", slog.Any("error", closeErr))
			}
		}()
		svc = svc.WithGas(gasRepo)
	}

	startService(ctx, l, "Grid Meter", svc.WithMetrics(appMetrics))
}
