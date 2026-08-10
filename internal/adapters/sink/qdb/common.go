// Package qdb provides QuestDB sink adapters for meterlogger.
package qdb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	qdbclient "github.com/questdb/go-questdb-client/v3"
)

const (
	dbCloseTimeout = 5 * time.Second

	// Reconnect backoff bounds. The first retry is immediate enough that a
	// QuestDB restart of a few seconds is absorbed between two flush ticks,
	// while a long outage settles at one dial per maxReconnectDelay instead of
	// hammering the server.
	initialReconnectDelay = 1 * time.Second
	maxReconnectDelay     = 60 * time.Second
	reconnectBackoffScale = 2

	// dialTimeout bounds a single reconnect attempt so a black-holed QuestDB
	// host cannot stall the flush loop.
	dialTimeout = 5 * time.Second

	// unhealthyAfterFailures is how many consecutive failures the sink tolerates
	// before Check reports unhealthy. One failed flush followed by a successful
	// redial is normal during a QuestDB restart and must not flap /readyz.
	unhealthyAfterFailures = 5
)

// ErrDisconnected is returned by Write and Flush while the ILP connection is
// down and the next redial is not due yet. Rows handed to Write in that window
// are dropped.
var ErrDisconnected = errors.New("questdb: ILP connection is down")

// DBClient wraps a QuestDB line sender and owns its reconnect state.
//
// The QuestDB ILP/TCP sender does not redial. A write error leaves the dead
// socket in place, so without this wrapper every later flush fails forever on
// the same connection. DBClient closes the sender on failure, redials with
// bounded exponential backoff, and reports the connection state to the health
// server.
//
// DBClient also implements healthserver.Checker.
type DBClient struct {
	cfg    Config
	logger *slog.Logger

	// dial is a test seam. Production uses dialSender.
	dial func(ctx context.Context, cfg Config) (qdbclient.LineSender, error)
	now  func() time.Time

	// senderMu guards the sender and the dial schedule. Held across network
	// writes, so Check must not take it.
	senderMu       sync.Mutex
	sender         qdbclient.LineSender
	nextDialAt     time.Time
	reconnectDelay time.Duration
	droppedRows    int64
	downSince      time.Time

	// stateMu guards the health fields only.
	stateMu             sync.RWMutex
	consecutiveFailures int
	lastErr             error
}

// Config holds the connection parameters for a QuestDB ILP client.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
}

