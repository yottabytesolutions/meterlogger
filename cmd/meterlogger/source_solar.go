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
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/enphase"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // sink builder functions are similar by design; each builds a distinct type
func buildSolarSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.EnvoySolarRepository {
	var sinks []domain.EnvoySolarRepository
	if config.QuestDB.Enabled {
		client, err := qdb.NewDBClient(ctx, qdb.Config{
			Host:     config.QuestDB.Host,
			Port:     config.QuestDB.Port,
			User:     config.QuestDB.User,
			Password: config.QuestDB.Password,
		}, l)
		if err != nil {
			l.ErrorContext(ctx, "failed to create QuestDB client", slog.Any("error", err))
			os.Exit(1)
		}
		if healthSrv != nil {
			healthSrv.Register(client)
		}
		sinks = append(sinks, qdb.NewQuestDBSolarWriter(client, config.Enphase.Measurement, l))
	}
	if dbs.postgres != nil {
		store, err := postgres.NewSolarStore(ctx, dbs.postgres, config.Enphase.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "postgres solar store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.mysql != nil {
		store, err := mysql.NewSolarStore(ctx, dbs.mysql, config.Enphase.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "mysql solar store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.timescaledb != nil {
		store, err := timescaledb.NewSolarStore(ctx, dbs.timescaledb, config.Enphase.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "timescaledb solar store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.clickhouse != nil {
		store, err := clickhouse.NewSolarStore(ctx, dbs.clickhouse, config.Enphase.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "clickhouse solar store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.tdengine != nil {
		store, err := tdengine.NewSolarStore(ctx, dbs.tdengine, config.Enphase.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "tdengine solar store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	return sinks
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
