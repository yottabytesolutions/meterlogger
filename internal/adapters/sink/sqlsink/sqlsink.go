// Package sqlsink implements the SQL sink stores shared by the postgres, mysql,
// timescaledb and tdengine sink packages. A Dialect value supplies everything
// that differs between those backends: driver, placeholder style, column type
// names, migrator and an optional post-create hook.
package sqlsink

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
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

// DB wraps *sql.DB with its dialect and a logger for health checks and
// lifecycle management.
type DB struct {
	db      *sql.DB
	dialect Dialect
	logger  *slog.Logger
}

// Open opens and pings a database connection for the given dialect.
// The host and database parameters are only used for logging.
func Open(ctx context.Context, d Dialect, dsn, host, database string, logger *slog.Logger) (*DB, error) {
	db, err := sql.Open(d.driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", d.name, err)
	}
	tunePool(db)
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s: %w", d.name, pingErr)
	}
	logger.InfoContext(ctx, "connected to "+d.displayName, slog.String("host", host), slog.String("db", database))
	return &DB{db: db, dialect: d, logger: logger}, nil
}

func tunePool(db *sql.DB) {
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxIdleTime(connMaxIdleTime)
	db.SetConnMaxLifetime(connMaxLifetime)
}

// NewDBFromSQL wraps an existing *sql.DB. Used in tests. Pool tuning is
// applied so the test surface mirrors production behaviour.
func NewDBFromSQL(d Dialect, db *sql.DB, logger *slog.Logger) *DB {
	tunePool(db)
	return &DB{db: db, dialect: d, logger: logger}
}

// Name implements healthserver.Checker.
func (d *DB) Name() string { return d.dialect.name }

// Check implements healthserver.Checker.
func (d *DB) Check(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	d.logger.Info("closing " + d.dialect.displayName + " connection")
	return d.db.Close()
}
