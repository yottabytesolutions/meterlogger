//go:build integration

package integration

import (
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mysql"
)

// TestMySQLSink connects to a real MySQL server, migrates, stores one record
// per repository, flushes and closes. The second round re-runs the
// constructors to prove the migration ledger is idempotent.
func TestMySQLSink(t *testing.T) {
	p := paramsFromEnv(t, "MYSQL", 3306, "meter", "meterpass")
	cfg := mysql.Config{
		Host:     p.Host,
		Port:     p.Port,
		User:     p.User,
		Password: p.Password,
		Database: p.Database,
	}
	ctors := sqlSinkCtors{
		heat:  mysql.NewHeatStore,
		grid:  mysql.NewGridStore,
		solar: mysql.NewSolarStore,
		duco:  mysql.NewDucoStore,
	}
	for _, round := range migrationRounds() {
		t.Run(round, func(t *testing.T) {
			ctx := t.Context()
			db, err := mysql.New(ctx, cfg, testLogger())
			if err != nil {
				t.Fatalf("mysql.New: %v", err)
			}
			defer closeDB(t, db)
			storeFlushClose(ctx, t, newSQLStores(ctx, t, db, ctors))
		})
	}
}
