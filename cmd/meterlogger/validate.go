package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/qdb"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/tdengine"
	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
	"github.com/yottabytesolutions/meterlogger/internal/config"
)

// sinkPingTimeout bounds the connect and health check of a single sink.
const sinkPingTimeout = 5 * time.Second

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var pingSinks bool

//nolint:gochecknoglobals // cobra CLI pattern requires package-level variables
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration without starting the logger",
	Long: "Validate the loaded configuration and exit 0 when it is usable, 1 otherwise. " +
		"Every problem is printed to stderr. With --ping, additionally connect to every " +
		"enabled sink, run its health check, and print one result line per sink.",
	Run: func(cmd *cobra.Command, _ []string) {
		os.Exit(runValidate(cmd.Context()))
	},
}

//nolint:gochecknoinits // init() is required by the cobra CLI pattern
func init() {
	validateCmd.Flags().BoolVar(
		&pingSinks,
		"ping",
		false,
		"Also connect to every enabled sink and run its health check",
	)
	rootCmd.AddCommand(validateCmd)
}

// runValidate checks the loaded configuration and, with --ping, connects to
// every enabled sink. It returns the process exit code.
func runValidate(ctx context.Context) int {
	errs := config.Validate(cfg, "")
	for _, msg := range errs {
		fmt.Fprintln(os.Stderr, msg)
	}
	if len(errs) > 0 {
		return 1
	}

	fmt.Fprintln(os.Stdout, "configuration valid")
	if !pingSinks {
		return 0
	}
	return runPings(ctx, os.Stdout, buildSinkPingers())
}

// sinkPinger connects to one sink and reports the result. The ping func owns
// the full connect, check, close cycle.
type sinkPinger struct {
	name string
	ping func(ctx context.Context) error
}

// runPings runs every pinger with a bounded per-sink timeout and prints one
// result line per sink. It returns 1 when any sink fails, 0 otherwise.
func runPings(ctx context.Context, out io.Writer, pingers []sinkPinger) int {
	exit := 0
	for _, p := range pingers {
		pingCtx, cancel := context.WithTimeout(ctx, sinkPingTimeout)
		err := p.ping(pingCtx)
		cancel()
		if err != nil {
			fmt.Fprintf(out, "%s: %v\n", p.name, err)
			exit = 1
			continue
		}
		fmt.Fprintf(out, "%s: ok\n", p.name)
	}
	return exit
}

// buildSinkPingers returns one pinger per enabled sink, in the same order the
// runtime opens them. The SQL constructors open the pool and ping; no store is
// created, so no schema migrations run.
func buildSinkPingers() []sinkPinger {
	var pingers []sinkPinger
	if cfg.QuestDB.Enabled {
		pingers = append(pingers, sinkPinger{config.SinkQuestDB, pingQuestDB})
	}
	if cfg.Postgres.Enabled {
		pingers = append(pingers, sinkPinger{config.SinkPostgres, func(ctx context.Context) error {
			return pingSQLSink(ctx, func() (*postgres.DB, error) {
				return postgres.New(ctx, postgres.Config{
					Host:     cfg.Postgres.Host,
					Port:     cfg.Postgres.Port,
					User:     cfg.Postgres.User,
					Password: cfg.Postgres.Password,
					Database: cfg.Postgres.Database,
					SSLMode:  cfg.Postgres.SSLMode,
				}, logger)
			})
		}})
	}
	if cfg.MySQL.Enabled {
		pingers = append(pingers, sinkPinger{config.SinkMySQL, func(ctx context.Context) error {
			return pingSQLSink(ctx, func() (*mysql.DB, error) {
				return mysql.New(ctx, mysql.Config{
					Host:     cfg.MySQL.Host,
					Port:     cfg.MySQL.Port,
					User:     cfg.MySQL.User,
					Password: cfg.MySQL.Password,
					Database: cfg.MySQL.Database,
				}, logger)
			})
		}})
	}
	if cfg.TimescaleDB.Enabled {
		pingers = append(pingers, sinkPinger{config.SinkTimescaleDB, func(ctx context.Context) error {
			return pingSQLSink(ctx, func() (*timescaledb.DB, error) {
				return timescaledb.New(ctx, timescaledb.Config{
					Host:     cfg.TimescaleDB.Host,
					Port:     cfg.TimescaleDB.Port,
					User:     cfg.TimescaleDB.User,
					Password: cfg.TimescaleDB.Password,
					Database: cfg.TimescaleDB.Database,
					SSLMode:  cfg.TimescaleDB.SSLMode,
				}, logger)
			})
		}})
	}
	if cfg.ClickHouse.Enabled {
		pingers = append(pingers, sinkPinger{config.SinkClickHouse, func(ctx context.Context) error {
			return pingSQLSink(ctx, func() (*clickhouse.DB, error) {
				return clickhouse.New(ctx, clickhouse.Config{
					Host:     cfg.ClickHouse.Host,
					Port:     cfg.ClickHouse.Port,
					User:     cfg.ClickHouse.User,
					Password: cfg.ClickHouse.Password,
					Database: cfg.ClickHouse.Database,
				}, logger)
			})
		}})
	}
	if cfg.TDEngine.Enabled {
		pingers = append(pingers, sinkPinger{config.SinkTDEngine, func(ctx context.Context) error {
			return pingSQLSink(ctx, func() (*tdengine.DB, error) {
				return tdengine.New(ctx, tdengine.Config{
					Host:     cfg.TDEngine.Host,
					Port:     cfg.TDEngine.Port,
					User:     cfg.TDEngine.User,
					Password: cfg.TDEngine.Password,
					Database: cfg.TDEngine.Database,
				}, logger)
			})
		}})
	}
	return pingers
}

// checkCloser is the surface every SQL-family sink connection exposes for a
// health probe.
type checkCloser interface {
	Check(ctx context.Context) error
	Close() error
}

// pingSQLSink opens a sink connection (which pings on open), runs its health
// check, and closes it again. Any error along the way is the ping result.
func pingSQLSink[D checkCloser](ctx context.Context, open func() (D, error)) error {
	db, err := open()
	if err != nil {
		return err
	}
	checkErr := db.Check(ctx)
	if closeErr := db.Close(); closeErr != nil && checkErr == nil {
		checkErr = closeErr
	}
	return checkErr
}

// pingQuestDB opens an ILP line sender to QuestDB and runs its health check.
// The connect itself is the real probe; Check reports the last flush outcome
// and is nil on a fresh client.
func pingQuestDB(ctx context.Context) error {
	client, err := qdb.NewDBClient(ctx, qdb.Config{
		Host:     cfg.QuestDB.Host,
		Port:     cfg.QuestDB.Port,
		User:     cfg.QuestDB.User,
		Password: cfg.QuestDB.Password,
	}, logger)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Check(ctx)
}
