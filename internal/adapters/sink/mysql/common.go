// Package mysql provides MySQL sink adapters for meterlogger.
// The implementation lives in the sqlsink package; this package supplies
// the MySQL dialect and DSN handling.
package mysql

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"strconv"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
)

// Config holds the connection parameters for a MySQL sink.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// DB wraps *sql.DB for MySQL.
type DB = sqlsink.DB

// HeatStore implements domain.HeatMeterRepository for MySQL.
type HeatStore = sqlsink.HeatStore

// GridStore implements domain.GridTelegramRepository for MySQL.
type GridStore = sqlsink.GridStore

// SolarStore implements domain.EnvoySolarRepository for MySQL.
type SolarStore = sqlsink.SolarStore

// DucoStore implements domain.DucoRepository for MySQL.
type DucoStore = sqlsink.DucoStore

// buildDSN builds the DSN via the driver's own formatter so credentials with
// special characters survive intact.
func buildDSN(cfg Config) string {
	mc := gomysql.NewConfig()
	mc.User = cfg.User
	mc.Passwd = cfg.Password
	mc.Net = "tcp"
	mc.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	mc.DBName = cfg.Database
	mc.ParseTime = true
	mc.MultiStatements = true
	return mc.FormatDSN()
}

// New opens and pings a MySQL connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	return sqlsink.Open(ctx, sqlsink.MySQLDialect(), buildDSN(cfg), cfg.Host, cfg.Database, logger)
}

// NewDBFromSQL wraps an existing *sql.DB. Used in tests. Pool tuning is
// applied so the test surface mirrors production behaviour.
func NewDBFromSQL(db *sql.DB, logger *slog.Logger) *DB {
	return sqlsink.NewDBFromSQL(sqlsink.MySQLDialect(), db, logger)
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
