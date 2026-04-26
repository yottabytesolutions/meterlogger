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

// initDBs creates shared database connections for all enabled SQL sinks based on config.
// Returns the connections; callers must defer Close() on each non-nil connection.
func initDBs(ctx context.Context) (*postgres.DB, *mysql.DB, *timescaledb.DB, *clickhouse.DB, *tdengine.DB) {
	var pg *postgres.DB
	var my *mysql.DB
	var ts *timescaledb.DB
	var ch *clickhouse.DB
	var td *tdengine.DB

	if config.Postgres.Enabled {
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
			os.Exit(1)
		}
		pg = db
	}

	if config.MySQL.Enabled {
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
			os.Exit(1)
		}
		my = db
	}

	if config.TimescaleDB.Enabled {
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
			os.Exit(1)
		}
		ts = db
	}

	if config.ClickHouse.Enabled {
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
			os.Exit(1)
		}
		ch = db
	}

	if config.TDEngine.Enabled {
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
			os.Exit(1)
		}
		td = db
	}

	return pg, my, ts, ch, td
}

func closeDB(name string, closeFn func() error) {
	if err := closeFn(); err != nil {
		logger.Error("close error", slog.String("db", name), slog.Any("error", err))
	}
}
