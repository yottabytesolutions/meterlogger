package sqlsink

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

type columnKind int

const (
	kindTimestamp columnKind = iota
	kindText
	kindShortText
	kindDouble
	kindBigInt
	kindInt
	kindBool
)

type column struct {
	name    string
	kind    columnKind
	notNull bool
}

const colSerialNo = "serial_no"

// gridDemandVersion is the grid schema version that adds the Belgian peak
// demand columns.
const gridDemandVersion = 2

// migrationTable pairs a concrete table name with its column definitions.
type migrationTable struct {
	name    string
	columns []column
}

func createTableSQL(d Dialect, table string, cols []column) string {
	defs := make([]string, len(cols))
	for i, c := range cols {
		def := d.quoteIdent(c.name) + " " + d.typeName(c.kind)
		if c.notNull && d.notNull {
			def += " NOT NULL"
		}
		defs[i] = def
	}
	// table name comes from config, not user HTTP input.
	return "CREATE TABLE IF NOT EXISTS " + table + " (\n    " + strings.Join(defs, ",\n    ") + "\n)"
}

func insertSQL(d Dialect, table string, cols []column) string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = d.quoteIdent(c.name)
	}
	return "INSERT INTO " + table + " (" + strings.Join(names, ", ") +
		") VALUES (" + d.placeholders(len(cols)) + ")"
}

func createTablesMigration(d Dialect, db *sql.DB, description string, tables []migrationTable) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: description,
			Up: func(ctx context.Context) error {
				for _, t := range tables {
					if _, err := db.ExecContext(ctx, createTableSQL(d, t.name, t.columns)); err != nil {
						return err
					}
					if d.postCreate != nil {
						if err := d.postCreate(ctx, db, t.name); err != nil {
							return err
						}
					}
				}
				return nil
			},
		},
	}
}

// migrate runs the version ledger for one store kind. The component key is
// "<dialect>_<kind>_<table>" and must not change: existing deployments track
// applied migrations under these exact keys. Extra migrations run after the
// initial table creation, ordered by version.
func migrate(
	ctx context.Context, db *DB, kind, table, description string,
	tables []migrationTable, logger *slog.Logger, extra ...schemastore.Migration,
) error {
	d := db.dialect
	m := d.newMigrator(db.db, logger)
	component := d.name + "_" + kind + "_" + table
	migrations := append(createTablesMigration(d, db.db, description, tables), extra...)
	if err := m.Migrate(ctx, component, migrations); err != nil {
		return fmt.Errorf("%s %s migration: %w", d.name, kind, err)
	}
	return nil
}

func heatColumns() []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "meter_id", kind: kindText, notNull: true},
		{name: colSerialNo, kind: kindText, notNull: true},
		{name: "power_w", kind: kindBigInt, notNull: true},
		{name: "energy_gj", kind: kindDouble, notNull: true},
		{name: "t_forward_c", kind: kindDouble, notNull: true},
		{name: "t_return_c", kind: kindDouble, notNull: true},
		{name: "t_diff_c", kind: kindDouble, notNull: true},
		{name: "volume_cm3", kind: kindDouble, notNull: true},
		{name: "seconds", kind: kindBigInt, notNull: true},
		{name: "max_flow", kind: kindDouble, notNull: true},
		{name: "max_power_w", kind: kindBigInt, notNull: true},
	}
}

// gridColumnsV1 is the grid table as created by migration version 1. It must
// not change: version 2 adds the peak demand columns on top of it.
func gridColumnsV1() []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "meter_type", kind: kindText},
		{name: colSerialNo, kind: kindText},
		{name: "usage_counter1", kind: kindDouble},
		{name: "usage_counter2", kind: kindDouble},
		{name: "output_counter1", kind: kindDouble},
		{name: "output_counter2", kind: kindDouble},
		{name: "total_power_usage", kind: kindBigInt},
		{name: "total_power_output", kind: kindBigInt},
		{name: "brownouts_p1", kind: kindBigInt},
		{name: "brownouts_p2", kind: kindBigInt},
		{name: "brownouts_p3", kind: kindBigInt},
		{name: "spikes_p1", kind: kindBigInt},
		{name: "spikes_p2", kind: kindBigInt},
		{name: "spikes_p3", kind: kindBigInt},
		{name: "voltage_p1", kind: kindDouble},
		{name: "voltage_p2", kind: kindDouble},
		{name: "voltage_p3", kind: kindDouble},
		{name: "current_p1", kind: kindBigInt},
		{name: "current_p2", kind: kindBigInt},
		{name: "current_p3", kind: kindBigInt},
		{name: "power_usage_p1", kind: kindBigInt},
		{name: "power_usage_p2", kind: kindBigInt},
		{name: "power_usage_p3", kind: kindBigInt},
		{name: "power_output_p1", kind: kindBigInt},
		{name: "power_output_p2", kind: kindBigInt},
		{name: "power_output_p3", kind: kindBigInt},
	}
}

// gridDemandColumns are the Belgian peak demand (capaciteitstarief) columns
// added by grid migration version 2. Nullable: rows from meters without these
// fields carry zeros for the demand values and NULL for the capture time.
func gridDemandColumns() []column {
	return []column{
		{name: "avg_demand", kind: kindBigInt},
		{name: "max_demand_month", kind: kindBigInt},
		{name: "max_demand_month_at", kind: kindTimestamp},
	}
}

