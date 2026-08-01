//nolint:dupl // gas, water and thermal stores share the same shape but persist distinct domain types
package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GasStore implements domain.GasRepository for ClickHouse.
type GasStore struct {
	db     *DB
	table  string
	logger *slog.Logger
	buf    batchBuffer[domain.GasReading]
}

// NewGasStore creates and migrates a GasStore.
func NewGasStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*GasStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_gas_"+table, gasMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("clickhouse gas migration: %w", err)
	}
	return &GasStore{db: db, table: table, logger: logger}, nil
}

// StoreGasReading buffers a gas reading for ClickHouse.
func (s *GasStore) StoreGasReading(ctx context.Context, r domain.GasReading) error {
	warnDropped(ctx, s.logger, s.table, s.buf.add(r))
	return nil
}

// Flush inserts the buffered gas readings in one transaction. A failed batch
// is re-queued for the next flush.
func (s *GasStore) Flush(ctx context.Context) error {
	return flushBatch(ctx, s.db.db, s.logger, s.table, &s.buf, s.insertRows)
}

// The table name comes from config, not user input.
func (s *GasStore) insertRows(ctx context.Context, tx *sql.Tx, rows []domain.GasReading) error {
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s (ts, received_at, channel, device_type, serial_no, reading_m3)
            VALUES (?,?,?,?,?,?)`, s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare gas insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		_, execErr := stmt.ExecContext(
			ctx,
			r.CapturedAt, r.ReceivedAt, r.Channel, r.DeviceType, r.SerialNo, r.ReadingM3,
		)
		if execErr != nil {
			return fmt.Errorf("exec gas batch insert: %w", execErr)
		}
	}
	return nil
}

// Close flushes pending rows with a bounded timeout. The shared DB is closed
// via DB.Close().
func (s *GasStore) Close() error { return closeWithFinalFlush(s.Flush) }
