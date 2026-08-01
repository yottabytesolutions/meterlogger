package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	// maxBufferedRows caps each store buffer so a long ClickHouse outage
	// cannot grow memory without bound. Oldest rows are dropped beyond it.
	maxBufferedRows = 10000
	// closeFlushTimeout bounds the final flush performed on Close.
	closeFlushTimeout = 5 * time.Second
)

// batchBuffer accumulates rows between flushes. Safe for concurrent use.
type batchBuffer[T any] struct {
	mu   sync.Mutex
	rows []T
}

// add appends a row and returns the number of oldest rows dropped to stay
// under the cap.
func (b *batchBuffer[T]) add(row T) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rows = append(b.rows, row)
	return b.trimLocked()
}

// take removes and returns all buffered rows.
func (b *batchBuffer[T]) take() []T {
	b.mu.Lock()
	defer b.mu.Unlock()
	rows := b.rows
	b.rows = nil
	return rows
}

// requeue puts a failed batch back at the front of the buffer, preserving
// insertion order. Returns the number of oldest rows dropped to stay under
// the cap.
func (b *batchBuffer[T]) requeue(batch []T) int {
	if len(batch) == 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rows = append(batch, b.rows...)
	return b.trimLocked()
}

// trimLocked drops the oldest rows beyond the cap. Caller must hold b.mu.
func (b *batchBuffer[T]) trimLocked() int {
	over := len(b.rows) - maxBufferedRows
	if over <= 0 {
		return 0
	}
	b.rows = append([]T(nil), b.rows[over:]...)
	return over
}

// warnDropped logs a warning when buffered rows were dropped.
func warnDropped(ctx context.Context, logger *slog.Logger, table string, dropped int) {
	if dropped == 0 {
		return
	}
	logger.WarnContext(
		ctx, "clickhouse: buffer cap reached, dropped oldest rows",
		slog.String("table", table), slog.Int("dropped", dropped),
	)
}

// closeWithFinalFlush runs flush with a bounded timeout. Used by the store
// Close methods so pending rows are not lost on shutdown.
func closeWithFinalFlush(flush func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), closeFlushTimeout)
	defer cancel()
	if err := flush(ctx); err != nil {
		return fmt.Errorf("final flush on close: %w", err)
	}
	return nil
}

// flushBatch drains one buffer and inserts its rows inside a dedicated
// transaction. The clickhouse-go driver supports exactly one prepared batch
// per transaction, so a flush spanning several tables must run one
// transaction per table. On failure the rows are re-queued for the next
// flush.
func flushBatch[T any](
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
	table string,
	buf *batchBuffer[T],
	insert func(ctx context.Context, tx *sql.Tx, rows []T) error,
) error {
	rows := buf.take()
	if len(rows) == 0 {
		return nil
	}
	err := func() error {
		tx, txErr := db.BeginTx(ctx, nil)
		if txErr != nil {
			return fmt.Errorf("begin clickhouse transaction: %w", txErr)
		}
		defer func() { _ = tx.Rollback() }()
		if insErr := insert(ctx, tx, rows); insErr != nil {
			return insErr
		}
		return tx.Commit()
	}()
	if err != nil {
		warnDropped(ctx, logger, table, buf.requeue(rows))
		return fmt.Errorf("%s: %w", table, err)
	}
	return nil
}
