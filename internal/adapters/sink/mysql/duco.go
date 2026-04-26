package mysql

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// DucoStore implements domain.DucoRepository for MySQL.
type DucoStore struct {
	db     *DB
	base   string
	logger *slog.Logger
}

// NewDucoStore creates and migrates a DucoStore.
func NewDucoStore(ctx context.Context, db *DB, base string, logger *slog.Logger) (*DucoStore, error) {
	m := schemastore.NewSQLMigrator(db.db, schemastore.QuestionPlaceholder, logger)
	if err := m.Migrate(ctx, "mysql_duco_"+base, ducoMigrations(db.db, base)); err != nil {
		return nil, fmt.Errorf("mysql duco migration: %w", err)
	}
	return &DucoStore{db: db, base: base, logger: logger}, nil
}

// StoreBoxStatus inserts duco box status into MySQL.
func (s *DucoStore) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s_box_general
            (ts, rf_home_id,
             exhaust_fan_speed, supply_fan_speed,
             exhaust_fan_pwm_percentage, supply_fan_pwm_percentage,
             bypass_status, filter_remaining_time, frost_prot_state,
             temp_eha, temp_eta, temp_oda, temp_sup,
             installer_state, weather_station_present)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.base,
		),
		time.Now(),
		b.General.RFHomeID,
		b.EnergyFan.ExhaustFanSpeed, b.EnergyFan.SupplyFanSpeed,
		b.EnergyFan.ExhaustFanPwmPercentage, b.EnergyFan.SupplyFanPwmPercentage,
		b.EnergyInfo.BypassStatus, b.EnergyInfo.FilterRemainingTime, b.EnergyInfo.FrostProtState,
		b.EnergyInfo.TempEHA, b.EnergyInfo.TempETA, b.EnergyInfo.TempODA, b.EnergyInfo.TempSUP,
		b.General.InstallerState, b.WeatherStation.Present,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "mysql: StoreBoxStatus failed", slog.Any("error", err))
	}
	return err
}

// StoreNodeData inserts duco node data into the appropriate MySQL table.
func (s *DucoStore) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	now := time.Now()
	switch d := nodeData.(type) {
	case domain.DucoRFSensorStatus:
		return s.storeRFSensor(ctx, now, d)
	case domain.DucoNodeBoxStatus:
		return s.storeBoxNode(ctx, now, d)
	case domain.DucoNodeBoxValveStatus:
		return s.storeValveNode(ctx, now, d)
	default:
		s.logger.WarnContext(ctx, "mysql: unknown node type, skipping",
			slog.String("type", fmt.Sprintf("%T", nodeData)))
		return nil
	}
}

func (s *DucoStore) storeRFSensor(ctx context.Context, now time.Time, d domain.DucoRFSensorStatus) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s_node
            (ts, node_id, location, device, connection_type, serial_no, sw_version,
             mode, state, co2, temp, humidity, rssi_direct, rssi_with_hops, hop_via,
             snsr, cerr, ovrl, cntdwn, show, link)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			s.base,
		),
		now, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
		d.Mode, d.State, d.Co2, d.Temp, d.Rh, d.RssiN2M, d.RssiN2H, d.HopVia,
		d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "mysql: store RF sensor node failed", slog.Any("error", err))
	}
	return err
}

func (s *DucoStore) storeBoxNode(ctx context.Context, now time.Time, d domain.DucoNodeBoxStatus) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s_box_node
            (ts, node_id, location, device, connection_type, serial_no, sw_version,
             mode, state, trgt, actl, co2, temp, humidity,
             snsr, cerr, ovrl, cntdwn, show, link)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			s.base,
		),
		now, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
		d.Mode, d.State, d.Trgt, d.Actl, d.Co2, d.Temp, d.Rh,
		d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "mysql: store box node failed", slog.Any("error", err))
	}
	return err
}

func (s *DucoStore) storeValveNode(ctx context.Context, now time.Time, d domain.DucoNodeBoxValveStatus) error {
	// table name comes from config, not user HTTP input.
	_, err := s.db.db.ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s_valve
            (ts, node_id, location, device, connection_type, serial_no, sw_version,
             mode, state, trgt, actl, snsr, cerr, ovrl, cntdwn, show, link)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			s.base,
		),
		now, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
		d.Mode, d.State, d.Trgt, d.Actl, d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "mysql: store valve node failed", slog.Any("error", err))
	}
	return err
}

// Flush is a no-op for MySQL (auto-commit).
func (s *DucoStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *DucoStore) Close() error { return nil }
