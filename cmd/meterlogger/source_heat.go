package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/stdout"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // one wiring table per source; parallel by design, distinct domain types
func buildHeatSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.HeatMeterRepository {
	m := config.Heat.Measurement
	return buildSinks(ctx, l, []sinkInit[domain.HeatMeterRepository]{
		{sinkNameQuestDB, config.QuestDB.Enabled, func() (domain.HeatMeterRepository, error) {
			client, err := newQuestDBClient(ctx, l, healthSrv)
			if err != nil {
				return nil, err
			}
			return qdb.NewQuestDBHeatTelegramWriter(client, m, l), nil
		}},
		{sinkNameStdout, config.Stdout.Enabled, func() (domain.HeatMeterRepository, error) {
			return stdout.NewStdoutStore(l), nil
		}},
		{sinkNamePostgres, dbs.postgres != nil, func() (domain.HeatMeterRepository, error) {
			return postgres.NewHeatStore(ctx, dbs.postgres, m, l)
		}},
		{sinkNameMySQL, dbs.mysql != nil, func() (domain.HeatMeterRepository, error) {
			return mysql.NewHeatStore(ctx, dbs.mysql, m, l)
		}},
		{sinkNameTimescaleDB, dbs.timescaledb != nil, func() (domain.HeatMeterRepository, error) {
			return timescaledb.NewHeatStore(ctx, dbs.timescaledb, m, l)
		}},
		{sinkNameClickHouse, dbs.clickhouse != nil, func() (domain.HeatMeterRepository, error) {
			return clickhouse.NewHeatStore(ctx, dbs.clickhouse, m, l)
		}},
		{sinkNameTDEngine, dbs.tdengine != nil, func() (domain.HeatMeterRepository, error) {
			return tdengine.NewHeatStore(ctx, dbs.tdengine, m, l)
		}},
	})
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

	heatMbusAddress := byte(config.Heat.MbusAddress) //nolint:gosec // G115: intentional conversion of config value
	reader, err := serialmbus.NewReader(ctx, config.Heat.SerialInterface, heatMbusAddress, l)
	if err != nil {
		l.ErrorContext(ctx, "failed to create heat mbus reader", slog.Any("error", err))
		os.Exit(1)
	}

	startService(
		ctx, l, "Heat Meter", service.NewHeatMeterLoggingService(
			reader,
			repo,
			config.Heat.ScrapeInterval,
			config.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
