package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
)

// namedCloser pairs a sink name with its close func, so a partial-init failure can unwind
// every connection opened so far without leaking any of them.
type namedCloser struct {
	name  string
	close func() error
}

// initDBs creates shared database connections for all enabled SQL sinks based on config.
// Returns the connections; callers must defer Close() on each non-nil connection.
func initDBs(ctx context.Context) (*postgres.DB, *mysql.DB, *timescaledb.DB, *clickhouse.DB, *tdengine.DB) {
	var pg *postgres.DB
	var my *mysql.DB
	var ts *timescaledb.DB
	var ch *clickhouse.DB
	var td *tdengine.DB
	var opened []namedCloser

	if config.Postgres.Enabled {
		pg, opened = connectPostgres(ctx, opened)
	}
	if config.MySQL.Enabled {
		my, opened = connectMySQL(ctx, opened)
	}
	if config.TimescaleDB.Enabled {
		ts, opened = connectTimescaleDB(ctx, opened)
	}
	if config.ClickHouse.Enabled {
		ch, opened = connectClickHouse(ctx, opened)
	}
	if config.TDEngine.Enabled {
		td, _ = connectTDEngine(ctx, opened)
	}

	return pg, my, ts, ch, td
}

func connectPostgres(ctx context.Context, opened []namedCloser) (*postgres.DB, []namedCloser) {
	db, err := postgres.New(
		ctx,
		config.Postgres.Host,
		config.Postgres.Port,
		config.Postgres.User,
		config.Postgres.Password,
		config.Postgres.Database,
		config.Postgres.SSLMode,
		logger,
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to PostgreSQL", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{"postgres", db.Close})
}

func connectMySQL(ctx context.Context, opened []namedCloser) (*mysql.DB, []namedCloser) {
	db, err := mysql.New(
		ctx,
		config.MySQL.Host,
		config.MySQL.Port,
		config.MySQL.User,
		config.MySQL.Password,
		config.MySQL.Database,
		logger,
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to MySQL", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{"mysql", db.Close})
}

func connectTimescaleDB(ctx context.Context, opened []namedCloser) (*timescaledb.DB, []namedCloser) {
	db, err := timescaledb.New(
		ctx,
		config.TimescaleDB.Host,
		config.TimescaleDB.Port,
		config.TimescaleDB.User,
		config.TimescaleDB.Password,
		config.TimescaleDB.Database,
		config.TimescaleDB.SSLMode,
		logger,
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to TimescaleDB", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{"timescaledb", db.Close})
}

func connectClickHouse(ctx context.Context, opened []namedCloser) (*clickhouse.DB, []namedCloser) {
	db, err := clickhouse.New(
		ctx,
		config.ClickHouse.Host,
		config.ClickHouse.Port,
		config.ClickHouse.User,
		config.ClickHouse.Password,
		config.ClickHouse.Database,
		logger,
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to ClickHouse", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{"clickhouse", db.Close})
}

func connectTDEngine(ctx context.Context, opened []namedCloser) (*tdengine.DB, []namedCloser) {
	db, err := tdengine.New(
		ctx,
		config.TDEngine.Host,
		config.TDEngine.Port,
		config.TDEngine.User,
		config.TDEngine.Password,
		config.TDEngine.Database,
		logger,
	)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to TDEngine", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{"tdengine", db.Close})
}

// closeAll closes every connection opened so far, in order, ahead of a fatal exit so a
// later sink's connect failure doesn't leak earlier ones.
func closeAll(opened []namedCloser) {
	for _, o := range opened {
		closeDB(o.name, o.close)
	}
}

func closeDB(name string, closeFn func() error) {
	if err := closeFn(); err != nil {
		logger.Error("close error", slog.String("db", name), slog.Any("error", err))
	}
}
