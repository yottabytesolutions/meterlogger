package mysql

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// SolarStore implements domain.EnvoySolarRepository for MySQL.
type SolarStore struct {
	db     *DB
	table  string
	logger *slog.Logger
}

// NewSolarStore creates and migrates a SolarStore.
func NewSolarStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*SolarStore, error) {
	m := schemastore.NewSQLMigrator(db.db, schemastore.QuestionPlaceholder, logger)
	if err := m.Migrate(ctx, "mysql_solar_"+table, solarMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("mysql solar migration: %w", err)
	}
	return &SolarStore{db: db, table: table, logger: logger}, nil
}

// StoreEnvoySolarData inserts solar data and inverter details into MySQL.
func (s *SolarStore) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s (ts, envoy_serial, production_wh, watt, panel_count) VALUES (?,?,?,?,?)`,
			s.table,
		),
		d.ReadingTime, d.EnvoySerial, d.ProductionWh, d.Watt, d.PanelCount,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "mysql: store solar failed", slog.String("table", s.table), slog.Any("error", err))
		return err
	}

	for _, inv := range d.Inverters {
		_, invErr := s.db.db.ExecContext(
			ctx,
			fmt.Sprintf(
				`INSERT INTO %s_inverters
                (ts, envoy_serial, inverter_serial, channel_id, operating, communicating,
                 producing, phase, watts, peak_watts)
                VALUES (?,?,?,?,?,?,?,?,?,?)`, s.table,
			),
			inv.ReportTime, d.EnvoySerial, inv.SerialNumber, inv.Chaneid,
			inv.Operating, inv.Communicating, inv.Producing,
			inv.Phase, inv.LastReportedWatts, inv.MaxReportWatts,
		)
		if invErr != nil {
			s.logger.ErrorContext(ctx, "mysql: store inverter failed", slog.Any("error", invErr))
			// Continue storing other inverters; report last error.
			err = invErr
		}
	}
	return err
}

// Flush is a no-op for MySQL (auto-commit).
func (s *SolarStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *SolarStore) Close() error { return nil }
