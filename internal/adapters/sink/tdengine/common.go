// Package tdengine provides TDEngine sink adapters for meterlogger.
// The implementation lives in the sqlsink package; this package supplies
// the TDEngine dialect and DSN handling.
package tdengine

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/url"
	"strconv"

	_ "github.com/taosdata/driver-go/v3/taosRestful" // TDEngine REST driver

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
)

// readBufferSize is the REST driver's response read buffer (50 MiB).
const readBufferSize = 52428800

// Config holds the connection parameters for a TDEngine sink.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// DB wraps *sql.DB with a logger for health checks and lifecycle management.
type DB = sqlsink.DB

// HeatStore implements domain.HeatMeterRepository for TDEngine.
type HeatStore = sqlsink.HeatStore

// GridStore implements domain.GridTelegramRepository for TDEngine.
type GridStore = sqlsink.GridStore

// SolarStore implements domain.EnvoySolarRepository for TDEngine.
type SolarStore = sqlsink.SolarStore

// DucoStore implements domain.DucoRepository for TDEngine.
type DucoStore = sqlsink.DucoStore

// buildDSN builds the taosRestful DSN. User and password are query-escaped,
// which the driver's parser unescapes, so credentials with '@', ':' or '/'
// survive intact.
func buildDSN(cfg Config) string {
	return url.QueryEscape(cfg.User) + ":" + url.QueryEscape(cfg.Password) +
		"@http(" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + ")/" +
		cfg.Database + "?readBufferSize=" + strconv.Itoa(readBufferSize)
}

// New opens and pings a TDEngine connection.
func New(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	return sqlsink.Open(ctx, sqlsink.TDEngineDialect(), buildDSN(cfg), cfg.Host, cfg.Database, logger)
}

// NewDBFromSQL wraps an existing *sql.DB. Used in tests. Pool tuning is
// applied so the test surface mirrors production behaviour.
func NewDBFromSQL(db *sql.DB, logger *slog.Logger) *DB {
	return sqlsink.NewDBFromSQL(sqlsink.TDEngineDialect(), db, logger)
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
