// Package tdengine provides TDEngine sink adapters for meterlogger.
package tdengine

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/taosdata/driver-go/v3/taosRestful" // TDEngine REST driver
)

// Connection pool tuning. The TDEngine REST driver is HTTP-based; with the
// stdlib default of 2 idle connections a modest burst of writes opens fresh
// HTTP connections and re-resolves DNS. Raising the pool and keeping idle
// connections alive for a long time avoids that.
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

// Config holds the connection parameters for a TDEngine sink. Passed as a
// single value instead of positional strings to eliminate the risk of
// silently transposing user/password/dbname at a call site.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// New opens and pings a TDEngine connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@http(%s:%d)/%s?readBufferSize=52428800",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
	)
	db, err := sql.Open("taosRestful", dsn)
	if err != nil {
		return nil, fmt.Errorf("open tdengine: %w", err)
	}
	tunePool(db)
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping tdengine: %w", pingErr)
	}
	logger.InfoContext(ctx, "connected to TDEngine", slog.String("host", cfg.Host), slog.String("db", cfg.Database))
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
func (d *DB) Name() string { return "tdengine" }

// Check implements healthserver.Checker.
func (d *DB) Check(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	d.logger.Info("closing TDEngine connection")
	return d.db.Close()
}
