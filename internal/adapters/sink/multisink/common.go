// Package multisink provides fan-out adapters that write to multiple sinks simultaneously.
package multisink

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// fanOut calls write on every sink concurrently, waits for all of them to finish, and returns
// a combined error via errors.Join. Each sink gets its own goroutine and its own slot in a
// pre-sized error slice, so no locking is required to collect results safely. op describes the
// operation, e.g. "multisink: grid store"; it is logged at debug level before the fan-out and
// with " failed" appended on per-sink errors.
func fanOut[S any](
	ctx context.Context,
	sinks []S,
	logger *slog.Logger,
	op string,
	write func(context.Context, S) error,
) error {
	logger.DebugContext(ctx, op, slog.Int("sinks", len(sinks)))
	errs := make([]error, len(sinks))
	var wg sync.WaitGroup
	wg.Add(len(sinks))
	for i, s := range sinks {
		go func(i int, s S) {
			defer wg.Done()
			if err := write(ctx, s); err != nil {
				logger.ErrorContext(ctx, op+" failed", slog.Any("error", err))
				errs[i] = err
			}
		}(i, s)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// fanOutClose closes every sink concurrently via fanOut. Close has no caller context, so a
// background context is used.
func fanOutClose[S any](sinks []S, logger *slog.Logger, op string, closeFn func(S) error) error {
	return fanOut(context.Background(), sinks, logger, op, func(_ context.Context, s S) error {
		return closeFn(s)
	})
}
