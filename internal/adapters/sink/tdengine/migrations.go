package tdengine

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
)

func heatMigrations(db *sql.DB, table string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create heat table",
			Up: func(ctx context.Context) error {
				// table name comes from config, not user HTTP input.
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts          TIMESTAMP,
    meter_id    NCHAR(255),
    serial_no   NCHAR(255),
    power_w     BIGINT,
    energy_gj   DOUBLE,
    t_forward_c DOUBLE,
    t_return_c  DOUBLE,
    t_diff_c    DOUBLE,
    volume_cm3  DOUBLE,
    seconds     BIGINT,
    max_flow    DOUBLE,
    max_power_w BIGINT
)`, table,
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
				// table name comes from config, not user HTTP input.
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts                 TIMESTAMP,
    meter_type         NCHAR(255),
    serial_no          NCHAR(255),
    usage_counter1     DOUBLE,
    usage_counter2     DOUBLE,
    output_counter1    DOUBLE,
    output_counter2    DOUBLE,
    total_power_usage  BIGINT,
    total_power_output BIGINT,
    brownouts_p1       INT,
    brownouts_p2       INT,
    brownouts_p3       INT,
    spikes_p1          INT,
    spikes_p2          INT,
    spikes_p3          INT,
    voltage_p1         DOUBLE,
    voltage_p2         DOUBLE,
    voltage_p3         DOUBLE,
    current_p1         BIGINT,
    current_p2         BIGINT,
    current_p3         BIGINT,
    power_usage_p1     BIGINT,
    power_usage_p2     BIGINT,
    power_usage_p3     BIGINT,
    power_output_p1    BIGINT,
    power_output_p2    BIGINT,
    power_output_p3    BIGINT
)`, table,
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
				// table name comes from config, not user HTTP input.
				_, err := db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s (
    ts            TIMESTAMP,
    envoy_serial  NCHAR(255),
    production_wh DOUBLE,
    watt          DOUBLE,
    panel_count   INT
)`, table,
					),
				)
				if err != nil {
					return err
				}
				_, err = db.ExecContext(
					ctx, fmt.Sprintf(
						`CREATE TABLE IF NOT EXISTS %s_inverters (
    ts              TIMESTAMP,
    envoy_serial    NCHAR(255),
    inverter_serial NCHAR(255),
    channel_id      INT,
    operating       BOOL,
    communicating   BOOL,
    producing       BOOL,
    phase           NCHAR(64),
    watts           INT,
    peak_watts      INT
)`, table,
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
    ts                         TIMESTAMP,
    rf_home_id                 NCHAR(255),
    exhaust_fan_speed          INT,
    supply_fan_speed           INT,
    exhaust_fan_pwm_percentage INT,
    supply_fan_pwm_percentage  INT,
    bypass_status              INT,
    filter_remaining_time      INT,
    frost_prot_state           BOOL,
    temp_eha                   INT,
    temp_eta                   INT,
    temp_oda                   INT,
    temp_sup                   INT,
    installer_state            NCHAR(255),
    weather_station_present    BOOL
)`, base,
	)
}

func ducoNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_node (
    ts              TIMESTAMP,
    node_id         INT,
    location        NCHAR(255),
    device          NCHAR(255),
    connection_type NCHAR(255),
    serial_no       NCHAR(255),
    sw_version      NCHAR(255),
    mode            NCHAR(255),
    state           NCHAR(255),
    co2             DOUBLE,
    temp            DOUBLE,
    humidity        DOUBLE,
    rssi_direct     INT,
    rssi_with_hops  INT,
    hop_via         INT,
    snsr            INT,
    cerr            INT,
    ovrl            INT,
    cntdwn          INT,
    show            INT,
    link            INT
)`, base,
	)
}

func ducoBoxNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_box_node (
    ts              TIMESTAMP,
    node_id         INT,
    location        NCHAR(255),
    device          NCHAR(255),
    connection_type NCHAR(255),
    serial_no       NCHAR(255),
    sw_version      NCHAR(255),
    mode            NCHAR(255),
    state           NCHAR(255),
    trgt            INT,
    actl            INT,
    co2             DOUBLE,
    temp            DOUBLE,
    humidity        DOUBLE,
    snsr            INT,
    cerr            INT,
    ovrl            INT,
    cntdwn          INT,
    show            INT,
    link            INT
)`, base,
	)
}

func ducoValveTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_valve (
    ts              TIMESTAMP,
    node_id         INT,
    location        NCHAR(255),
    device          NCHAR(255),
    connection_type NCHAR(255),
    serial_no       NCHAR(255),
    sw_version      NCHAR(255),
    mode            NCHAR(255),
    state           NCHAR(255),
    trgt            INT,
    actl            INT,
    snsr            INT,
    cerr            INT,
    ovrl            INT,
    cntdwn          INT,
    show            INT,
    link            INT
)`, base,
	)
}

func ducoMigrations(db *sql.DB, base string) []schemastore.Migration {
	return []schemastore.Migration{
		{
			Version:     1,
			Description: "create ventilation tables",
			Up: func(ctx context.Context) error {
				// table names come from config, not user HTTP input.
				ddls := []string{
					ducoBoxGeneralTableSQL(base),
					ducoNodeTableSQL(base),
					ducoBoxNodeTableSQL(base),
					ducoValveTableSQL(base),
				}
				for _, ddl := range ddls {
					if _, err := db.ExecContext(ctx, ddl); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}
}