func gridColumns() []column {
	return append(gridColumnsV1(), gridDemandColumns()...)
}

// addColumnsMigration builds a migration that adds cols to an existing table
// with one ALTER TABLE ADD COLUMN statement per column. Columns are added
// nullable on every dialect so existing rows stay valid.
func addColumnsMigration(
	d Dialect, db *sql.DB, version int, description, table string, cols []column,
) schemastore.Migration {
	return schemastore.Migration{
		Version:     version,
		Description: description,
		Up: func(ctx context.Context) error {
			for _, c := range cols {
				// The table name comes from config and the column names from the
				// static schema definitions, not from user input.
				//nolint:gosec // G202: no user input in this DDL
				stmt := "ALTER TABLE " + table + " ADD COLUMN " + d.quoteIdent(c.name) + " " + d.typeName(c.kind)
				if _, err := db.ExecContext(ctx, stmt); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// subdeviceColumns is the shared shape of the M-Bus subdevice tables (gas,
// water, thermal); only the reading column name differs.
func subdeviceColumns(readingCol string) []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "received_at", kind: kindTimestamp, notNull: true},
		{name: "channel", kind: kindInt, notNull: true},
		{name: "device_type", kind: kindInt, notNull: true},
		{name: colSerialNo, kind: kindShortText, notNull: true},
		{name: readingCol, kind: kindDouble, notNull: true},
	}
}

func gasColumns() []column { return subdeviceColumns("reading_m3") }

func waterColumns() []column { return subdeviceColumns("reading_m3") }

func thermalColumns() []column { return subdeviceColumns("reading_gj") }

func solarColumns() []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "envoy_serial", kind: kindText, notNull: true},
		{name: "production_wh", kind: kindDouble, notNull: true},
		{name: "watt", kind: kindDouble, notNull: true},
		{name: "panel_count", kind: kindInt, notNull: true},
	}
}

func solarInverterColumns() []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "envoy_serial", kind: kindText, notNull: true},
		{name: "inverter_serial", kind: kindText, notNull: true},
		{name: "channel_id", kind: kindInt},
		{name: "operating", kind: kindBool},
		{name: "communicating", kind: kindBool},
		{name: "producing", kind: kindBool},
		{name: "phase", kind: kindShortText},
		{name: "watts", kind: kindInt},
		{name: "peak_watts", kind: kindInt},
	}
}

func ducoBoxGeneralColumns() []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "rf_home_id", kind: kindText},
		{name: "exhaust_fan_speed", kind: kindInt},
		{name: "supply_fan_speed", kind: kindInt},
		{name: "exhaust_fan_pwm_percentage", kind: kindInt},
		{name: "supply_fan_pwm_percentage", kind: kindInt},
		{name: "bypass_status", kind: kindInt},
		{name: "filter_remaining_time", kind: kindInt},
		{name: "frost_prot_state", kind: kindBool},
		{name: "temp_eha", kind: kindInt},
		{name: "temp_eta", kind: kindInt},
		{name: "temp_oda", kind: kindInt},
		{name: "temp_sup", kind: kindInt},
		{name: "installer_state", kind: kindText},
		{name: "weather_station_present", kind: kindBool},
	}
}

func ducoNodeIdentityColumns() []column {
	return []column{
		{name: "ts", kind: kindTimestamp, notNull: true},
		{name: "node_id", kind: kindInt},
		{name: "location", kind: kindText},
		{name: "device", kind: kindText},
		{name: "connection_type", kind: kindText},
		{name: colSerialNo, kind: kindText},
		{name: "sw_version", kind: kindText},
		{name: "mode", kind: kindText},
		{name: "state", kind: kindText},
	}
}

func ducoDiagnosticColumns() []column {
	return []column{
		{name: "snsr", kind: kindInt},
		{name: "cerr", kind: kindInt},
		{name: "ovrl", kind: kindInt},
		{name: "cntdwn", kind: kindInt},
		{name: "show", kind: kindInt},
		{name: "link", kind: kindInt},
	}
}

func ducoNodeColumns() []column {
	cols := ducoNodeIdentityColumns()
	cols = append(cols,
		column{name: "co2", kind: kindDouble},
		column{name: "temp", kind: kindDouble},
		column{name: "humidity", kind: kindDouble},
		column{name: "rssi_direct", kind: kindInt},
		column{name: "rssi_with_hops", kind: kindInt},
		column{name: "hop_via", kind: kindInt},
	)
	return append(cols, ducoDiagnosticColumns()...)
}

func ducoBoxNodeColumns() []column {
	cols := ducoNodeIdentityColumns()
	cols = append(cols,
		column{name: "trgt", kind: kindInt},
		column{name: "actl", kind: kindInt},
		column{name: "co2", kind: kindDouble},
		column{name: "temp", kind: kindDouble},
		column{name: "humidity", kind: kindDouble},
	)
	return append(cols, ducoDiagnosticColumns()...)
}

func ducoValveColumns() []column {
	cols := ducoNodeIdentityColumns()
	cols = append(cols,
		column{name: "trgt", kind: kindInt},
		column{name: "actl", kind: kindInt},
	)
	return append(cols, ducoDiagnosticColumns()...)
}
