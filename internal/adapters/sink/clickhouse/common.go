// Package clickhouse provides ClickHouse sink adapters for meterlogger.
package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // registers the "clickhouse" database/sql driver
)

// Connection pool tuning. Keep idle connections alive long enough that DNS
// and TCP handshakes are not re-done for every write, while still recycling
// periodically so server restarts and DNS changes are eventually picked up.
// driverName is both the registered database/sql driver and the URL scheme.
const driverName = "clickhouse"

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

// Config holds the connection parameters for a ClickHouse sink.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// buildDSN builds the ClickHouse URL. url.UserPassword escapes credentials
// so special characters cannot break parsing or leak into driver errors.
func buildDSN(cfg Config) string {
	u := url.URL{
		Scheme:   driverName,
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     "/" + cfg.Database,
		RawQuery: "dial_timeout=5s",
	}
	return u.String()
}

// New opens and pings a ClickHouse connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	db, err := sql.Open(driverName, buildDSN(cfg))
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	tunePool(db)
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", pingErr)
	}
	logger.InfoContext(ctx, "connected to ClickHouse", slog.String("host", cfg.Host), slog.String("db", cfg.Database))
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
func (d *DB) Name() string { return driverName }

// Check implements healthserver.Checker.
func (d *DB) Check(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	d.logger.Info("closing ClickHouse connection")
	return d.db.Close()
}
