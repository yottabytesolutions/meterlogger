package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // per-source constructor sets are parallel by design, distinct domain types
func buildHeatSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.HeatMeterRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Heat.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.HeatMeterRepository {
			return qdb.NewQuestDBHeatTelegramWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.HeatMeterRepository, error) {
			return sqlsink.NewHeatStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.HeatMeterRepository, error) {
			return clickhouse.NewHeatStore(ctx, db, m, l)
		},
	)
}

func runHeatMeter(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	appMetrics *metrics.Metrics,
	dbs dbConnections,
) {
	sinks := buildHeatSinks(ctx, l, healthSrv, dbs)
	if len(sinks) == 0 {
		l.WarnContext(ctx, "heat source enabled but no sinks available; skipping")
		return
	}
	repo := multisink.NewHeatRepository(sinks, l)
	defer func() {
		if err := repo.Close(); err != nil {
			l.ErrorContext(ctx, "heat repo close error", slog.Any("error", err))
		}
	}()

	heatMbusAddress := byte(cfg.Heat.MbusAddress) //nolint:gosec // G115: intentional conversion of config value
	reader, err := serialmbus.NewReader(ctx, cfg.Heat.SerialInterface, heatMbusAddress, l)
	if err != nil {
		l.ErrorContext(ctx, "failed to create heat mbus reader", slog.Any("error", err))
		os.Exit(1)
	}

	startService(
		ctx, l, "Heat Meter", service.NewHeatMeterLoggingService(
			reader,
			repo,
			cfg.Heat.ScrapeInterval,
			cfg.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
