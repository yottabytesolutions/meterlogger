package multisink

import (
	"context"
	"errors"
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

// StoreBoxStatus writes to all sinks and returns a combined error.
func (r *DucoRepository) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.StoreBoxStatus(ctx, b); err != nil {
			r.logger.ErrorContext(ctx, "multisink: duco box store failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// StoreNodeData writes to all sinks and returns a combined error.
func (r *DucoRepository) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.StoreNodeData(ctx, nodeData); err != nil {
			r.logger.ErrorContext(ctx, "multisink: duco node store failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Flush flushes all sinks and returns a combined error.
func (r *DucoRepository) Flush(ctx context.Context) error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.Flush(ctx); err != nil {
			r.logger.ErrorContext(ctx, "multisink: duco flush failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close closes all sinks and returns a combined error.
func (r *DucoRepository) Close() error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.Close(); err != nil {
			r.logger.Error("multisink: duco close failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
