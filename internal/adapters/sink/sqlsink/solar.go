package sqlsink

import (
	"context"
	"log/slog"
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// SolarStore implements domain.EnvoySolarRepository.
type SolarStore struct {
	db             *DB
	table          string
	insert         string
	insertInverter string
	logger         *slog.Logger
}

// NewSolarStore creates and migrates a SolarStore. Version 1 creates the solar
// and inverter tables; version 2 adds the per-panel device_data columns to the
// inverter table.
func NewSolarStore(ctx context.Context, db *DB, table string, logger *slog.Logger) (*SolarStore, error) {
	tables := []migrationTable{
		{name: table, columns: solarColumns()},
		{name: table + "_inverters", columns: solarInverterColumnsV1()},
	}
	deviceCols := addColumnsMigration(
		db.dialect, db.db, solarInverterVersion,
		"add inverter device_data columns", table+"_inverters", solarInverterDeviceColumns(),
	)
	if err := migrate(ctx, db, "solar", table, "create solar tables", tables, logger, deviceCols); err != nil {
		return nil, err
	}
	return &SolarStore{
		db:             db,
		table:          table,
		insert:         insertSQL(db.dialect, table, solarColumns()),
		insertInverter: insertSQL(db.dialect, table+"_inverters", solarInverterColumns()),
		logger:         logger,
	}, nil
}

// StoreEnvoySolarData inserts solar data and inverter details.
func (s *SolarStore) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insert,
		d.ReadingTime, d.EnvoySerial, d.ProductionWh, d.Watt, d.PanelCount,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": store solar failed",
			slog.String("table", s.table), slog.Any("error", err))
		return err
	}

	for _, inv := range d.Inverters {
		_, invErr := s.db.db.ExecContext(
			ctx, s.insertInverter,
			inv.ReportTime, d.EnvoySerial, inv.SerialNumber, inv.Chaneid,
			inv.Operating, inv.Communicating, inv.Producing,
			inv.Phase, inv.LastReportedWatts, inv.MaxReportWatts,
			strings.Join(inv.DeviceStatus, ","),
			inv.DCVoltage, inv.DCCurrent, inv.ACVoltage, inv.ACCurrent, inv.ACFrequency,
			inv.TemperatureC, inv.LeadingVArs, inv.LaggingVArs,
			inv.WhToday, inv.WhYesterday, inv.WhWeek, inv.WhLifetime,
			inv.RSSI, inv.ISSI,
		)
		if invErr != nil {
			s.logger.ErrorContext(ctx, s.db.dialect.name+": store inverter failed", slog.Any("error", invErr))
			// Continue storing other inverters; report last error.
			err = invErr
		}
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *SolarStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *SolarStore) Close() error { return nil }
