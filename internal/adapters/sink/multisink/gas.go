//nolint:dupl // gas and grid share the same fan-out shape but operate on distinct domain types
package multisink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GasRepository fans out to multiple domain.GasRepository implementations.
type GasRepository struct {
	sinks  []domain.GasRepository
	logger *slog.Logger
}

// NewGasRepository wraps the given sinks. Panics if sinks is empty.
func NewGasRepository(sinks []domain.GasRepository, logger *slog.Logger) *GasRepository {
	if len(sinks) == 0 {
		panic("multisink.GasRepository: at least one sink required")
	}
	return &GasRepository{sinks: sinks, logger: logger}
}

// StoreGasReading writes to all sinks concurrently and returns a combined error.
func (r *GasRepository) StoreGasReading(ctx context.Context, reading domain.GasReading) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: gas store",
		func(ctx context.Context, s domain.GasRepository) error {
			return s.StoreGasReading(ctx, reading)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *GasRepository) Flush(ctx context.Context) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: gas flush",
		func(ctx context.Context, s domain.GasRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *GasRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: gas close",
		func(s domain.GasRepository) error {
			return s.Close()
		})
}
