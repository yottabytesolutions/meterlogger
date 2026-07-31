//nolint:dupl // heat and grid share the same fan-out shape but operate on distinct domain types
package multisink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// HeatRepository fans out to multiple domain.HeatMeterRepository implementations.
type HeatRepository struct {
	sinks  []domain.HeatMeterRepository
	logger *slog.Logger
}

// NewHeatRepository wraps the given sinks. Panics if sinks is empty.
func NewHeatRepository(sinks []domain.HeatMeterRepository, logger *slog.Logger) *HeatRepository {
	if len(sinks) == 0 {
		panic("multisink.HeatRepository: at least one sink required")
	}
	return &HeatRepository{sinks: sinks, logger: logger}
}

// StoreHeatTelegram writes to all sinks concurrently and returns a combined error.
func (r *HeatRepository) StoreHeatTelegram(ctx context.Context, t domain.HeatTelegram) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: heat store",
		func(ctx context.Context, s domain.HeatMeterRepository) error {
			return s.StoreHeatTelegram(ctx, t)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *HeatRepository) Flush(ctx context.Context) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: heat flush",
		func(ctx context.Context, s domain.HeatMeterRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *HeatRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: heat close",
		func(s domain.HeatMeterRepository) error {
			return s.Close()
		})
}
