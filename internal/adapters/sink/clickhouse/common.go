// Package clickhouse provides ClickHouse sink adapters for meterlogger.
package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // registers the "clickhouse" database/sql driver
)

// Connection pool tuning. Keep idle connections alive long enough that DNS
// and TCP handshakes are not re-done for every write, while still recycling
// periodically so server restarts and DNS changes are eventually picked up.
const (
	maxOpenConns    = 4
	maxIdleConns    = 4
	connMaxIdleTime = 30 * time.Minute
	connMaxLifetime = 1 * time.Hour
)

// DB wraps *sql.DB with a logger for health checks and lifecycle management.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
}

// New opens and pings a ClickHouse connection.
func New(
	ctx context.Context,
	host string,
	port int,
	user, password, dbname string,
	logger *slog.Logger,
) (*DB, error) {
	dsn := fmt.Sprintf(
		"clickhouse://%s:%s@%s/%s?dial_timeout=5s",
		user, password, net.JoinHostPort(host, strconv.Itoa(port)), dbname,
	)
	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	tunePool(db)
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", pingErr)
	}
	logger.InfoContext(ctx, "connected to ClickHouse", slog.String("host", host), slog.String("db", dbname))
	return &DB{db: db, logger: logger}, nil
}

// tunePool applies the package pool settings to db.
func tunePool(db *sql.DB) {
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxIdleTime(connMaxIdleTime)
	db.SetConnMaxLifetime(connMaxLifetime)
}

// NewDBFromSQL wraps an existing *sql.DB. Used in tests. Pool tuning is
// applied so the test surface mirrors production behaviour.
func NewDBFromSQL(db *sql.DB, logger *slog.Logger) *DB {
	tunePool(db)
	return &DB{db: db, logger: logger}
}

// Name implements healthserver.Checker.
func (d *DB) Name() string { return "clickhouse" }

// Check implements healthserver.Checker.
func (d *DB) Check(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	d.logger.Info("closing ClickHouse connection")
	return d.db.Close()
}
