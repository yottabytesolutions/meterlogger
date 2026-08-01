//nolint:dupl // thermal and gas share the same fan-out shape but operate on distinct domain types
package multisink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// ThermalRepository fans out to multiple domain.ThermalRepository implementations.
type ThermalRepository struct {
	sinks  []domain.ThermalRepository
	logger *slog.Logger
}

// NewThermalRepository wraps the given sinks. Panics if sinks is empty.
func NewThermalRepository(sinks []domain.ThermalRepository, logger *slog.Logger) *ThermalRepository {
	if len(sinks) == 0 {
		panic("multisink.ThermalRepository: at least one sink required")
	}
	return &ThermalRepository{sinks: sinks, logger: logger}
}

// StoreThermalReading writes to all sinks concurrently and returns a combined error.
func (r *ThermalRepository) StoreThermalReading(ctx context.Context, reading domain.ThermalReading) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: thermal store",
		func(ctx context.Context, s domain.ThermalRepository) error {
			return s.StoreThermalReading(ctx, reading)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *ThermalRepository) Flush(ctx context.Context) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: thermal flush",
		func(ctx context.Context, s domain.ThermalRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *ThermalRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: thermal close",
		func(s domain.ThermalRepository) error {
			return s.Close()
		})
}
