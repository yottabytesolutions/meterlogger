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
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/enphase"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // one wiring table per source; parallel by design, distinct domain types
func buildSolarSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.EnvoySolarRepository {
	m := config.Enphase.Measurement
	return buildSinks(ctx, l, []sinkInit[domain.EnvoySolarRepository]{
		{sinkNameQuestDB, config.QuestDB.Enabled, func() (domain.EnvoySolarRepository, error) {
			client, err := newQuestDBClient(ctx, l, healthSrv)
			if err != nil {
				return nil, err
			}
			return qdb.NewQuestDBSolarWriter(client, m, l), nil
		}},
		{sinkNameStdout, config.Stdout.Enabled, func() (domain.EnvoySolarRepository, error) {
			return stdout.NewStdoutStore(l), nil
		}},
		{sinkNamePostgres, dbs.postgres != nil, func() (domain.EnvoySolarRepository, error) {
			return postgres.NewSolarStore(ctx, dbs.postgres, m, l)
		}},
		{sinkNameMySQL, dbs.mysql != nil, func() (domain.EnvoySolarRepository, error) {
			return mysql.NewSolarStore(ctx, dbs.mysql, m, l)
		}},
		{sinkNameTimescaleDB, dbs.timescaledb != nil, func() (domain.EnvoySolarRepository, error) {
			return timescaledb.NewSolarStore(ctx, dbs.timescaledb, m, l)
		}},
		{sinkNameClickHouse, dbs.clickhouse != nil, func() (domain.EnvoySolarRepository, error) {
			return clickhouse.NewSolarStore(ctx, dbs.clickhouse, m, l)
		}},
		{sinkNameTDEngine, dbs.tdengine != nil, func() (domain.EnvoySolarRepository, error) {
			return tdengine.NewSolarStore(ctx, dbs.tdengine, m, l)
		}},
	})
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
					EnvoyURL: config.Enphase.EnvoyURL,
					User:     config.Enphase.User,
					Password: config.Enphase.Password,
					Serial:   config.Enphase.Serial,
				},
				l,
			),
			repo,
			config.Enphase.ScrapeInterval,
			config.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
