package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/gridmeter"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/sml"
	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

// newGridReader constructs the grid meter reader selected by
// cfg.Grid.Reader. Shared by runGridMeter and the grid probe so both
// exercise the same wiring.
//
//nolint:ireturn // factory selects between reader implementations behind the domain port
func newGridReader(l *slog.Logger) (domain.GridTelegramReader, error) {
	switch cfg.Grid.Reader {
	case config.GridReaderSML:
		if cfg.Grid.DecryptionKey != "" {
			return nil, fmt.Errorf("Grid.DecryptionKey only applies to the dsmr reader")
		}
		return sml.NewReader(cfg.Grid.SerialInterface, l), nil
	case "", config.GridReaderDSMR:
		reader := gridmeter.NewGridReader(cfg.Grid.SerialInterface, l)
		if cfg.Grid.DecryptionKey != "" {
			return reader.WithDecryption(cfg.Grid.DecryptionKey, cfg.Grid.AuthenticationKey)
		}
		return reader, nil
	}
	return nil, fmt.Errorf("invalid Grid.Reader %q; valid values are dsmr, sml", cfg.Grid.Reader)
}

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

	reader, err := newGridReader(l)
	if err != nil {
		l.ErrorContext(ctx, "failed to create grid reader", slog.Any("error", err))
		os.Exit(1)
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
			if closeErr := gasRepo.Close(); closeErr != nil {
				l.ErrorContext(ctx, "gas repo close error", slog.Any("error", closeErr))
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
