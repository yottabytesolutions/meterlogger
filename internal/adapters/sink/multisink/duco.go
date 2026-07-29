package multisink

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// DucoRepository fans out to multiple domain.DucoRepository implementations.
type DucoRepository struct {
	sinks  []domain.DucoRepository
	logger *slog.Logger
}

// NewDucoRepository wraps the given sinks. Panics if sinks is empty.
func NewDucoRepository(sinks []domain.DucoRepository, logger *slog.Logger) *DucoRepository {
	if len(sinks) == 0 {
		panic("multisink.DucoRepository: at least one sink required")
	}
	return &DucoRepository{sinks: sinks, logger: logger}
}

// StoreBoxStatus writes to all sinks concurrently and returns a combined error.
func (r *DucoRepository) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: duco box store failed",
		func(ctx context.Context, s domain.DucoRepository) error {
			return s.StoreBoxStatus(ctx, b)
		})
}

// StoreNodeData writes to all sinks concurrently and returns a combined error.
func (r *DucoRepository) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: duco node store failed",
		func(ctx context.Context, s domain.DucoRepository) error {
			return s.StoreNodeData(ctx, nodeData)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *DucoRepository) Flush(ctx context.Context) error {
	return fanOut(ctx, r.sinks, r.logger, "multisink: duco flush failed",
		func(ctx context.Context, s domain.DucoRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *DucoRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: duco close failed",
		func(s domain.DucoRepository) error {
			return s.Close()
		})
}
