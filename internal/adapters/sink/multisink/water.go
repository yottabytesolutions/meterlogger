//nolint:dupl // water and gas share the same fan-out shape but operate on distinct domain types
package multisink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// WaterRepository fans out to multiple domain.WaterRepository implementations.
type WaterRepository struct {
	sinks  []domain.WaterRepository
	logger *slog.Logger
}

// NewWaterRepository wraps the given sinks. Panics if sinks is empty.
func NewWaterRepository(sinks []domain.WaterRepository, logger *slog.Logger) *WaterRepository {
	if len(sinks) == 0 {
		panic("multisink.WaterRepository: at least one sink required")
	}
	return &WaterRepository{sinks: sinks, logger: logger}
}

// StoreWaterReading writes to all sinks concurrently and returns a combined error.
func (r *WaterRepository) StoreWaterReading(ctx context.Context, reading domain.WaterReading) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: water store",
		func(ctx context.Context, s domain.WaterRepository) error {
			return s.StoreWaterReading(ctx, reading)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *WaterRepository) Flush(ctx context.Context) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: water flush",
		func(ctx context.Context, s domain.WaterRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *WaterRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: water close",
		func(s domain.WaterRepository) error {
			return s.Close()
		})
}
