// Package timescaledb provides TimescaleDB sink adapters for meterlogger.
package timescaledb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx as database/sql driver
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

// Config holds the connection parameters for a TimescaleDB sink. Passed as a
// single value instead of positional strings to eliminate the risk of
// silently transposing user/password/dbname/sslmode at a call site.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// New opens and pings a TimescaleDB connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	sslmode := cfg.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, sslmode,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open timescaledb: %w", err)
	}
	tunePool(db)
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping timescaledb: %w", pingErr)
	}
	logger.InfoContext(ctx, "connected to TimescaleDB", slog.String("host", cfg.Host), slog.String("db", cfg.Database))
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
func (d *DB) Name() string { return "timescaledb" }

// Check implements healthserver.Checker.
func (d *DB) Check(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	d.logger.Info("closing TimescaleDB connection")
	return d.db.Close()
}
