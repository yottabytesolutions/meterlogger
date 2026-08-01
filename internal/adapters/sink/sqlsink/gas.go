package sqlsink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GasStore implements domain.GasRepository.
type GasStore struct {
	db     *DB
	table  string
	insert string
	logger *slog.Logger
}

// NewGasStore creates and migrates a GasStore.
func NewGasStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*GasStore, error) {
	tables := []migrationTable{{name: table, columns: gasColumns()}}
	if err := migrate(ctx, db, "gas", table, "create gas table", tables, logger); err != nil {
		return nil, err
	}
	return &GasStore{
		db:     db,
		table:  table,
		insert: insertSQL(db.dialect, table, gasColumns()),
		logger: logger,
	}, nil
}

// StoreGasReading inserts a gas reading. Deduplication happens in the service;
// the store writes what it is given.
func (s *GasStore) StoreGasReading(ctx context.Context, r domain.GasReading) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insert,
		r.CapturedAt, r.ReceivedAt, r.Channel, r.DeviceType, r.SerialNo, r.ReadingM3,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": StoreGasReading failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *GasStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *GasStore) Close() error { return nil }
