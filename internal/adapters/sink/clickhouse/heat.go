package clickhouse

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const nanoJoulesToGigajoules = 1e-9

// HeatStore implements domain.HeatMeterRepository for ClickHouse.
type HeatStore struct {
	db     *DB
	table  string
	logger *slog.Logger
	buf    batchBuffer[domain.HeatTelegram]
}

// NewHeatStore creates and migrates a HeatStore.
func NewHeatStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*HeatStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_heat_"+table, heatMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("clickhouse heat migration: %w", err)
	}
	return &HeatStore{db: db, table: table, logger: logger}, nil
}

// StoreHeatTelegram buffers a heat telegram for ClickHouse.
func (s *HeatStore) StoreHeatTelegram(ctx context.Context, t domain.HeatTelegram) error {
	warnDropped(ctx, s.logger, s.table, s.buf.add(t))
	return nil
}

// Flush performs a batch insert into ClickHouse. On failure the batch is
// re-queued for the next flush.
func (s *HeatStore) Flush(ctx context.Context) error {
	batch := s.buf.take()
	if len(batch) == 0 {
		return nil
	}
	if err := s.insertBatch(ctx, batch); err != nil {
		warnDropped(ctx, s.logger, s.table, s.buf.requeue(batch))
		return err
	}
	return nil
}

func (s *HeatStore) insertBatch(ctx context.Context, batch []domain.HeatTelegram) error {
	tx, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin clickhouse transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The table name comes from config, not user input.
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

// Close flushes pending rows with a bounded timeout. The shared DB is closed
// via DB.Close().
func (s *HeatStore) Close() error { return closeWithFinalFlush(s.Flush) }
