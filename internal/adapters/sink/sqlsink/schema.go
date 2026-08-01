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
// applied migrations under these exact keys.
func migrate(
	ctx context.Context, db *DB, kind, table, description string,
	tables []migrationTable, logger *slog.Logger,
) error {
	d := db.dialect
	m := d.newMigrator(db.db, logger)
	component := d.name + "_" + kind + "_" + table
	if err := m.Migrate(ctx, component, createTablesMigration(d, db.db, description, tables)); err != nil {
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

func gridColumns() []column {
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
