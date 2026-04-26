// Package multisink provides fan-out adapters that write to multiple sinks simultaneously.
//
//nolint:dupl // each repository type has distinct sink interfaces; extraction would obscure intent
package multisink

import (
	"context"
	"errors"
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

// StoreHeatTelegram writes to all sinks and returns a combined error.
func (r *HeatRepository) StoreHeatTelegram(ctx context.Context, t domain.HeatTelegram) error {
	r.logger.DebugContext(ctx, "multisink: storing heat telegram", slog.Int("sinks", len(r.sinks)))
	var errs []error
	for _, s := range r.sinks {
		if err := s.StoreHeatTelegram(ctx, t); err != nil {
			r.logger.ErrorContext(ctx, "multisink: heat store failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Flush flushes all sinks and returns a combined error.
func (r *HeatRepository) Flush(ctx context.Context) error {
	r.logger.DebugContext(ctx, "multisink: flushing heat sinks", slog.Int("sinks", len(r.sinks)))
	var errs []error
	for _, s := range r.sinks {
		if err := s.Flush(ctx); err != nil {
			r.logger.ErrorContext(ctx, "multisink: heat flush failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close closes all sinks and returns a combined error.
func (r *HeatRepository) Close() error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.Close(); err != nil {
			r.logger.Error("multisink: heat close failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
