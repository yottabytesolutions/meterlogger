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

const (
	sinkNamePostgres    = "postgres"
	sinkNameMySQL       = "mysql"
	sinkNameTimescaleDB = "timescaledb"
	sinkNameClickHouse  = "clickhouse"
	sinkNameTDEngine    = "tdengine"
)

// namedCloser pairs a sink name with its close func, so a partial-init failure can unwind
// every connection opened so far without leaking any of them.
type namedCloser struct {
	name  string
	close func() error
}

// dbConnections holds the shared connection for every enabled SQL sink, nil
// for any sink that isn't enabled. Passed as a single value throughout
// cmd/meterlogger instead of five positional pointers, so a call site can't
// silently swap two same-shaped arguments.
type dbConnections struct {
	postgres    *postgres.DB
	mysql       *mysql.DB
	timescaledb *timescaledb.DB
	clickhouse  *clickhouse.DB
	tdengine    *tdengine.DB
}

// initDBs creates shared database connections for all enabled SQL sinks based on config.
// Returns the connections; callers must defer Close() on each non-nil connection.
func initDBs(ctx context.Context) dbConnections {
	var dbs dbConnections
	var opened []namedCloser

	if config.Postgres.Enabled {
		dbs.postgres, opened = connectPostgres(ctx, opened)
	}
	if config.MySQL.Enabled {
		dbs.mysql, opened = connectMySQL(ctx, opened)
	}
	if config.TimescaleDB.Enabled {
		dbs.timescaledb, opened = connectTimescaleDB(ctx, opened)
	}
	if config.ClickHouse.Enabled {
		dbs.clickhouse, opened = connectClickHouse(ctx, opened)
	}
	if config.TDEngine.Enabled {
		dbs.tdengine, _ = connectTDEngine(ctx, opened)
	}

	return dbs
}

func connectPostgres(ctx context.Context, opened []namedCloser) (*postgres.DB, []namedCloser) {
	db, err := postgres.New(ctx, postgres.Config{
		Host:     config.Postgres.Host,
		Port:     config.Postgres.Port,
		User:     config.Postgres.User,
		Password: config.Postgres.Password,
		Database: config.Postgres.Database,
		SSLMode:  config.Postgres.SSLMode,
	}, logger)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to PostgreSQL", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{sinkNamePostgres, db.Close})
}

func connectMySQL(ctx context.Context, opened []namedCloser) (*mysql.DB, []namedCloser) {
	db, err := mysql.New(ctx, mysql.Config{
		Host:     config.MySQL.Host,
		Port:     config.MySQL.Port,
		User:     config.MySQL.User,
		Password: config.MySQL.Password,
		Database: config.MySQL.Database,
	}, logger)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to MySQL", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{sinkNameMySQL, db.Close})
}

func connectTimescaleDB(ctx context.Context, opened []namedCloser) (*timescaledb.DB, []namedCloser) {
	db, err := timescaledb.New(ctx, timescaledb.Config{
		Host:     config.TimescaleDB.Host,
		Port:     config.TimescaleDB.Port,
		User:     config.TimescaleDB.User,
		Password: config.TimescaleDB.Password,
		Database: config.TimescaleDB.Database,
		SSLMode:  config.TimescaleDB.SSLMode,
	}, logger)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to TimescaleDB", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{sinkNameTimescaleDB, db.Close})
}

func connectClickHouse(ctx context.Context, opened []namedCloser) (*clickhouse.DB, []namedCloser) {
	db, err := clickhouse.New(ctx, clickhouse.Config{
		Host:     config.ClickHouse.Host,
		Port:     config.ClickHouse.Port,
		User:     config.ClickHouse.User,
		Password: config.ClickHouse.Password,
		Database: config.ClickHouse.Database,
	}, logger)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to ClickHouse", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{sinkNameClickHouse, db.Close})
}

func connectTDEngine(ctx context.Context, opened []namedCloser) (*tdengine.DB, []namedCloser) {
	db, err := tdengine.New(ctx, tdengine.Config{
		Host:     config.TDEngine.Host,
		Port:     config.TDEngine.Port,
		User:     config.TDEngine.User,
		Password: config.TDEngine.Password,
		Database: config.TDEngine.Database,
	}, logger)
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to TDEngine", slog.Any("error", err))
		closeAll(opened)
		os.Exit(1)
	}
	return db, append(opened, namedCloser{sinkNameTDEngine, db.Close})
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
