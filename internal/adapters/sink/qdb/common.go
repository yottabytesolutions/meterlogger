// Package qdb provides QuestDB sink adapters for meterlogger.
package qdb

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	qdb "github.com/questdb/go-questdb-client/v3"
)

const dbCloseTimeout = 5 * time.Second

// DBClient wraps a QuestDB line sender and records the result of the most
// recent Flush so the health server can report QuestDB liveness without
// opening a new TCP connection (which would trigger a fresh DNS lookup).
//
// DBClient also implements healthserver.Checker.
type DBClient struct {
	sender qdb.LineSender
	logger *slog.Logger

	mu           sync.RWMutex
	lastFlushErr error
}

// Config holds the connection parameters for a QuestDB ILP client. Passed as
// a single value instead of positional strings to eliminate the risk of
// silently transposing user/password at a call site.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

// NewDBClient opens a persistent ILP/TCP line sender to QuestDB.
func NewDBClient(ctx context.Context, cfg Config, logger *slog.Logger) (*DBClient, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	logger.InfoContext(ctx, "NewDBClient, connecting", slog.String("host", cfg.Host))
	sender, err := qdb.NewLineSender(
		ctx,
		qdb.WithTcp(),
		qdb.WithAddress(addr),
		qdb.WithBasicAuth(cfg.User, cfg.Password),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create QuestDB line sender: %w", err)
	}
	return &DBClient{
		sender: sender,
		logger: logger,
	}, nil
}

// Name implements healthserver.Checker.
func (c *DBClient) Name() string { return "questdb" }

// Check implements healthserver.Checker. It reports the outcome of the most
// recent Flush. Because QuestDB ILP has no ping message, the flush result is
// the best available signal; it reuses the already-open TCP connection and
// therefore triggers no new DNS lookup.
func (c *DBClient) Check(_ context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastFlushErr
}

// Flush flushes the underlying line sender and records the outcome for Check.
// Writers should call this instead of sender.Flush directly.
func (c *DBClient) Flush(ctx context.Context) error {
	err := c.sender.Flush(ctx)
	c.mu.Lock()
	c.lastFlushErr = err
	c.mu.Unlock()
	return err
}

// Close flushes any buffered data and closes the underlying line sender.
func (c *DBClient) Close() {
	c.logger.Info("Closing QuestDB client")

	flushCtx, cancel := context.WithTimeout(context.Background(), dbCloseTimeout)
	defer cancel()

	if err := c.Flush(flushCtx); err != nil {
		c.logger.Error("Failed to flush data", slog.Any("error", err))
	}

	if err := c.sender.Close(flushCtx); err != nil {
		c.logger.Error("Failed to close sender", slog.Any("error", err))
	}
}
