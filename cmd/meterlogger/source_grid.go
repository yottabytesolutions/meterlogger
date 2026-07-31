package main

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/stdout"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // one wiring table per source; parallel by design, distinct domain types
func buildGridSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.GridTelegramRepository {
	m := config.Grid.Measurement
	return buildSinks(ctx, l, []sinkInit[domain.GridTelegramRepository]{
		{sinkNameQuestDB, config.QuestDB.Enabled, func() (domain.GridTelegramRepository, error) {
			client, err := newQuestDBClient(ctx, l, healthSrv)
			if err != nil {
				return nil, err
			}
			return qdb.NewQuestDBGridWriter(client, m, l), nil
		}},
		{sinkNameStdout, config.Stdout.Enabled, func() (domain.GridTelegramRepository, error) {
			return stdout.NewStdoutStore(l), nil
		}},
		{sinkNamePostgres, dbs.postgres != nil, func() (domain.GridTelegramRepository, error) {
			return postgres.NewGridStore(ctx, dbs.postgres, m, l)
		}},
		{sinkNameMySQL, dbs.mysql != nil, func() (domain.GridTelegramRepository, error) {
			return mysql.NewGridStore(ctx, dbs.mysql, m, l)
		}},
		{sinkNameTimescaleDB, dbs.timescaledb != nil, func() (domain.GridTelegramRepository, error) {
			return timescaledb.NewGridStore(ctx, dbs.timescaledb, m, l)
		}},
		{sinkNameClickHouse, dbs.clickhouse != nil, func() (domain.GridTelegramRepository, error) {
			return clickhouse.NewGridStore(ctx, dbs.clickhouse, m, l)
		}},
		{sinkNameTDEngine, dbs.tdengine != nil, func() (domain.GridTelegramRepository, error) {
			return tdengine.NewGridStore(ctx, dbs.tdengine, m, l)
		}},
	})
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
			gridmeter.NewGridReader(config.Grid.SerialInterface, l),
			repo,
			config.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
