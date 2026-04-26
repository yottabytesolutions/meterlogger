//nolint:dupl // each repository type has distinct sink interfaces; extraction would obscure intent
package multisink

import (
	"context"
	"errors"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GridRepository fans out to multiple domain.GridTelegramRepository implementations.
type GridRepository struct {
	sinks  []domain.GridTelegramRepository
	logger *slog.Logger
}

// NewGridRepository wraps the given sinks. Panics if sinks is empty.
func NewGridRepository(sinks []domain.GridTelegramRepository, logger *slog.Logger) *GridRepository {
	if len(sinks) == 0 {
		panic("multisink.GridRepository: at least one sink required")
	}
	return &GridRepository{sinks: sinks, logger: logger}
}

// StoreGridTelegram writes to all sinks and returns a combined error.
func (r *GridRepository) StoreGridTelegram(ctx context.Context, t domain.GridTelegram) error {
	r.logger.DebugContext(ctx, "multisink: storing grid telegram", slog.Int("sinks", len(r.sinks)))
	var errs []error
	for _, s := range r.sinks {
		if err := s.StoreGridTelegram(ctx, t); err != nil {
			r.logger.ErrorContext(ctx, "multisink: grid store failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Flush flushes all sinks and returns a combined error.
func (r *GridRepository) Flush(ctx context.Context) error {
	r.logger.DebugContext(ctx, "multisink: flushing grid sinks", slog.Int("sinks", len(r.sinks)))
	var errs []error
	for _, s := range r.sinks {
		if err := s.Flush(ctx); err != nil {
			r.logger.ErrorContext(ctx, "multisink: grid flush failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close closes all sinks and returns a combined error.
func (r *GridRepository) Close() error {
	var errs []error
	for _, s := range r.sinks {
		if err := s.Close(); err != nil {
			r.logger.Error("multisink: grid close failed", slog.Any("error", err))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