// NewDBClient opens a persistent ILP/TCP line sender to QuestDB.
func NewDBClient(ctx context.Context, cfg Config, logger *slog.Logger) (*DBClient, error) {
	logger.InfoContext(ctx, "NewDBClient, connecting", slog.String("host", cfg.Host))
	sender, err := dialSender(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &DBClient{
		cfg:            cfg,
		logger:         logger,
		dial:           dialSender,
		now:            time.Now,
		sender:         sender,
		reconnectDelay: initialReconnectDelay,
	}, nil
}

// dialSender opens one ILP/TCP line sender.
func dialSender(ctx context.Context, cfg Config) (qdbclient.LineSender, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	sender, err := qdbclient.NewLineSender(
		ctx,
		qdbclient.WithTcp(),
		qdbclient.WithAddress(addr),
		qdbclient.WithBasicAuth(cfg.User, cfg.Password),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create QuestDB line sender: %w", err)
	}
	return sender, nil
}

// Name implements healthserver.Checker.
func (c *DBClient) Name() string { return "questdb" }

// Check implements healthserver.Checker. QuestDB ILP has no ping message, so
// the connection state plus the outcome of recent writes is the best available
// signal. It reports unhealthy once unhealthyAfterFailures writes or flushes
// have failed in a row, which means the redial is not succeeding either.
//
// Note that a flush with an empty buffer writes no bytes and therefore cannot
// detect a peer that closed the connection. The first buffered row does.
func (c *DBClient) Check(_ context.Context) error {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	if c.consecutiveFailures < unhealthyAfterFailures {
		return nil
	}
	return fmt.Errorf("%d consecutive QuestDB failures: %w", c.consecutiveFailures, c.lastErr)
}

// Write runs build against the current line sender to buffer one row. It is the
// only way writers may reach the sender: DBClient swaps the sender on reconnect
// and serialises access, which a cached reference would defeat.
//
// While the connection is down and the next redial is not due, the row is
// dropped and ErrDisconnected is returned.
func (c *DBClient) Write(ctx context.Context, build func(sender qdbclient.LineSender) error) error {
	c.senderMu.Lock()
	defer c.senderMu.Unlock()

	sender, err := c.ensureSender(ctx)
	if err != nil {
		c.droppedRows++
		return err
	}

	if buildErr := build(sender); buildErr != nil {
		c.connectionFailed(ctx, buildErr)
		return buildErr
	}
	return nil
}

// Flush flushes the underlying line sender. On failure the connection is
// closed and a redial is scheduled; the buffered rows are lost, which is what
// the ILP client does with them on a failed flush anyway.
func (c *DBClient) Flush(ctx context.Context) error {
	c.senderMu.Lock()
	defer c.senderMu.Unlock()

	sender, err := c.ensureSender(ctx)
	if err != nil {
		return err
	}

	if flushErr := sender.Flush(ctx); flushErr != nil {
		c.connectionFailed(ctx, flushErr)
		return flushErr
	}
	c.recordSuccess()
	return nil
}

// ensureSender returns the live sender, redialing when the backoff window has
// elapsed. Callers must hold senderMu.
func (c *DBClient) ensureSender(ctx context.Context) (qdbclient.LineSender, error) {
	if c.sender != nil {
		return c.sender, nil
	}

	now := c.now()
	if now.Before(c.nextDialAt) {
		return nil, ErrDisconnected
	}

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	c.logger.WarnContext(ctx, "questdb: reconnecting", slog.String("host", c.cfg.Host))
	sender, err := c.dial(dialCtx, c.cfg)
	if err != nil {
		c.scheduleRedial(now)
		c.recordFailure(err)
		c.logger.ErrorContext(
			ctx,
			"questdb: reconnect failed",
			slog.Any("error", err),
			slog.Duration("retry_in", c.reconnectDelay),
		)
		return nil, err
	}

	c.logger.InfoContext(
		ctx,
		"questdb: reconnected",
		slog.Duration("down_for", now.Sub(c.downSince)),
		slog.Int64("dropped_rows", c.droppedRows),
	)
	c.sender = sender
	c.droppedRows = 0
	c.reconnectDelay = initialReconnectDelay
	c.nextDialAt = time.Time{}
	c.recordSuccess()
	return sender, nil
}

// connectionFailed tears down the dead sender and schedules a redial. Callers
// must hold senderMu.
func (c *DBClient) connectionFailed(ctx context.Context, err error) {
	now := c.now()
	if c.sender != nil {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbCloseTimeout)
		if closeErr := c.sender.Close(closeCtx); closeErr != nil {
			c.logger.WarnContext(ctx, "questdb: closing failed sender", slog.Any("error", closeErr))
		}
		cancel()
		c.sender = nil
		c.downSince = now
		c.reconnectDelay = initialReconnectDelay
		c.logger.ErrorContext(ctx, "questdb: connection lost, will reconnect", slog.Any("error", err))
	}
	c.nextDialAt = now.Add(c.reconnectDelay)
	c.recordFailure(err)
}

// scheduleRedial grows the backoff for the next dial attempt. Callers must hold
// senderMu.
func (c *DBClient) scheduleRedial(now time.Time) {
	c.nextDialAt = now.Add(c.reconnectDelay)
	c.reconnectDelay = min(c.reconnectDelay*reconnectBackoffScale, maxReconnectDelay)
}

func (c *DBClient) recordFailure(err error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.consecutiveFailures++
	c.lastErr = err
}

func (c *DBClient) recordSuccess() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.consecutiveFailures = 0
	c.lastErr = nil
}

// Close flushes any buffered data and closes the underlying line sender.
func (c *DBClient) Close() {
	c.logger.Info("Closing QuestDB client")

	flushCtx, cancel := context.WithTimeout(context.Background(), dbCloseTimeout)
	defer cancel()

	if err := c.Flush(flushCtx); err != nil {
		c.logger.Error("Failed to flush data", slog.Any("error", err))
	}

	c.senderMu.Lock()
	defer c.senderMu.Unlock()
	if c.sender == nil {
		return
	}
	if err := c.sender.Close(flushCtx); err != nil {
		c.logger.Error("Failed to close sender", slog.Any("error", err))
	}
	c.sender = nil
}
