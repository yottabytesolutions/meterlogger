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
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/ducobox"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // sink builder functions are similar by design; each builds a distinct type
func buildVentilationSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) []domain.DucoRepository {
	var sinks []domain.DucoRepository
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
		sinks = append(sinks, qdb.NewDucoQuestDBRepository(client, config.Ventilation.MeasurementBaseName, l))
	}
	if pgDB != nil {
		store, err := postgres.NewDucoStore(ctx, pgDB, config.Ventilation.MeasurementBaseName, l)
		if err != nil {
			l.ErrorContext(ctx, "postgres duco store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if myDB != nil {
		store, err := mysql.NewDucoStore(ctx, myDB, config.Ventilation.MeasurementBaseName, l)
		if err != nil {
			l.ErrorContext(ctx, "mysql duco store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if tsDB != nil {
		store, err := timescaledb.NewDucoStore(ctx, tsDB, config.Ventilation.MeasurementBaseName, l)
		if err != nil {
			l.ErrorContext(ctx, "timescaledb duco store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if chDB != nil {
		store, err := clickhouse.NewDucoStore(ctx, chDB, config.Ventilation.MeasurementBaseName, l)
		if err != nil {
			l.ErrorContext(ctx, "clickhouse duco store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	if tdDB != nil {
		store, err := tdengine.NewDucoStore(ctx, tdDB, config.Ventilation.MeasurementBaseName, l)
		if err != nil {
			l.ErrorContext(ctx, "tdengine duco store init failed", slog.Any("error", err))
			os.Exit(1)
		}
		sinks = append(sinks, store)
	}
	return sinks
}

func runVentilation(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	appMetrics *metrics.Metrics,
	pgDB *postgres.DB, myDB *mysql.DB,
	tsDB *timescaledb.DB, chDB *clickhouse.DB, tdDB *tdengine.DB,
) {
	conf := config.Ventilation
	sinks := buildVentilationSinks(ctx, l, healthSrv, pgDB, myDB, tsDB, chDB, tdDB)
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
