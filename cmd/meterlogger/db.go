package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

const (
	sinkNameQuestDB     = "questdb"
	sinkNameStdout      = "stdout"
	sinkNamePostgres    = "postgres"
	sinkNameMySQL       = "mysql"
	sinkNameTimescaleDB = "timescaledb"
	sinkNameClickHouse  = "clickhouse"
	sinkNameTDEngine    = "tdengine"
)

// namedCloser pairs a sink name with its close func, so a partial-init failure
// can unwind every connection opened so far without leaking any of them.
type namedCloser struct {
	name  string
	close func() error
}

// dbConnections holds the shared connection for every enabled SQL sink, nil
// for any sink that isn't enabled.
type dbConnections struct {
	postgres    *postgres.DB
	mysql       *mysql.DB
	timescaledb *timescaledb.DB
	clickhouse  *clickhouse.DB
	tdengine    *tdengine.DB
}

// sql returns every open connection that shares the sqlsink implementation
// (all SQL dialects except ClickHouse), in initialization order. The four
// wrapper packages alias sqlsink.DB, so one slice covers them all.
func (d dbConnections) sql() []*sqlsink.DB {
	var out []*sqlsink.DB
	for _, db := range []*sqlsink.DB{d.postgres, d.mysql, d.timescaledb, d.tdengine} {
		if db != nil {
			out = append(out, db)
		}
	}
	return out
}

// closers returns a closer per open connection, in initialization order.
func (d dbConnections) closers() []namedCloser {
	var out []namedCloser
	if d.postgres != nil {
		out = append(out, namedCloser{sinkNamePostgres, d.postgres.Close})
	}
	if d.mysql != nil {
		out = append(out, namedCloser{sinkNameMySQL, d.mysql.Close})
	}
	if d.timescaledb != nil {
		out = append(out, namedCloser{sinkNameTimescaleDB, d.timescaledb.Close})
	}
	if d.clickhouse != nil {
		out = append(out, namedCloser{sinkNameClickHouse, d.clickhouse.Close})
	}
	if d.tdengine != nil {
		out = append(out, namedCloser{sinkNameTDEngine, d.tdengine.Close})
	}
	return out
}

// checkers returns a health checker per open connection.
func (d dbConnections) checkers() []healthserver.Checker {
	var out []healthserver.Checker
	if d.postgres != nil {
		out = append(out, d.postgres)
	}
	if d.mysql != nil {
		out = append(out, d.mysql)
	}
	if d.timescaledb != nil {
		out = append(out, d.timescaledb)
	}
	if d.clickhouse != nil {
		out = append(out, d.clickhouse)
	}
	if d.tdengine != nil {
		out = append(out, d.tdengine)
	}
	return out
}

// initDBs creates shared database connections for all enabled SQL sinks.
// Callers must close them on shutdown via closers().
func initDBs(ctx context.Context) dbConnections {
	var dbs dbConnections
	var opened []namedCloser

	if config.Postgres.Enabled {
		dbs.postgres, opened = connect(ctx, sinkNamePostgres, opened, func() (*postgres.DB, error) {
			return postgres.New(ctx, postgres.Config{
				Host:     config.Postgres.Host,
				Port:     config.Postgres.Port,
				User:     config.Postgres.User,
				Password: config.Postgres.Password,
				Database: config.Postgres.Database,
				SSLMode:  config.Postgres.SSLMode,
			}, logger)
		})
	}
	if config.MySQL.Enabled {
		dbs.mysql, opened = connect(ctx, sinkNameMySQL, opened, func() (*mysql.DB, error) {
			return mysql.New(ctx, mysql.Config{
				Host:     config.MySQL.Host,
				Port:     config.MySQL.Port,
				User:     config.MySQL.User,
				Password: config.MySQL.Password,
				Database: config.MySQL.Database,
			}, logger)
		})
	}
	if config.TimescaleDB.Enabled {
		dbs.timescaledb, opened = connect(ctx, sinkNameTimescaleDB, opened, func() (*timescaledb.DB, error) {
			return timescaledb.New(ctx, timescaledb.Config{
				Host:     config.TimescaleDB.Host,
				Port:     config.TimescaleDB.Port,
				User:     config.TimescaleDB.User,
				Password: config.TimescaleDB.Password,
				Database: config.TimescaleDB.Database,
				SSLMode:  config.TimescaleDB.SSLMode,
			}, logger)
		})
	}
	if config.ClickHouse.Enabled {
		dbs.clickhouse, opened = connect(ctx, sinkNameClickHouse, opened, func() (*clickhouse.DB, error) {
			return clickhouse.New(ctx, clickhouse.Config{
				Host:     config.ClickHouse.Host,
				Port:     config.ClickHouse.Port,
				User:     config.ClickHouse.User,
				Password: config.ClickHouse.Password,
				Database: config.ClickHouse.Database,
			}, logger)
		})
	}
	if config.TDEngine.Enabled {
		dbs.tdengine, _ = connect(ctx, sinkNameTDEngine, opened, func() (*tdengine.DB, error) {
			return tdengine.New(ctx, tdengine.Config{
				Host:     config.TDEngine.Host,
				Port:     config.TDEngine.Port,
				User:     config.TDEngine.User,
				Password: config.TDEngine.Password,
				Database: config.TDEngine.Database,
			}, logger)
		})
	}

	return dbs
}

// connect opens one sink connection. On failure it closes everything opened so
// far and exits; a sink that is enabled in config must be reachable at boot.
//
//nolint:ireturn // returns the concrete *X.DB the type parameter is instantiated with
func connect[D io.Closer](
	ctx context.Context,
	name string,
	opened []namedCloser,
	open func() (D, error),
) (D, []namedCloser) {
	db, err := open()
	if err != nil {
		logger.ErrorContext(ctx, "failed to connect to "+name, slog.Any("error", err))
		closeAll(opened)
		osExit(1)
	}
	return db, append(opened, namedCloser{name, db.Close})
}

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
