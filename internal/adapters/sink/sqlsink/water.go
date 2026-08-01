//nolint:dupl // gas, water and thermal stores share the same shape but persist distinct domain types
package sqlsink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// WaterStore implements domain.WaterRepository.
type WaterStore struct {
	db     *DB
	table  string
	insert string
	logger *slog.Logger
}

// NewWaterStore creates and migrates a WaterStore.
func NewWaterStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*WaterStore, error) {
	tables := []migrationTable{{name: table, columns: waterColumns()}}
	if err := migrate(ctx, db, "water", table, "create water table", tables, logger); err != nil {
		return nil, err
	}
	return &WaterStore{
		db:     db,
		table:  table,
		insert: insertSQL(db.dialect, table, waterColumns()),
		logger: logger,
	}, nil
}

// StoreWaterReading inserts a water reading. Deduplication happens in the
// service; the store writes what it is given.
func (s *WaterStore) StoreWaterReading(ctx context.Context, r domain.WaterReading) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insert,
		r.CapturedAt, r.ReceivedAt, r.Channel, r.DeviceType, r.SerialNo, r.ReadingM3,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": StoreWaterReading failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *WaterStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *WaterStore) Close() error { return nil }
