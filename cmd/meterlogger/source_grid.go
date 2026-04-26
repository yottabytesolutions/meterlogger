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
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) []domain.GridTelegramRepository {
	var sinks []domain.GridTelegramRepository
	if config.QuestDB.Enabled {
		client, err := qdb.NewDBClient(
			ctx, config.QuestDB.Host, config.QuestDB.Port,
			config.QuestDB.User, config.QuestDB.Password, l,
		)
		if err != nil {
			l.ErrorContext(ctx, "failed to create QuestDB client", slog.Any("error", err))
			os.Exit(1)
		}
		if healthSrv != nil {
			healthSrv.Register(client)
		}
		sinks = append(sinks, qdb.NewQuestDBGridWriter(client, config.Grid.Measurement, l))
	}
	if pgDB != nil {
		store, err := postgres.NewGridStore(ctx, pgDB, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "postgres grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if myDB != nil {
		store, err := mysql.NewGridStore(ctx, myDB, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "mysql grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if tsDB != nil {
		store, err := timescaledb.NewGridStore(ctx, tsDB, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "timescaledb grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if chDB != nil {
		store, err := clickhouse.NewGridStore(ctx, chDB, config.Grid.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "clickhouse grid store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if tdDB != nil {
		store, err := tdengine.NewGridStore(ctx, tdDB, config.Grid.Measurement, l)
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
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) {
	sinks := buildGridSinks(ctx, l, healthSrv, pgDB, myDB, tsDB, chDB, tdDB)
	if len(sinks) == 0 {
		l.WarnContext(ctx, "grid source enabled but no sinks available; skipping")
		return
	}
	repo := multisink.NewGridRepository(sinks, l)
	defer func() {
		if err := repo.Close(); err != nil {
			l.Error("grid repo close error", slog.Any("error", err))
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
