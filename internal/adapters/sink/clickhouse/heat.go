package clickhouse

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const nanoJoulesToGigajoules = 1e-9

// HeatStore implements domain.HeatMeterRepository for ClickHouse.
type HeatStore struct {
	db     *DB
	table  string
	logger *slog.Logger
	mu     sync.Mutex
	buffer []domain.HeatTelegram
}

// NewHeatStore creates and migrates a HeatStore.
func NewHeatStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*HeatStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_heat_"+table, heatMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("clickhouse heat migration: %w", err)
	}
	return &HeatStore{db: db, table: table, logger: logger, buffer: make([]domain.HeatTelegram, 0)}, nil
}

// StoreHeatTelegram buffers a heat telegram for ClickHouse.
func (s *HeatStore) StoreHeatTelegram(_ context.Context, t domain.HeatTelegram) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffer = append(s.buffer, t)
	return nil
}

// Flush performs a batch insert into ClickHouse.
func (s *HeatStore) Flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return nil
	}
	batch := s.buffer
	s.buffer = make([]domain.HeatTelegram, 0)
	s.mu.Unlock()

	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clickhouse transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s
            (ts, meter_id, serial_no, power_w, energy_gj, t_forward_c, t_return_c, t_diff_c,
             volume_cm3, seconds, max_flow, max_power_w)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare clickhouse insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, t := range batch {
		_, err = stmt.ExecContext(
			ctx,
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
			return fmt.Errorf("exec batch insert: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit clickhouse batch: %w", err)
	}

	return nil
}

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *HeatStore) Close() error { return nil }
