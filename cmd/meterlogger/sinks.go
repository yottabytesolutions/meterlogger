package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

// sinkInit describes one optional sink for one source: whether it is enabled
// and how to build its repository.
type sinkInit[R any] struct {
	name    string
	enabled bool
	build   func() (R, error)
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
		Host:     config.QuestDB.Host,
		Port:     config.QuestDB.Port,
		User:     config.QuestDB.User,
		Password: config.QuestDB.Password,
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
