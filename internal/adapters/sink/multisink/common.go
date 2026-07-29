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
// pre-sized error slice, so no locking is required to collect results safely.
func fanOut[S any](
	ctx context.Context,
	sinks []S,
	logger *slog.Logger,
	failMsg string,
	write func(context.Context, S) error,
) error {
	errs := make([]error, len(sinks))
	var wg sync.WaitGroup
	wg.Add(len(sinks))
	for i, s := range sinks {
		go func(i int, s S) {
			defer wg.Done()
			if err := write(ctx, s); err != nil {
				logger.ErrorContext(ctx, failMsg, slog.Any("error", err))
				errs[i] = err
			}
		}(i, s)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// fanOutClose calls closeFn on every sink concurrently, waits for all of them to finish, and
// returns a combined error via errors.Join.
func fanOutClose[S any](sinks []S, logger *slog.Logger, failMsg string, closeFn func(S) error) error {
	errs := make([]error, len(sinks))
	var wg sync.WaitGroup
	wg.Add(len(sinks))
	for i, s := range sinks {
		go func(i int, s S) {
			defer wg.Done()
			if err := closeFn(s); err != nil {
				logger.Error(failMsg, slog.Any("error", err))
				errs[i] = err
			}
		}(i, s)
	}
	wg.Wait()
	return errors.Join(errs...)
}
