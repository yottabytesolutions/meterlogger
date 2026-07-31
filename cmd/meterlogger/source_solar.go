package main

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/enphase"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // per-source constructor sets are parallel by design, distinct domain types
func buildSolarSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.EnvoySolarRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Enphase.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.EnvoySolarRepository {
			return qdb.NewQuestDBSolarWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.EnvoySolarRepository, error) {
			return sqlsink.NewSolarStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.EnvoySolarRepository, error) {
			return clickhouse.NewSolarStore(ctx, db, m, l)
		},
	)
}

func runSolarMeter(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	appMetrics *metrics.Metrics,
	dbs dbConnections,
) {
	sinks := buildSolarSinks(ctx, l, healthSrv, dbs)
	if len(sinks) == 0 {
		l.WarnContext(ctx, "solar source enabled but no sinks available; skipping")
		return
	}
	repo := multisink.NewSolarRepository(sinks, l)
	defer func() {
		if err := repo.Close(); err != nil {
			l.ErrorContext(ctx, "solar repo close error", slog.Any("error", err))
		}
	}()

	startService(
		ctx, l, "Solar Meter", service.NewSolarLoggingService(
			enphase.NewEnvoyReader(
				enphase.Config{
					EnvoyURL: cfg.Enphase.EnvoyURL,
					User:     cfg.Enphase.User,
					Password: cfg.Enphase.Password,
					Serial:   cfg.Enphase.Serial,
				},
				l,
			),
			repo,
			cfg.Enphase.ScrapeInterval,
			cfg.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
