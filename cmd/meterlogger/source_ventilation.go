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
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/ducobox"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // one wiring table per source; parallel by design, distinct domain types
func buildVentilationSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.DucoRepository {
	m := config.Ventilation.MeasurementBaseName
	return buildSinks(ctx, l, []sinkInit[domain.DucoRepository]{
		{sinkNameQuestDB, config.QuestDB.Enabled, func() (domain.DucoRepository, error) {
			client, err := newQuestDBClient(ctx, l, healthSrv)
			if err != nil {
				return nil, err
			}
			return qdb.NewDucoQuestDBRepository(client, m, l), nil
		}},
		{sinkNameStdout, config.Stdout.Enabled, func() (domain.DucoRepository, error) {
			return stdout.NewStdoutStore(l), nil
		}},
		{sinkNamePostgres, dbs.postgres != nil, func() (domain.DucoRepository, error) {
			return postgres.NewDucoStore(ctx, dbs.postgres, m, l)
		}},
		{sinkNameMySQL, dbs.mysql != nil, func() (domain.DucoRepository, error) {
			return mysql.NewDucoStore(ctx, dbs.mysql, m, l)
		}},
		{sinkNameTimescaleDB, dbs.timescaledb != nil, func() (domain.DucoRepository, error) {
			return timescaledb.NewDucoStore(ctx, dbs.timescaledb, m, l)
		}},
		{sinkNameClickHouse, dbs.clickhouse != nil, func() (domain.DucoRepository, error) {
			return clickhouse.NewDucoStore(ctx, dbs.clickhouse, m, l)
		}},
		{sinkNameTDEngine, dbs.tdengine != nil, func() (domain.DucoRepository, error) {
			return tdengine.NewDucoStore(ctx, dbs.tdengine, m, l)
		}},
	})
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
