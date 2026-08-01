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

// ThermalStore implements domain.ThermalRepository for ClickHouse.
type ThermalStore struct {
	db     *DB
	table  string
	logger *slog.Logger
	buf    batchBuffer[domain.ThermalReading]
}

// NewThermalStore creates and migrates a ThermalStore.
func NewThermalStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*ThermalStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_thermal_"+table, thermalMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("clickhouse thermal migration: %w", err)
	}
	return &ThermalStore{db: db, table: table, logger: logger}, nil
}

// StoreThermalReading buffers a thermal reading for ClickHouse.
func (s *ThermalStore) StoreThermalReading(ctx context.Context, r domain.ThermalReading) error {
	warnDropped(ctx, s.logger, s.table, s.buf.add(r))
	return nil
}

// Flush inserts the buffered thermal readings in one transaction. A failed
// batch is re-queued for the next flush.
func (s *ThermalStore) Flush(ctx context.Context) error {
	return flushBatch(ctx, s.db.db, s.logger, s.table, &s.buf, s.insertRows)
}

// The table name comes from config, not user input.
func (s *ThermalStore) insertRows(ctx context.Context, tx *sql.Tx, rows []domain.ThermalReading) error {
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s (ts, received_at, channel, device_type, serial_no, reading_gj)
            VALUES (?,?,?,?,?,?)`, s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare thermal insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		_, execErr := stmt.ExecContext(
			ctx,
			r.CapturedAt, r.ReceivedAt, r.Channel, r.DeviceType, r.SerialNo, r.ReadingGJ,
		)
		if execErr != nil {
			return fmt.Errorf("exec thermal batch insert: %w", execErr)
		}
	}
	return nil
}

// Close flushes pending rows with a bounded timeout. The shared DB is closed
// via DB.Close().
func (s *ThermalStore) Close() error { return closeWithFinalFlush(s.Flush) }
