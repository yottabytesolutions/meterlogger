//go:build integration

package integration

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/sqlsink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// migrationRounds names the two constructor passes; the second pass proves
// the migration ledger is idempotent.
func migrationRounds() []string {
	return []string{"first-migration", "second-migration"}
}

// dbParams holds the connection parameters read from the environment.
type dbParams struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// paramsFromEnv reads <prefix>_HOST etc. and skips the test when the host
// is unset. See doc.go for the full contract.
func paramsFromEnv(t *testing.T, prefix string, defaultPort int, defaultUser, defaultPassword string) dbParams {
	t.Helper()
	host := os.Getenv(prefix + "_HOST")
	if host == "" {
		t.Skipf("%s_HOST not set, skipping", prefix)
	}
	return dbParams{
		Host:     host,
		Port:     envInt(t, prefix+"_PORT", defaultPort),
		User:     envOr(prefix+"_USER", defaultUser),
		Password: envOr(prefix+"_PASSWORD", defaultPassword),
		Database: envOr(prefix+"_DB", "meterlogger"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("%s=%q is not an integer: %v", key, v, err)
	}
	return n
}

// closeDB closes a database handle and fails the test on error. Meant for
// use in defer, where the error would otherwise be lost.
func closeDB(t *testing.T, db interface{ Close() error }) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Errorf("close database: %v", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// sqlStoreCtor matches the store constructors of the postgres, timescaledb
// and mysql wrapper packages, whose store types all alias sqlsink.
type sqlStoreCtor[S any] func(context.Context, *sqlsink.DB, string, *slog.Logger) (*S, error)

// sqlSinkCtors bundles the four wrapper constructors of one SQL sink package.
type sqlSinkCtors struct {
	heat  sqlStoreCtor[sqlsink.HeatStore]
	grid  sqlStoreCtor[sqlsink.GridStore]
	solar sqlStoreCtor[sqlsink.SolarStore]
	duco  sqlStoreCtor[sqlsink.DucoStore]
}

// newSQLStores runs the wrapper constructors, which migrate the schema, and
// returns the stores behind their domain interfaces.
func newSQLStores(ctx context.Context, t *testing.T, db *sqlsink.DB, c sqlSinkCtors) sinkStores {
	t.Helper()
	heat, err := c.heat(ctx, db, "heat", testLogger())
	if err != nil {
		t.Fatalf("NewHeatStore: %v", err)
	}
	grid, err := c.grid(ctx, db, "grid", testLogger())
	if err != nil {
		t.Fatalf("NewGridStore: %v", err)
	}
	solar, err := c.solar(ctx, db, "solar", testLogger())
	if err != nil {
		t.Fatalf("NewSolarStore: %v", err)
	}
	duco, err := c.duco(ctx, db, "duco", testLogger())
	if err != nil {
		t.Fatalf("NewDucoStore: %v", err)
	}
	return sinkStores{heat: heat, grid: grid, solar: solar, duco: duco}
}

// sinkStores bundles one repository of each kind, expressed as domain
// interfaces so SQL and ClickHouse sinks share the same exercise path.
type sinkStores struct {
	heat  domain.HeatMeterRepository
	grid  domain.GridTelegramRepository
	solar domain.EnvoySolarRepository
	duco  domain.DucoRepository
}

// storeFlushClose writes one representative record to every store, flushes,
// and closes. Any error fails the test.
func storeFlushClose(ctx context.Context, t *testing.T, s sinkStores) {
	t.Helper()
	if err := s.heat.StoreHeatTelegram(ctx, heatTelegram()); err != nil {
		t.Fatalf("StoreHeatTelegram: %v", err)
	}
	if err := s.grid.StoreGridTelegram(ctx, gridTelegram()); err != nil {
		t.Fatalf("StoreGridTelegram: %v", err)
	}
	if err := s.solar.StoreEnvoySolarData(ctx, solarData()); err != nil {
		t.Fatalf("StoreEnvoySolarData: %v", err)
	}
	if err := s.duco.StoreBoxStatus(ctx, ducoBoxStatus()); err != nil {
		t.Fatalf("StoreBoxStatus: %v", err)
	}
	for _, node := range ducoNodes() {
		if err := s.duco.StoreNodeData(ctx, node); err != nil {
			t.Fatalf("StoreNodeData %s: %v", node.NodeDevType(), err)
		}
	}
	for name, repo := range map[string]interface {
		Flush(context.Context) error
		Close() error
	}{"heat": s.heat, "grid": s.grid, "solar": s.solar, "duco": s.duco} {
		if err := repo.Flush(ctx); err != nil {
			t.Fatalf("Flush %s: %v", name, err)
		}
		if err := repo.Close(); err != nil {
			t.Fatalf("Close %s: %v", name, err)
		}
	}
}

// Fixture values mirror the sqlsink unit tests so both suites exercise the
// same shape of data.

func heatTelegram() domain.HeatTelegram {
	return domain.HeatTelegram{
		Timestamp:      time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		MeterID:        "m1",
		SerialNo:       "s1",
		Joules:         2_000_000_000,
		VolumeCm3:      9.5,
		SecondsCounter: 3600,
		Tforward:       70.5,
		Treturn:        40.25,
		Tdiff:          30.25,
		ActualPower:    1200,
		ActualFlow:     1.2,
		MaxPower:       5000,
		MaxFlow:        1.5,
	}
}

func gridTelegram() domain.GridTelegram {
	return domain.GridTelegram{
		Time: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), MeterMerkType: "ISK", Serienummer: "sn1",
		UsageCounter1: 1.1, UsageCounter2: 2.2, OutputCounter1: 3.3, OutputCounter2: 4.4,
		TotalPowerUsage: 500, TotalPowerOutput: 600,
		BrownoutsP1: 1, BrownoutsP2: 2, BrownoutsP3: 3,
		SpikesP1: 4, SpikesP2: 5, SpikesP3: 6,
		VoltageP1: 230.1, VoltageP2: 231.2, VoltageP3: 232.3,
		CurrentP1: 7, CurrentP2: 8, CurrentP3: 9,
		PowerUsageP1: 10, PowerUsageP2: 11, PowerUsageP3: 12,
		PowerOutputP1: 13, PowerOutputP2: 14, PowerOutputP3: 15,
	}
}

func solarData() domain.EnvoySolarData {
	ts := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return domain.EnvoySolarData{
		ReadingTime: ts, ProductionWh: 123.4, Watt: 567.8, PanelCount: 10, EnvoySerial: "e1",
		Inverters: []domain.InverterDetails{{
			SerialNumber: "inv1", Chaneid: 3, Producing: true, Operating: true,
			Phase: "L1", Communicating: false, ReportTime: ts.Add(time.Minute),
			LastReportedWatts: 250, MaxReportWatts: 300,
		}},
	}
}

func ducoBoxStatus() domain.DucoBoxStatus {
	return domain.DucoBoxStatus{
		EnergyFan: domain.EnergyFan{
			ExhaustFanSpeed: 1200, SupplyFanSpeed: 1100,
			ExhaustFanPwmPercentage: 45, SupplyFanPwmPercentage: 40,
		},
		EnergyInfo: domain.EnergyInfo{
			BypassStatus: 1, FilterRemainingTime: 90, FrostProtState: true,
			TempEHA: 18, TempETA: 20, TempODA: 5, TempSUP: 17,
		},
		General:        domain.General{InstallerState: "ok", RFHomeID: "rf1"},
		WeatherStation: domain.WeatherStation{Present: true},
	}
}

func ducoNodes() []domain.DucoNodeStatus {
	base := domain.BaseDucoNodeStatus{
		Node: 2, DevType: "SENS", Netw: "rf", Location: "hall", State: "auto",
		Cntdwn: 1, Mode: "AUTO", Ovrl: 3, Snsr: 4, Cerr: 5,
		Swversion: "1.2", Serialnb: "sn2", Show: 6, Link: 7,
	}
	return []domain.DucoNodeStatus{
		domain.DucoRFSensorStatus{
			BaseDucoNodeStatus: base,
			Temp:               21.5, Co2: 450.0, Rh: 55.5, RssiN2M: -60, HopVia: 1, RssiN2H: -70,
		},
		domain.DucoNodeBoxStatus{
			BaseDucoNodeStatus: base,
			Trgt:               50, Actl: 48, Rh: 51.0, Temp: 20.5, Co2: 400.0,
		},
		domain.DucoNodeBoxValveStatus{BaseDucoNodeStatus: base, Trgt: 30, Actl: 28},
	}
}
