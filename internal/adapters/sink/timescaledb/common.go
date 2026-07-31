// Package timescaledb provides TimescaleDB sink adapters for meterlogger.
// The implementation lives in the sqlsink package; this package supplies
// the TimescaleDB dialect (PostgreSQL plus hypertables) and DSN handling.
package timescaledb

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/url"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx as database/sql driver

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
)

// Config holds the connection parameters for a TimescaleDB sink.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	SSLMode  string
}

// DB wraps *sql.DB with a logger for health checks and lifecycle management.
type DB = sqlsink.DB

// HeatStore implements domain.HeatMeterRepository for TimescaleDB.
type HeatStore = sqlsink.HeatStore

// GridStore implements domain.GridTelegramRepository for TimescaleDB.
type GridStore = sqlsink.GridStore

// SolarStore implements domain.EnvoySolarRepository for TimescaleDB.
type SolarStore = sqlsink.SolarStore

// DucoStore implements domain.DucoRepository for TimescaleDB.
type DucoStore = sqlsink.DucoStore

// buildDSN builds a postgres:// URL so credentials with spaces, quotes, '@'
// or '/' survive intact.
func buildDSN(cfg Config) string {
	sslmode := cfg.SSLMode
	if sslmode == "" {
		sslmode = "disable"
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cfg.User, cfg.Password),
		Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Path:     "/" + cfg.Database,
		RawQuery: url.Values{"sslmode": []string{sslmode}}.Encode(),
	}
	return u.String()
}

// New opens and pings a TimescaleDB connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	return sqlsink.Open(ctx, sqlsink.TimescaleDBDialect(), buildDSN(cfg), cfg.Host, cfg.Database, logger)
}

// NewDBFromSQL wraps an existing *sql.DB. Used in tests. Pool tuning is
// applied so the test surface mirrors production behaviour.
func NewDBFromSQL(db *sql.DB, logger *slog.Logger) *DB {
	return sqlsink.NewDBFromSQL(sqlsink.TimescaleDBDialect(), db, logger)
}

// NewHeatStore creates and migrates a HeatStore.
func NewHeatStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*HeatStore, error) {
	return sqlsink.NewHeatStore(ctx, db, table, logger)
}

// NewGridStore creates and migrates a GridStore.
func NewGridStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*GridStore, error) {
	return sqlsink.NewGridStore(ctx, db, table, logger)
}

// NewSolarStore creates and migrates a SolarStore.
func NewSolarStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*SolarStore, error) {
	return sqlsink.NewSolarStore(ctx, db, table, logger)
}

// NewDucoStore creates and migrates a DucoStore.
func NewDucoStore(ctx context.Context, db *DB, base string, logger *slog.Logger) (*DucoStore, error) {
	return sqlsink.NewDucoStore(ctx, db, base, logger)
}
