package postgres

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
						`
CREATE TABLE IF NOT EXISTS %s (
    ts           TIMESTAMPTZ      NOT NULL,
    meter_id     TEXT             NOT NULL,
    serial_no    TEXT             NOT NULL,
    power_w      BIGINT           NOT NULL,
    energy_gj    DOUBLE PRECISION NOT NULL,
    t_forward_c  DOUBLE PRECISION NOT NULL,
    t_return_c   DOUBLE PRECISION NOT NULL,
    t_diff_c     DOUBLE PRECISION NOT NULL,
    volume_cm3   DOUBLE PRECISION NOT NULL,
    seconds      BIGINT           NOT NULL,
    max_flow     DOUBLE PRECISION NOT NULL,
    max_power_w  BIGINT           NOT NULL
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
						`
CREATE TABLE IF NOT EXISTS %s (
    ts                 TIMESTAMPTZ      NOT NULL,
    meter_type         TEXT,
    serial_no          TEXT,
    usage_counter1     DOUBLE PRECISION,
    usage_counter2     DOUBLE PRECISION,
    output_counter1    DOUBLE PRECISION,
    output_counter2    DOUBLE PRECISION,
    total_power_usage  BIGINT,
    total_power_output BIGINT,
    brownouts_p1       BIGINT,
    brownouts_p2       BIGINT,
    brownouts_p3       BIGINT,
    spikes_p1          BIGINT,
    spikes_p2          BIGINT,
    spikes_p3          BIGINT,
    voltage_p1         DOUBLE PRECISION,
    voltage_p2         DOUBLE PRECISION,
    voltage_p3         DOUBLE PRECISION,
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
						`
CREATE TABLE IF NOT EXISTS %s (
    ts              TIMESTAMPTZ      NOT NULL,
    envoy_serial    TEXT             NOT NULL,
    production_wh   DOUBLE PRECISION NOT NULL,
    watt            DOUBLE PRECISION NOT NULL,
    panel_count     INT              NOT NULL
)`, table,
					),
				)
				if err != nil {
					return err
				}
				_, err = db.ExecContext(
					ctx, fmt.Sprintf(
						`
CREATE TABLE IF NOT EXISTS %s_inverters (
    ts              TIMESTAMPTZ NOT NULL,
    envoy_serial    TEXT        NOT NULL,
    inverter_serial TEXT        NOT NULL,
    channel_id      INT,
    operating       BOOLEAN,
    communicating   BOOLEAN,
    producing       BOOLEAN,
    phase           TEXT,
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
    ts                         TIMESTAMPTZ NOT NULL,
    rf_home_id                 TEXT,
    exhaust_fan_speed          INT,
    supply_fan_speed           INT,
    exhaust_fan_pwm_percentage INT,
    supply_fan_pwm_percentage  INT,
    bypass_status              INT,
    filter_remaining_time      INT,
    frost_prot_state           BOOLEAN,
    temp_eha                   INT,
    temp_eta                   INT,
    temp_oda                   INT,
    temp_sup                   INT,
    installer_state            TEXT,
    weather_station_present    BOOLEAN
)`, base,
	)
}

func ducoNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_node (
    ts              TIMESTAMPTZ      NOT NULL,
    node_id         INT,
    location        TEXT,
    device          TEXT,
    connection_type TEXT,
    serial_no       TEXT,
    sw_version      TEXT,
    mode            TEXT,
    state           TEXT,
    co2             DOUBLE PRECISION,
    temp            DOUBLE PRECISION,
    humidity        DOUBLE PRECISION,
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
    ts              TIMESTAMPTZ      NOT NULL,
    node_id         INT,
    location        TEXT,
    device          TEXT,
    connection_type TEXT,
    serial_no       TEXT,
    sw_version      TEXT,
    mode            TEXT,
    state           TEXT,
    trgt            INT,
    actl            INT,
    co2             DOUBLE PRECISION,
    temp            DOUBLE PRECISION,
    humidity        DOUBLE PRECISION,
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
    ts              TIMESTAMPTZ NOT NULL,
    node_id         INT,
    location        TEXT,
    device          TEXT,
    connection_type TEXT,
    serial_no       TEXT,
    sw_version      TEXT,
    mode            TEXT,
    state           TEXT,
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
