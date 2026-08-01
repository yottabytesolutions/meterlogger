//go:build integration

package integration

import (
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/timescaledb"
)

// TestTimescaleDBSink is the PostgreSQL flow against a TimescaleDB server,
// which additionally turns the tables into hypertables. Two rounds prove
// migrations and hypertable creation are idempotent.
func TestTimescaleDBSink(t *testing.T) {
	p := paramsFromEnv(t, "TIMESCALEDB", 5432, "meter", "meterpass")
	cfg := timescaledb.Config{
		Host:     p.Host,
		Port:     p.Port,
		User:     p.User,
		Password: p.Password,
		Database: p.Database,
		SSLMode:  "disable",
	}
	ctors := sqlSinkCtors{
		heat:  timescaledb.NewHeatStore,
		grid:  timescaledb.NewGridStore,
		solar: timescaledb.NewSolarStore,
		duco:  timescaledb.NewDucoStore,
	}
	for _, round := range migrationRounds() {
		t.Run(round, func(t *testing.T) {
			ctx := t.Context()
			db, err := timescaledb.New(ctx, cfg, testLogger())
			if err != nil {
				t.Fatalf("timescaledb.New: %v", err)
			}
			defer closeDB(t, db)
			storeFlushClose(ctx, t, newSQLStores(ctx, t, db, ctors))
		})
	}
}
