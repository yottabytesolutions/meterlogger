package sqlsink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const nanoJoulesToGigajoules = 1e-9

// HeatStore implements domain.HeatMeterRepository.
type HeatStore struct {
	db     *DB
	table  string
	insert string
	logger *slog.Logger
}

// NewHeatStore creates and migrates a HeatStore.
func NewHeatStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*HeatStore, error) {
	tables := []migrationTable{{name: table, columns: heatColumns()}}
	if err := migrate(ctx, db, "heat", table, "create heat table", tables, logger); err != nil {
		return nil, err
	}
	return &HeatStore{
		db:     db,
		table:  table,
		insert: insertSQL(db.dialect, table, heatColumns()),
		logger: logger,
	}, nil
}

// StoreHeatTelegram inserts a heat telegram.
func (s *HeatStore) StoreHeatTelegram(ctx context.Context, t domain.HeatTelegram) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insert,
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
		s.logger.ErrorContext(ctx, s.db.dialect.name+": StoreHeatTelegram failed",
			slog.String("table", s.table), slog.Any("error", err))
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *HeatStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *HeatStore) Close() error { return nil }
