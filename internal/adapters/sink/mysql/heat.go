package mysql

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const nanoJoulesToGigajoules = 1e-9

// HeatStore implements domain.HeatMeterRepository for MySQL.
type HeatStore struct {
	db     *DB
	table  string
	logger *slog.Logger
}

// NewHeatStore creates and migrates a HeatStore.
func NewHeatStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*HeatStore, error) {
	m := schemastore.NewSQLMigrator(db.db, schemastore.QuestionPlaceholder, logger)
	if err := m.Migrate(ctx, "mysql_heat_"+table, heatMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("mysql heat migration: %w", err)
	}
	return &HeatStore{db: db, table: table, logger: logger}, nil
}

// StoreHeatTelegram inserts a heat telegram into MySQL.
func (s *HeatStore) StoreHeatTelegram(ctx context.Context, t domain.HeatTelegram) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s
            (ts, meter_id, serial_no, power_w, energy_gj, t_forward_c, t_return_c, t_diff_c,
             volume_cm3, seconds, max_flow, max_power_w)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, s.table,
		),
		t.Timestamp,
		t.MeterID,
		t.SerialNo,
		t.ActualPower,
		float64(t.Joules)*nanoJoulesToGigajoules,
		t.Tforward,
		t.Treturn,
		t.Tdiff,
		t.VolumeCm3,
		t.SecondsCounter,
		t.MaxFlow,
		t.MaxPower,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "mysql: StoreHeatTelegram failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op for MySQL (auto-commit).
func (s *HeatStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *HeatStore) Close() error { return nil }
