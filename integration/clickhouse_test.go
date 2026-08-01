//go:build integration

package integration

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/clickhouse"
)

// TestClickHouseSink connects to a real ClickHouse server. ClickHouse buffers
// writes, so after store, Flush and Close the test verifies with a count
// query that the rows actually landed. The second round re-runs the
// constructors to prove the migration ledger is idempotent.
func TestClickHouseSink(t *testing.T) {
	p := paramsFromEnv(t, "CLICKHOUSE", 9000, "default", "")
	cfg := clickhouse.Config{
		Host:     p.Host,
		Port:     p.Port,
		User:     p.User,
		Password: p.Password,
		Database: p.Database,
	}
	for _, round := range migrationRounds() {
		t.Run(round, func(t *testing.T) {
			ctx := t.Context()
			db, err := clickhouse.New(ctx, cfg, testLogger())
			if err != nil {
				t.Fatalf("clickhouse.New: %v", err)
			}
			defer closeDB(t, db)
			storeFlushClose(ctx, t, newClickHouseStores(ctx, t, db))
		})
	}

	verifyClickHouseRows(t, p)
}

func newClickHouseStores(ctx context.Context, t *testing.T, db *clickhouse.DB) sinkStores {
	t.Helper()
	heat, err := clickhouse.NewHeatStore(ctx, db, "heat", testLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	grid, err := clickhouse.NewGridStore(ctx, db, "grid", testLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	solar, err := clickhouse.NewSolarStore(ctx, db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	duco, err := clickhouse.NewDucoStore(ctx, db, "duco", testLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	return sinkStores{heat: heat, grid: grid, solar: solar, duco: duco}
}

// verifyClickHouseRows opens a direct driver connection and checks that the
// batched inserts reached the tables. The test ran two rounds, so every
// table must hold at least two rows (counts can be higher when the database
// is reused across local runs).
func verifyClickHouseRows(t *testing.T, p dbParams) {
	t.Helper()
	dsn := url.URL{
		Scheme: "clickhouse",
		User:   url.UserPassword(p.User, p.Password),
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:   "/" + p.Database,
	}
	db, err := sql.Open("clickhouse", dsn.String())
	if err != nil {
		t.Fatalf("open verification connection: %v", err)
	}
	defer closeDB(t, db)

	ctx := t.Context()
	for _, table := range []string{
		"heat", "grid", "solar", "solar_inverters",
		"duco_box_general", "duco_node", "duco_box_node", "duco_valve",
	} {
		var count uint64
		// Table names come from the fixed list above, not user input.
		if scanErr := db.QueryRowContext(ctx, "SELECT count() FROM "+table).Scan(&count); scanErr != nil {
			t.Errorf("count %s: %v", table, scanErr)
			continue
		}
		if count < 2 {
			t.Errorf("table %s: got %d rows, want at least 2", table, count)
		}
	}
}
