package main

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/ducobox"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // per-source constructor sets are parallel by design, distinct domain types
func buildVentilationSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.DucoRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, config.Ventilation.MeasurementBaseName,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.DucoRepository {
			return qdb.NewDucoQuestDBRepository(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.DucoRepository, error) {
			return sqlsink.NewDucoStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.DucoRepository, error) {
			return clickhouse.NewDucoStore(ctx, db, m, l)
		},
	)
}

func runVentilation(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	appMetrics *metrics.Metrics,
	dbs dbConnections,
) {
	conf := config.Ventilation
	sinks := buildVentilationSinks(ctx, l, healthSrv, dbs)
	if len(sinks) == 0 {
		l.WarnContext(ctx, "ventilation source enabled but no sinks available; skipping")
		return
	}
	repo := multisink.NewDucoRepository(sinks, l)
	defer func() {
		if err := repo.Close(); err != nil {
			l.ErrorContext(ctx, "ventilation repo close error", slog.Any("error", err))
		}
	}()

	startService(
		ctx, l, "Ventilation Meter", service.NewDucoLoggingService(
			ducobox.NewDucoReader(conf.HostURL, l),
			repo,
			conf.ScrapeInterval,
			config.FlushInterval,
			conf.Nodes,
			l,
		).WithMetrics(appMetrics),
	)
}
