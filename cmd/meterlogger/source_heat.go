package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mqtt"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/kamstrup"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/multical401"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/source/serialmbus"
	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

// newHeatReader constructs the heat meter reader selected by cfg.Heat.Reader.
// Shared by runHeatMeter and the heat probe so both exercise the same wiring.
//
//nolint:ireturn // factory selects between reader implementations behind the domain port
func newHeatReader(ctx context.Context, l *slog.Logger) (domain.HeatMeterReader, error) {
	switch cfg.Heat.Reader {
	case config.HeatReaderOptical:
		return kamstrup.NewReader(ctx, cfg.Heat.SerialInterface, l)
	case config.HeatReaderOptical401:
		return multical401.NewReader(ctx, cfg.Heat.SerialInterface, multical401.Config{
			EnergyUnit:     cfg.Heat.Optical401.EnergyUnit,
			EnergyDecimals: cfg.Heat.Optical401.EnergyDecimals,
			VolumeDecimals: cfg.Heat.Optical401.VolumeDecimals,
			PowerDecimals:  cfg.Heat.Optical401.PowerDecimals,
			FlowDecimals:   cfg.Heat.Optical401.FlowDecimals,
		}, l)
	case "", config.HeatReaderMbus:
		addr := byte(cfg.Heat.MbusAddress) //nolint:gosec // G115: intentional conversion of config value
		return serialmbus.NewReader(ctx, cfg.Heat.SerialInterface, addr, l)
	}
	return nil, fmt.Errorf("invalid Heat.Reader %q; valid values are mbus, optical, optical401", cfg.Heat.Reader)
}

//nolint:dupl // per-source constructor sets are parallel by design, distinct domain types
func buildHeatSinks(
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
) []domain.HeatMeterRepository {
	return buildSourceSinks(ctx, l, healthSrv, dbs, cfg.Heat.Measurement,
		func(c *qdb.DBClient, m string, l *slog.Logger) domain.HeatMeterRepository {
			return qdb.NewQuestDBHeatTelegramWriter(c, m, l)
		},
		func(c *mqtt.Client, m string, l *slog.Logger) domain.HeatMeterRepository {
			return mqtt.NewHeatWriter(c, m, l)
		},
		func(ctx context.Context, db *sqlsink.DB, m string, l *slog.Logger) (domain.HeatMeterRepository, error) {
			return sqlsink.NewHeatStore(ctx, db, m, l)
		},
		func(ctx context.Context, db *clickhouse.DB, m string, l *slog.Logger) (domain.HeatMeterRepository, error) {
			return clickhouse.NewHeatStore(ctx, db, m, l)
		},
	)
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

	reader, err := newHeatReader(ctx, l)
	if err != nil {
		l.ErrorContext(ctx, "failed to create heat reader", slog.Any("error", err))
		os.Exit(1)
	}

	startService(
		ctx, l, "Heat Meter", service.NewHeatMeterLoggingService(
			reader,
			repo,
			cfg.Heat.ScrapeInterval,
			cfg.FlushInterval,
			l,
		).WithMetrics(appMetrics),
	)
}
