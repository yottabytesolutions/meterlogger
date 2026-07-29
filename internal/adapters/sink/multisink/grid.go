//nolint:dupl // heat and grid share the same fan-out shape but operate on distinct domain types
package multisink

import (
	"context"
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

// StoreGridTelegram writes to all sinks concurrently and returns a combined error.
func (r *GridRepository) StoreGridTelegram(ctx context.Context, t domain.GridTelegram) error {
	r.logger.DebugContext(ctx, "multisink: storing grid telegram", slog.Int("sinks", len(r.sinks)))
	return fanOut(ctx, r.sinks, r.logger, "multisink: grid store failed",
		func(ctx context.Context, s domain.GridTelegramRepository) error {
			return s.StoreGridTelegram(ctx, t)
		})
}

// Flush flushes all sinks concurrently and returns a combined error.
func (r *GridRepository) Flush(ctx context.Context) error {
	r.logger.DebugContext(ctx, "multisink: flushing grid sinks", slog.Int("sinks", len(r.sinks)))
	return fanOut(ctx, r.sinks, r.logger, "multisink: grid flush failed",
		func(ctx context.Context, s domain.GridTelegramRepository) error {
			return s.Flush(ctx)
		})
}

// Close closes all sinks concurrently and returns a combined error.
func (r *GridRepository) Close() error {
	return fanOutClose(r.sinks, r.logger, "multisink: grid close failed",
		func(s domain.GridTelegramRepository) error {
			return s.Close()
		})
}
