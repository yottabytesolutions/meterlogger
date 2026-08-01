//go:build integration

package integration

import (
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/postgres"
)

// TestPostgresSink connects to a real PostgreSQL server, migrates, stores one
// record per repository, flushes and closes. The second round re-runs the
// constructors to prove the migration ledger is idempotent.
func TestPostgresSink(t *testing.T) {
	p := paramsFromEnv(t, "POSTGRES", 5432, "meter", "meterpass")
	cfg := postgres.Config{
		Host:     p.Host,
		Port:     p.Port,
		User:     p.User,
		Password: p.Password,
		Database: p.Database,
		SSLMode:  "disable",
	}
	ctors := sqlSinkCtors{
		heat:  postgres.NewHeatStore,
		grid:  postgres.NewGridStore,
		solar: postgres.NewSolarStore,
		duco:  postgres.NewDucoStore,
	}
	for _, round := range migrationRounds() {
		t.Run(round, func(t *testing.T) {
			ctx := t.Context()
			db, err := postgres.New(ctx, cfg, testLogger())
			if err != nil {
				t.Fatalf("postgres.New: %v", err)
			}
			defer closeDB(t, db)
			storeFlushClose(ctx, t, newSQLStores(ctx, t, db, ctors))
		})
	}
}
