package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GridStore implements domain.GridTelegramRepository for PostgreSQL.
type GridStore struct {
	db     *DB
	table  string
	logger *slog.Logger
}

// NewGridStore creates and migrates a GridStore.
func NewGridStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*GridStore, error) {
	m := schemastore.NewSQLMigrator(db.db, schemastore.DollarPlaceholder, logger)
	if err := m.Migrate(ctx, "postgres_grid_"+table, gridMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("postgres grid migration: %w", err)
	}
	return &GridStore{db: db, table: table, logger: logger}, nil
}

// StoreGridTelegram inserts a grid telegram into PostgreSQL.
func (s *GridStore) StoreGridTelegram(ctx context.Context, t domain.GridTelegram) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s
            (ts, meter_type, serial_no,
             usage_counter1, usage_counter2, output_counter1, output_counter2,
             total_power_usage, total_power_output,
             brownouts_p1, brownouts_p2, brownouts_p3,
             spikes_p1, spikes_p2, spikes_p3,
             voltage_p1, voltage_p2, voltage_p3,
             current_p1, current_p2, current_p3,
             power_usage_p1, power_usage_p2, power_usage_p3,
             power_output_p1, power_output_p2, power_output_p3)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,
                    $16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`, s.table,
		),
		t.Time, t.MeterMerkType, t.Serienummer,
		t.UsageCounter1, t.UsageCounter2, t.OutputCounter1, t.OutputCounter2,
		t.TotalPowerUsage, t.TotalPowerOutput,
		t.BrownoutsP1, t.BrownoutsP2, t.BrownoutsP3,
		t.SpikesP1, t.SpikesP2, t.SpikesP3,
		t.VoltageP1, t.VoltageP2, t.VoltageP3,
		t.CurrentP1, t.CurrentP2, t.CurrentP3,
		t.PowerUsageP1, t.PowerUsageP2, t.PowerUsageP3,
		t.PowerOutputP1, t.PowerOutputP2, t.PowerOutputP3,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "postgres: StoreGridTelegram failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op for PostgreSQL (auto-commit).
func (s *GridStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *GridStore) Close() error { return nil }
