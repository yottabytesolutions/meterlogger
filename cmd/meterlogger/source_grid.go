package main

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

//nolint:dupl // per-source constructor sets are parallel by design, distinct domain types
func buildGridSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.GridTelegramRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Grid.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.GridTelegramRepository {
			return qdb.NewQuestDBGridWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.GridTelegramRepository, error) {
			return sqlsink.NewGridStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.GridTelegramRepository, error) {
			return clickhouse.NewGridStore(ctx, db, m, l)
		},
	)
}

//nolint:dupl // per-subdevice constructor sets are parallel by design, distinct domain types
func buildGasSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.GasRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Grid.Gas.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.GasRepository {
			return qdb.NewQuestDBGasWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.GasRepository, error) {
			return sqlsink.NewGasStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.GasRepository, error) {
			return clickhouse.NewGasStore(ctx, db, m, l)
		},
	)
}

//nolint:dupl // per-subdevice constructor sets are parallel by design, distinct domain types
func buildWaterSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.WaterRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Grid.Water.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.WaterRepository {
			return qdb.NewQuestDBWaterWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.WaterRepository, error) {
			return sqlsink.NewWaterStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.WaterRepository, error) {
			return clickhouse.NewWaterStore(ctx, db, m, l)
		},
	)
}

//nolint:dupl // per-subdevice constructor sets are parallel by design, distinct domain types
func buildThermalSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.ThermalRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Grid.Thermal.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.ThermalRepository {
			return qdb.NewQuestDBThermalWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.ThermalRepository, error) {
			return sqlsink.NewThermalStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.ThermalRepository, error) {
			return clickhouse.NewThermalStore(ctx, db, m, l)
		},
	)
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

	reader := gridmeter.NewGridReader(cfg.Grid.SerialInterface, l)
	if cfg.Grid.DecryptionKey != "" {
		var keyErr error
		reader, keyErr = reader.WithDecryption(cfg.Grid.DecryptionKey, cfg.Grid.AuthenticationKey)
		if keyErr != nil {
			l.ErrorContext(ctx, "invalid grid decryption configuration", slog.Any("error", keyErr))
			return
		}
	}

	svc := service.NewGridLoggingService(
		reader,
		repo,
		cfg.FlushInterval,
		l,
	)

	if cfg.Grid.Gas.Enabled {
		gasSinks := buildGasSinks(ctx, l, healthSrv, dbs)
		gasRepo := multisink.NewGasRepository(gasSinks, l)
		defer func() {
			if err := gasRepo.Close(); err != nil {
				l.ErrorContext(ctx, "gas repo close error", slog.Any("error", err))
			}
		}()
		svc = svc.WithGas(gasRepo)
	}

	if cfg.Grid.Water.Enabled {
		waterSinks := buildWaterSinks(ctx, l, healthSrv, dbs)
		waterRepo := multisink.NewWaterRepository(waterSinks, l)
		defer func() {
			if err := waterRepo.Close(); err != nil {
				l.ErrorContext(ctx, "water repo close error", slog.Any("error", err))
			}
		}()
		svc = svc.WithWater(waterRepo)
	}

	if cfg.Grid.Thermal.Enabled {
		thermalSinks := buildThermalSinks(ctx, l, healthSrv, dbs)
		thermalRepo := multisink.NewThermalRepository(thermalSinks, l)
		defer func() {
			if err := thermalRepo.Close(); err != nil {
				l.ErrorContext(ctx, "thermal repo close error", slog.Any("error", err))
			}
		}()
		svc = svc.WithThermal(thermalRepo)
	}

	startService(ctx, l, "Grid Meter", svc.WithMetrics(appMetrics))
}
