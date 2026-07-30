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
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // sink builder functions are similar by design; each builds a distinct type
func buildHeatSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.HeatMeterRepository {
	var sinks []domain.HeatMeterRepository
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
		sinks = append(sinks, qdb.NewQuestDBHeatTelegramWriter(client, config.Heat.Measurement, l))
	}
	if dbs.postgres != nil {
		store, err := postgres.NewHeatStore(ctx, dbs.postgres, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "postgres heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.mysql != nil {
		store, err := mysql.NewHeatStore(ctx, dbs.mysql, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "mysql heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.timescaledb != nil {
		store, err := timescaledb.NewHeatStore(ctx, dbs.timescaledb, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "timescaledb heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.clickhouse != nil {
		store, err := clickhouse.NewHeatStore(ctx, dbs.clickhouse, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "clickhouse heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if dbs.tdengine != nil {
		store, err := tdengine.NewHeatStore(ctx, dbs.tdengine, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "tdengine heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	return sinks
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
