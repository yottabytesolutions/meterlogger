package mysql

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
    ts           DATETIME(6)      NOT NULL,
    meter_id     VARCHAR(255)     NOT NULL,
    serial_no    VARCHAR(255)     NOT NULL,
    power_w      BIGINT           NOT NULL,
    energy_gj    DOUBLE           NOT NULL,
    t_forward_c  DOUBLE           NOT NULL,
    t_return_c   DOUBLE           NOT NULL,
    t_diff_c     DOUBLE           NOT NULL,
    volume_cm3   DOUBLE           NOT NULL,
    seconds      BIGINT           NOT NULL,
    max_flow     DOUBLE           NOT NULL,
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
    ts                 DATETIME(6)  NOT NULL,
    meter_type         VARCHAR(255) DEFAULT NULL,
    serial_no          VARCHAR(255) DEFAULT NULL,
    usage_counter1     DOUBLE       DEFAULT NULL,
    usage_counter2     DOUBLE       DEFAULT NULL,
    output_counter1    DOUBLE       DEFAULT NULL,
    output_counter2    DOUBLE       DEFAULT NULL,
    total_power_usage  BIGINT       DEFAULT NULL,
    total_power_output BIGINT       DEFAULT NULL,
    brownouts_p1       BIGINT       DEFAULT NULL,
    brownouts_p2       BIGINT       DEFAULT NULL,
    brownouts_p3       BIGINT       DEFAULT NULL,
    spikes_p1          BIGINT       DEFAULT NULL,
    spikes_p2          BIGINT       DEFAULT NULL,
    spikes_p3          BIGINT       DEFAULT NULL,
    voltage_p1         DOUBLE       DEFAULT NULL,
    voltage_p2         DOUBLE       DEFAULT NULL,
    voltage_p3         DOUBLE       DEFAULT NULL,
    current_p1         BIGINT       DEFAULT NULL,
    current_p2         BIGINT       DEFAULT NULL,
    current_p3         BIGINT       DEFAULT NULL,
    power_usage_p1     BIGINT       DEFAULT NULL,
    power_usage_p2     BIGINT       DEFAULT NULL,
    power_usage_p3     BIGINT       DEFAULT NULL,
    power_output_p1    BIGINT       DEFAULT NULL,
    power_output_p2    BIGINT       DEFAULT NULL,
    power_output_p3    BIGINT       DEFAULT NULL
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
    ts              DATETIME(6)  NOT NULL,
    envoy_serial    VARCHAR(255) NOT NULL,
    production_wh   DOUBLE       NOT NULL,
    watt            DOUBLE       NOT NULL,
    panel_count     INT          NOT NULL
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
    ts              DATETIME(6)  NOT NULL,
    envoy_serial    VARCHAR(255) NOT NULL,
    inverter_serial VARCHAR(255) NOT NULL,
    channel_id      INT          DEFAULT NULL,
    operating       TINYINT(1)   DEFAULT NULL,
    communicating   TINYINT(1)   DEFAULT NULL,
    producing       TINYINT(1)   DEFAULT NULL,
    phase           VARCHAR(50)  DEFAULT NULL,
    watts           INT          DEFAULT NULL,
    peak_watts      INT          DEFAULT NULL
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
    ts                         DATETIME(6)  NOT NULL,
    rf_home_id                 VARCHAR(255) DEFAULT NULL,
    exhaust_fan_speed          INT          DEFAULT NULL,
    supply_fan_speed           INT          DEFAULT NULL,
    exhaust_fan_pwm_percentage INT          DEFAULT NULL,
    supply_fan_pwm_percentage  INT          DEFAULT NULL,
    bypass_status              INT          DEFAULT NULL,
    filter_remaining_time      INT          DEFAULT NULL,
    frost_prot_state           TINYINT(1)   DEFAULT NULL,
    temp_eha                   INT          DEFAULT NULL,
    temp_eta                   INT          DEFAULT NULL,
    temp_oda                   INT          DEFAULT NULL,
    temp_sup                   INT          DEFAULT NULL,
    installer_state            VARCHAR(255) DEFAULT NULL,
    weather_station_present    TINYINT(1)   DEFAULT NULL
)`, base,
	)
}

func ducoNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_node (
    ts              DATETIME(6)  NOT NULL,
    node_id         INT          DEFAULT NULL,
    location        VARCHAR(255) DEFAULT NULL,
    device          VARCHAR(255) DEFAULT NULL,
    connection_type VARCHAR(255) DEFAULT NULL,
    serial_no       VARCHAR(255) DEFAULT NULL,
    sw_version      VARCHAR(255) DEFAULT NULL,
    mode            VARCHAR(255) DEFAULT NULL,
    state           VARCHAR(255) DEFAULT NULL,
    co2             DOUBLE       DEFAULT NULL,
    temp            DOUBLE       DEFAULT NULL,
    humidity        DOUBLE       DEFAULT NULL,
    rssi_direct     INT          DEFAULT NULL,
    rssi_with_hops  INT          DEFAULT NULL,
    hop_via         INT          DEFAULT NULL,
    snsr            INT          DEFAULT NULL,
    cerr            INT          DEFAULT NULL,
    ovrl            INT          DEFAULT NULL,
    cntdwn          INT          DEFAULT NULL,
    show            INT          DEFAULT NULL,
    link            INT          DEFAULT NULL
)`, base,
	)
}

func ducoBoxNodeTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_box_node (
    ts              DATETIME(6)  NOT NULL,
    node_id         INT          DEFAULT NULL,
    location        VARCHAR(255) DEFAULT NULL,
    device          VARCHAR(255) DEFAULT NULL,
    connection_type VARCHAR(255) DEFAULT NULL,
    serial_no       VARCHAR(255) DEFAULT NULL,
    sw_version      VARCHAR(255) DEFAULT NULL,
    mode            VARCHAR(255) DEFAULT NULL,
    state           VARCHAR(255) DEFAULT NULL,
    trgt            INT          DEFAULT NULL,
    actl            INT          DEFAULT NULL,
    co2             DOUBLE       DEFAULT NULL,
    temp            DOUBLE       DEFAULT NULL,
    humidity        DOUBLE       DEFAULT NULL,
    snsr            INT          DEFAULT NULL,
    cerr            INT          DEFAULT NULL,
    ovrl            INT          DEFAULT NULL,
    cntdwn          INT          DEFAULT NULL,
    show            INT          DEFAULT NULL,
    link            INT          DEFAULT NULL
)`, base,
	)
}

func ducoValveTableSQL(base string) string {
	return fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s_valve (
    ts              DATETIME(6)  NOT NULL,
    node_id         INT          DEFAULT NULL,
    location        VARCHAR(255) DEFAULT NULL,
    device          VARCHAR(255) DEFAULT NULL,
    connection_type VARCHAR(255) DEFAULT NULL,
    serial_no       VARCHAR(255) DEFAULT NULL,
    sw_version      VARCHAR(255) DEFAULT NULL,
    mode            VARCHAR(255) DEFAULT NULL,
    state           VARCHAR(255) DEFAULT NULL,
    trgt            INT          DEFAULT NULL,
    actl            INT          DEFAULT NULL,
    snsr            INT          DEFAULT NULL,
    cerr            INT          DEFAULT NULL,
    ovrl            INT          DEFAULT NULL,
    cntdwn          INT          DEFAULT NULL,
    show            INT          DEFAULT NULL,
    link            INT          DEFAULT NULL
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
