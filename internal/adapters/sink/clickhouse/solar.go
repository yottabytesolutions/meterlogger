package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// SolarStore implements domain.EnvoySolarRepository for ClickHouse.
type SolarStore struct {
	db     *DB
	table  string
	logger *slog.Logger
	main   batchBuffer[domain.EnvoySolarData]
	invs   batchBuffer[solarInverterRow]
}

// solarInverterRow pairs one inverter reading with its envoy serial for the
// inverters table.
type solarInverterRow struct {
	envoySerial string
	inv         domain.InverterDetails
}

// NewSolarStore creates and migrates a SolarStore.
func NewSolarStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*SolarStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_solar_"+table, solarMigrations(db.db, table)); err != nil {
		return nil, fmt.Errorf("clickhouse solar migration: %w", err)
	}
	return &SolarStore{db: db, table: table, logger: logger}, nil
}

// StoreEnvoySolarData buffers solar data for ClickHouse.
func (s *SolarStore) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	warnDropped(ctx, s.logger, s.table, s.main.add(d))
	for _, inv := range d.Inverters {
		warnDropped(ctx, s.logger, s.table+"_inverters", s.invs.add(solarInverterRow{d.EnvoySerial, inv}))
	}
	return nil
}

// Flush inserts the buffered solar and inverter batches, one transaction per
// table (the driver allows a single prepared batch per transaction). Failed
// batches are re-queued for the next flush.
func (s *SolarStore) Flush(ctx context.Context) error {
	return errors.Join(
		flushBatch(ctx, s.db.db, s.logger, s.table, &s.main, s.insertMainRows),
		flushBatch(ctx, s.db.db, s.logger, s.table+"_inverters", &s.invs, s.insertInverterRows),
	)
}

// Table names come from config, not user input.
func (s *SolarStore) insertMainRows(ctx context.Context, tx *sql.Tx, rows []domain.EnvoySolarData) error {
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s (ts, envoy_serial, production_wh, watt, panel_count) VALUES (?,?,?,?,?)`,
			s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare solar insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, d := range rows {
		_, execErr := stmt.ExecContext(ctx, d.ReadingTime, d.EnvoySerial, d.ProductionWh, d.Watt, d.PanelCount)
		if execErr != nil {
			return fmt.Errorf("exec solar batch insert: %w", execErr)
		}
	}
	return nil
}

func (s *SolarStore) insertInverterRows(ctx context.Context, tx *sql.Tx, rows []solarInverterRow) error {
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s_inverters
        (ts, envoy_serial, inverter_serial, channel_id, operating, communicating,
         producing, phase, watts, peak_watts, status,
         dc_voltage, dc_current, ac_voltage, ac_current, ac_frequency,
         temperature_c, leading_vars, lagging_vars,
         wh_today, wh_yesterday, wh_week, wh_lifetime, rssi, issi)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.table,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare inverter insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		inv := r.inv
		_, execErr := stmt.ExecContext(
			ctx,
			inv.ReportTime, r.envoySerial, inv.SerialNumber, inv.Chaneid,
			inv.Operating, inv.Communicating, inv.Producing,
			inv.Phase, inv.LastReportedWatts, inv.MaxReportWatts,
			strings.Join(inv.DeviceStatus, ","),
			inv.DCVoltage, inv.DCCurrent, inv.ACVoltage, inv.ACCurrent, inv.ACFrequency,
			inv.TemperatureC, inv.LeadingVArs, inv.LaggingVArs,
			inv.WhToday, inv.WhYesterday, inv.WhWeek, inv.WhLifetime,
			inv.RSSI, inv.ISSI,
		)
		if execErr != nil {
			return fmt.Errorf("exec inverter batch insert: %w", execErr)
		}
	}
	return nil
}

// Close flushes pending rows with a bounded timeout. The shared DB is closed
// via DB.Close().
func (s *SolarStore) Close() error { return closeWithFinalFlush(s.Flush) }
