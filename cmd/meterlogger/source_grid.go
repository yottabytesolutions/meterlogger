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
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // sink builder functions are similar by design; each builds a distinct type
func buildGridSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.GridTelegramRepository {
	var sinks []domain.GridTelegramRepository
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
		sinks = append(sinks, qdb.NewQuestDBGridWriter(client, config.Grid.Measurement, l))
	}
	if dbs.postgres != nil {
		store, err := postgres.NewGridStore(ctx, dbs.postgres, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "postgres grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.mysql != nil {
		store, err := mysql.NewGridStore(ctx, dbs.mysql, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "mysql grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.timescaledb != nil {
		store, err := timescaledb.NewGridStore(ctx, dbs.timescaledb, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "timescaledb grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.clickhouse != nil {
		store, err := clickhouse.NewGridStore(ctx, dbs.clickhouse, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "clickhouse grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.tdengine != nil {
		store, err := tdengine.NewGridStore(ctx, dbs.tdengine, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "tdengine grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	return sinks
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

	resultChannel := make(chan domain.GridTelegram)
	startService(
		ctx, l, "Grid Meter", service.NewGridLoggingService(
			gridmeter.NewGridReader(config.Grid.SerialInterface, resultChannel, l),
			repo,
			config.FlushInterval,
			resultChannel,
			l,
		).WithMetrics(appMetrics),
	)
}
