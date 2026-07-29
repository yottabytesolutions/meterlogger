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
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) []domain.HeatMeterRepository {
	var sinks []domain.HeatMeterRepository
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
		sinks = append(sinks, qdb.NewQuestDBHeatTelegramWriter(client, config.Heat.Measurement, l))
	}
	if pgDB != nil {
		store, err := postgres.NewHeatStore(ctx, pgDB, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "postgres heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if myDB != nil {
		store, err := mysql.NewHeatStore(ctx, myDB, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "mysql heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if tsDB != nil {
		store, err := timescaledb.NewHeatStore(ctx, tsDB, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "timescaledb heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if chDB != nil {
		store, err := clickhouse.NewHeatStore(ctx, chDB, config.Heat.Measurement, l)
		if err != nil {
			l.ErrorContext(ctx, "clickhouse heat store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if tdDB != nil {
		store, err := tdengine.NewHeatStore(ctx, tdDB, config.Heat.Measurement, l)
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
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) {
	sinks := buildHeatSinks(ctx, l, healthSrv, pgDB, myDB, tsDB, chDB, tdDB)
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
