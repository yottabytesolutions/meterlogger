package sqlsink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GridStore implements domain.GridTelegramRepository.
type GridStore struct {
	db     *DB
	table  string
	insert string
	logger *slog.Logger
}

// NewGridStore creates and migrates a GridStore.
func NewGridStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*GridStore, error) {
	tables := []migrationTable{{name: table, columns: gridColumns()}}
	if err := migrate(ctx, db, "grid", table, "create grid table", tables, logger); err != nil {
		return nil, err
	}
	return &GridStore{
		db:     db,
		table:  table,
		insert: insertSQL(db.dialect, table, gridColumns()),
		logger: logger,
	}, nil
}

// StoreGridTelegram inserts a grid telegram.
func (s *GridStore) StoreGridTelegram(ctx context.Context, t domain.GridTelegram) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insert,
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
		s.logger.ErrorContext(ctx, s.db.dialect.name+": StoreGridTelegram failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *GridStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *GridStore) Close() error { return nil }
