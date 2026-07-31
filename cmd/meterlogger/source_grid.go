package main

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

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

	startService(
		ctx, l, "Grid Meter", service.NewGridLoggingService(
			gridmeter.NewGridReader(cfg.Grid.SerialInterface, l),
			repo,
			cfg.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
