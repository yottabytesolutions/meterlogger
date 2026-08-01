//nolint:dupl // gas, water and thermal stores share the same shape but persist distinct domain types
package sqlsink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// ThermalStore implements domain.ThermalRepository.
type ThermalStore struct {
	db     *DB
	table  string
	insert string
	logger *slog.Logger
}

// NewThermalStore creates and migrates a ThermalStore.
func NewThermalStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*ThermalStore, error) {
	tables := []migrationTable{{name: table, columns: thermalColumns()}}
	if err := migrate(ctx, db, "thermal", table, "create thermal table", tables, logger); err != nil {
		return nil, err
	}
	return &ThermalStore{
		db:     db,
		table:  table,
		insert: insertSQL(db.dialect, table, thermalColumns()),
		logger: logger,
	}, nil
}

// StoreThermalReading inserts a thermal reading. Deduplication happens in the
// service; the store writes what it is given.
func (s *ThermalStore) StoreThermalReading(ctx context.Context, r domain.ThermalReading) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insert,
		r.CapturedAt, r.ReceivedAt, r.Channel, r.DeviceType, r.SerialNo, r.ReadingGJ,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": StoreThermalReading failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *ThermalStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *ThermalStore) Close() error { return nil }
