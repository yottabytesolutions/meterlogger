package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/stdout"
	"github.com/yottabytesolutions/meterlogger/internal/config"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

// sinkInit describes one optional sink for one source: whether it is enabled
// and how to build its repository.
type sinkInit[R any] struct {
	name    string
	enabled bool
	build   func() (R, error)
}

// buildSourceSinks assembles every enabled sink for one source. The
// constructor parameters are what actually differs per source; the set of
// sinks and their enablement rules live here once. All shared SQL dialects
// (postgres, mysql, timescaledb, tdengine) are covered by the single
// newSQLStore constructor via the dialect-agnostic sqlsink.DB.
func buildSourceSinks[R any](
	ctx context.Context, l *slog.Logger,
	healthSrv *healthserver.Server,
	dbs dbConnections,
	measurement string,
	newQuestDBWriter func(client *qdb.DBClient, measurement string, l *slog.Logger) R,
	newSQLStore func(ctx context.Context, db *sqlsink.DB, measurement string, l *slog.Logger) (R, error),
	newClickHouseStore func(ctx context.Context, db *clickhouse.DB, measurement string, l *slog.Logger) (R, error),
) []R {
	inits := []sinkInit[R]{
		{config.SinkQuestDB, cfg.QuestDB.Enabled, func() (R, error) {
			client, err := newQuestDBClient(ctx, l, healthSrv)
			if err != nil {
				var zero R
				return zero, err
			}
			return newQuestDBWriter(client, measurement, l), nil
		}},
		{config.SinkStdout, cfg.Stdout.Enabled, func() (R, error) {
			// The stdout debug sink implements every repository interface;
			// the assertion is covered by TestBuildSourceSinks_StdoutOnly.
			sink, ok := any(stdout.NewStdoutStore(l)).(R)
			if !ok {
				var zero R
				return zero, fmt.Errorf("stdout sink does not implement %T", zero)
			}
			return sink, nil
		}},
	}
	for _, db := range dbs.sql() {
		inits = append(inits, sinkInit[R]{db.Name(), true, func() (R, error) {
			return newSQLStore(ctx, db, measurement, l)
		}})
	}
	inits = append(inits, sinkInit[R]{config.SinkClickHouse, dbs.clickhouse != nil, func() (R, error) {
		return newClickHouseStore(ctx, dbs.clickhouse, measurement, l)
	}})
	return buildSinks(ctx, l, inits)
}

// buildSinks assembles the enabled sinks for one source. Any constructor
// failure is fatal: a sink that is enabled in config must work.
func buildSinks[R any](ctx context.Context, l *slog.Logger, inits []sinkInit[R]) []R {
	var sinks []R
	for _, in := range inits {
		if !in.enabled {
			continue
		}
		sink, err := in.build()
		if err != nil {
			l.ErrorContext(ctx, in.name+" sink init failed", slog.Any("error", err))
			osExit(1)
		}
		sinks = append(sinks, sink)
	}
	return sinks
}

// newQuestDBClient creates a QuestDB ILP client and registers it with the
// health server. Each source gets its own client: the sender is not safe for
// concurrent use and sources run in separate goroutines.
func newQuestDBClient(
	ctx context.Context,
	l *slog.Logger,
	healthSrv *healthserver.Server,
) (*qdb.DBClient, error) {
	client, err := qdb.NewDBClient(ctx, qdb.Config{
		Host:     cfg.QuestDB.Host,
		Port:     cfg.QuestDB.Port,
		User:     cfg.QuestDB.User,
		Password: cfg.QuestDB.Password,
	}, l)
	if err != nil {
		return nil, err
	}
	if healthSrv != nil {
		healthSrv.Register(client)
	}
	return client, nil
}

// osExit is a test seam for fatal sink failures.
//
//nolint:gochecknoglobals // swapped in tests to observe fatal paths
var osExit = os.Exit
