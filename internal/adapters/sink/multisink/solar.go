package multisink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// SolarRepository fans out to multiple domain.EnvoySolarRepository implementations.
type SolarRepository struct {
	sinks  []domain.EnvoySolarRepository
	logger *slog.Logger
}

// NewSolarRepository wraps the given sinks. Panics if sinks is empty.
func NewSolarRepository(sinks []domain.EnvoySolarRepository, logger *slog.Logger) *SolarRepository {
	if len(sinks) == 0 {
		panic("multisink.SolarRepository: at least one sink required")
	}
	return &SolarRepository{sinks: sinks, logger: logger}
}

// StoreEnvoySolarData writes to all sinks concurrently and returns a combined error.
func (r *SolarRepository) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: solar store failed",
		func(ctx context.Context, s domain.EnvoySolarRepository) error {
			return s.StoreEnvoySolarData(ctx, d)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *SolarRepository) Flush(ctx context.Context) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: solar flush failed",
		func(ctx context.Context, s domain.EnvoySolarRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *SolarRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: solar close failed",
		func(s domain.EnvoySolarRepository) error {
			return s.Close()
		})
}
