package multisink

import (
	"context"
	"errors"
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

// StoreEnvoySolarData writes to all sinks and returns a combined error.
func (r *SolarRepository) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.StoreEnvoySolarData(ctx, d); err != nil {
			r.logger.ErrorContext(ctx, "multisink: solar store failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Flush flushes all sinks and returns a combined error.
func (r *SolarRepository) Flush(ctx context.Context) error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.Flush(ctx); err != nil {
			r.logger.ErrorContext(ctx, "multisink: solar flush failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close closes all sinks and returns a combined error.
func (r *SolarRepository) Close() error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.Close(); err != nil {
			r.logger.Error("multisink: solar close failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
