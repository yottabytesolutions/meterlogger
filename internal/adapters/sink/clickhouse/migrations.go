package clickhouse

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

// Table names in the DDL below come from config, not user input.

// gridDemandVersion is the grid schema version that adds the Belgian peak
// demand columns.
const gridDemandVersion = 2

func heatMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create heat table",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts          DateTime64(9, 'UTC') NOT NULL,
    meter_id    String               NOT NULL,
    serial_no   String               NOT NULL,
    power_w     Int64                NOT NULL,
    energy_gj   Float64              NOT NULL,
    t_forward_c Float64              NOT NULL,
    t_return_c  Float64              NOT NULL,
    t_diff_c    Float64              NOT NULL,
    volume_cm3  Float64              NOT NULL,
    seconds     Int64                NOT NULL,
    max_flow    Float64              NOT NULL,
    max_power_w Int64                NOT NULL
) ENGINE = MergeTree() ORDER BY (ts, meter_id)`, table,
					),
				)
				return err
			},
		},
	}
}

func gridMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create grid table",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts                 DateTime64(9, 'UTC') NOT NULL,
    meter_type         String,
    serial_no          String,
    usage_counter1     Float64,
    usage_counter2     Float64,
    output_counter1    Float64,
    output_counter2    Float64,
    total_power_usage  Int64,
    total_power_output Int64,
    brownouts_p1       Int64,
    brownouts_p2       Int64,
    brownouts_p3       Int64,
    spikes_p1          Int64,
    spikes_p2          Int64,
    spikes_p3          Int64,
    voltage_p1         Float64,
    voltage_p2         Float64,
    voltage_p3         Float64,
    current_p1         Int64,
    current_p2         Int64,
    current_p3         Int64,
    power_usage_p1     Int64,
    power_usage_p2     Int64,
    power_usage_p3     Int64,
    power_output_p1    Int64,
    power_output_p2    Int64,
    power_output_p3    Int64
) ENGINE = MergeTree() ORDER BY (ts)`, table,
					),
				)
				return err
			},
		},
		{
			Version:     gridDemandVersion,
			Description: "add peak demand columns",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`ALTER TABLE %s
    ADD COLUMN avg_demand          Int64,
    ADD COLUMN max_demand_month    Int64,
    ADD COLUMN max_demand_month_at Nullable(DateTime64(9, 'UTC'))`, table,
					),
				)
				return err
			},
		},
	}
}

func gasMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create gas table",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts          DateTime64(9, 'UTC') NOT NULL,
    received_at DateTime64(9, 'UTC') NOT NULL,
    channel     Int32                NOT NULL,
    device_type Int32                NOT NULL,
    serial_no   String               NOT NULL,
    reading_m3  Float64              NOT NULL
) ENGINE = MergeTree() ORDER BY (ts)`, table,
					),
				)
				return err
			},
		},
	}
}

func waterMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create water table",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts          DateTime64(9, 'UTC') NOT NULL,
    received_at DateTime64(9, 'UTC') NOT NULL,
    channel     Int32                NOT NULL,
    device_type Int32                NOT NULL,
    serial_no   String               NOT NULL,
    reading_m3  Float64              NOT NULL
) ENGINE = MergeTree() ORDER BY (ts)`, table,
					),
				)
				return err
			},
		},
	}
}

func thermalMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create thermal table",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts          DateTime64(9, 'UTC') NOT NULL,
    received_at DateTime64(9, 'UTC') NOT NULL,
    channel     Int32                NOT NULL,
    device_type Int32                NOT NULL,
    serial_no   String               NOT NULL,
    reading_gj  Float64              NOT NULL
) ENGINE = MergeTree() ORDER BY (ts)`, table,
					),
				)
				return err
			},
		},
	}
}

func solarMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create solar tables",
			Up: func(ctx context.Context) error {
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts            DateTime64(9, 'UTC') NOT NULL,
    envoy_serial  String               NOT NULL,
    production_wh Float64              NOT NULL,
    watt          Float64              NOT NULL,
    panel_count   Int32                NOT NULL
) ENGINE = MergeTree() ORDER BY (ts, envoy_serial)`, table,
					),
				)
				if err != nil {
					return err
				}
				_, err = db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s_inverters (
    ts              DateTime64(9, 'UTC') NOT NULL,
    envoy_serial    String               NOT NULL,
    inverter_serial String               NOT NULL,
    channel_id      Int32,
    operating       Bool,
    communicating   Bool,
    producing       Bool,
    phase           String,
    watts           Int32,
    peak_watts      Int32
) ENGINE = MergeTree() ORDER BY (ts, inverter_serial)`, table,
					),
				)
				return err
			},
		},
	}
}

func ducoBoxGeneralTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_box_general (
    ts                         DateTime64(9, 'UTC') NOT NULL,
    rf_home_id                 String,
    exhaust_fan_speed          Int32,
    supply_fan_speed           Int32,
    exhaust_fan_pwm_percentage Int32,
    supply_fan_pwm_percentage  Int32,
    bypass_status              Int32,
    filter_remaining_time      Int32,
    frost_prot_state           Bool,
    temp_eha                   Int32,
    temp_eta                   Int32,
    temp_oda                   Int32,
    temp_sup                   Int32,
    installer_state            String,
    weather_station_present    Bool
) ENGINE = MergeTree() ORDER BY (ts)`, base,
	)
}

func ducoNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_node (
    ts              DateTime64(9, 'UTC') NOT NULL,
    node_id         Int32,
    location        String,
    device          String,
    connection_type String,
    serial_no       String,
    sw_version      String,
    mode            String,
    state           String,
    co2             Float64,
    temp            Float64,
    humidity        Float64,
    rssi_direct     Int32,
    rssi_with_hops  Int32,
    hop_via         Int32,
    snsr            Int32,
    cerr            Int32,
    ovrl            Int32,
    cntdwn          Int32,
    show            Int32,
    link            Int32
) ENGINE = MergeTree() ORDER BY (ts, node_id)`, base,
	)
}

func ducoBoxNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_box_node (
    ts              DateTime64(9, 'UTC') NOT NULL,
    node_id         Int32,
    location        String,
    device          String,
    connection_type String,
    serial_no       String,
    sw_version      String,
    mode            String,
    state           String,
    trgt            Int32,
    actl            Int32,
    co2             Float64,
    temp            Float64,
    humidity        Float64,
    snsr            Int32,
    cerr            Int32,
    ovrl            Int32,
    cntdwn          Int32,
    show            Int32,
    link            Int32
) ENGINE = MergeTree() ORDER BY (ts, node_id)`, base,
	)
}

func ducoValveTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_valve (
    ts              DateTime64(9, 'UTC') NOT NULL,
    node_id         Int32,
    location        String,
    device          String,
    connection_type String,
    serial_no       String,
    sw_version      String,
    mode            String,
    state           String,
    trgt            Int32,
    actl            Int32,
    snsr            Int32,
    cerr            Int32,
    ovrl            Int32,
    cntdwn          Int32,
    show            Int32,
    link            Int32
) ENGINE = MergeTree() ORDER BY (ts, node_id)`, base,
	)
}

func ducoMigrations(db *sql.DB, base string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create ventilation tables",
			Up: func(ctx context.Context) error {
				tables := []string{
					ducoBoxGeneralTableSQL(base),
					ducoNodeTableSQL(base),
					ducoBoxNodeTableSQL(base),
					ducoValveTableSQL(base),
				}
				for _, ddl := range tables {
					if _, err := db.ExecContext(ctx, ddl); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}
}
