// Package mysql provides MySQL sink adapters for meterlogger.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL driver.
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

// DB wraps *sql.DB for MySQL.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
}

// Config holds the connection parameters for a MySQL sink. Passed as a
// single value instead of positional strings to eliminate the risk of
// silently transposing user/password/dbname at a call site.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// New opens and pings a MySQL connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database,
	)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	tunePool(db)
	if pingErr := db.PingContext(ctx); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", pingErr)
	}
	logger.InfoContext(ctx, "connected to MySQL", slog.String("host", cfg.Host), slog.String("db", cfg.Database))
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
func (d *DB) Name() string { return "mysql" }

// Check implements healthserver.Checker.
func (d *DB) Check(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (d *DB) Close() error {
	d.logger.Info("closing MySQL connection")
	return d.db.Close()
}
