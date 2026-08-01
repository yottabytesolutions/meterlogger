package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/schemastore"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// Buffered rows carry the timestamp captured at store time so batching does
// not skew readings towards the flush moment.
type ducoBoxRow struct {
	ts     time.Time
	status domain.DucoBoxStatus
}

type ducoSensorRow struct {
	ts   time.Time
	node domain.DucoRFSensorStatus
}

type ducoBoxNodeRow struct {
	ts   time.Time
	node domain.DucoNodeBoxStatus
}

type ducoValveRow struct {
	ts   time.Time
	node domain.DucoNodeBoxValveStatus
}

// DucoStore implements domain.DucoRepository for ClickHouse.
type DucoStore struct {
	db       *DB
	base     string
	logger   *slog.Logger
	boxes    batchBuffer[ducoBoxRow]
	sensors  batchBuffer[ducoSensorRow]
	boxNodes batchBuffer[ducoBoxNodeRow]
	valves   batchBuffer[ducoValveRow]
}

// NewDucoStore creates and migrates a DucoStore.
func NewDucoStore(ctx context.Context, db *DB, base string, logger *slog.Logger) (*DucoStore, error) {
	m := schemastore.NewClickHouseMigrator(db.db, logger)
	if err := m.Migrate(ctx, "clickhouse_duco_"+base, ducoMigrations(db.db, base)); err != nil {
		return nil, fmt.Errorf("clickhouse duco migration: %w", err)
	}
	return &DucoStore{db: db, base: base, logger: logger}, nil
}

// StoreBoxStatus buffers duco box status for ClickHouse.
func (s *DucoStore) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	warnDropped(ctx, s.logger, s.base+"_box_general", s.boxes.add(ducoBoxRow{ts: time.Now(), status: b}))
	return nil
}

// StoreNodeData buffers duco node data for ClickHouse.
func (s *DucoStore) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	now := time.Now()
	switch d := nodeData.(type) {
	case domain.DucoRFSensorStatus:
		warnDropped(ctx, s.logger, s.base+"_node", s.sensors.add(ducoSensorRow{ts: now, node: d}))
	case domain.DucoNodeBoxStatus:
		warnDropped(ctx, s.logger, s.base+"_box_node", s.boxNodes.add(ducoBoxNodeRow{ts: now, node: d}))
	case domain.DucoNodeBoxValveStatus:
		warnDropped(ctx, s.logger, s.base+"_valve", s.valves.add(ducoValveRow{ts: now, node: d}))
	default:
		s.logger.WarnContext(
			ctx, "clickhouse: unknown node type, skipping",
			slog.String("type", fmt.Sprintf("%T", nodeData)),
		)
	}
	return nil
}

// Flush inserts every buffered duco table batch, one transaction per table
// (the driver allows a single prepared batch per transaction). Failed
// batches are re-queued for the next flush.
func (s *DucoStore) Flush(ctx context.Context) error {
	return errors.Join(
		flushBatch(ctx, s.db.db, s.logger, s.base+"_box_general", &s.boxes, s.insertBoxRows),
		flushBatch(ctx, s.db.db, s.logger, s.base+"_node", &s.sensors, s.insertSensorRows),
		flushBatch(ctx, s.db.db, s.logger, s.base+"_box_node", &s.boxNodes, s.insertBoxNodeRows),
		flushBatch(ctx, s.db.db, s.logger, s.base+"_valve", &s.valves, s.insertValveRows),
	)
}

// Table names below come from config, not user input.

func (s *DucoStore) insertBoxRows(ctx context.Context, tx *sql.Tx, rows []ducoBoxRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s_box_general
            (ts, rf_home_id,
             exhaust_fan_speed, supply_fan_speed,
             exhaust_fan_pwm_percentage, supply_fan_pwm_percentage,
             bypass_status, filter_remaining_time, frost_prot_state,
             temp_eha, temp_eta, temp_oda, temp_sup,
             installer_state, weather_station_present)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.base,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare box status insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		b := r.status
		_, err = stmt.ExecContext(
			ctx,
			r.ts,
			b.General.RFHomeID,
			b.EnergyFan.ExhaustFanSpeed, b.EnergyFan.SupplyFanSpeed,
			b.EnergyFan.ExhaustFanPwmPercentage, b.EnergyFan.SupplyFanPwmPercentage,
			b.EnergyInfo.BypassStatus, b.EnergyInfo.FilterRemainingTime, b.EnergyInfo.FrostProtState,
			b.EnergyInfo.TempEHA, b.EnergyInfo.TempETA, b.EnergyInfo.TempODA, b.EnergyInfo.TempSUP,
			b.General.InstallerState, b.WeatherStation.Present,
		)
		if err != nil {
			return fmt.Errorf("exec box status insert: %w", err)
		}
	}
	return nil
}

func (s *DucoStore) insertSensorRows(ctx context.Context, tx *sql.Tx, rows []ducoSensorRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s_node
            (ts, node_id, location, device, connection_type, serial_no, sw_version,
             mode, state, co2, temp, humidity, rssi_direct, rssi_with_hops, hop_via,
             snsr, cerr, ovrl, cntdwn, show, link)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.base,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare rf sensor insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		d := r.node
		_, err = stmt.ExecContext(
			ctx,
			r.ts, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
			d.Mode, d.State, d.Co2, d.Temp, d.Rh, d.RssiN2M, d.RssiN2H, d.HopVia,
			d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
		)
		if err != nil {
			return fmt.Errorf("exec rf sensor insert: %w", err)
		}
	}
	return nil
}

func (s *DucoStore) insertBoxNodeRows(ctx context.Context, tx *sql.Tx, rows []ducoBoxNodeRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s_box_node
            (ts, node_id, location, device, connection_type, serial_no, sw_version,
             mode, state, trgt, actl, co2, temp, humidity,
             snsr, cerr, ovrl, cntdwn, show, link)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.base,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare box node insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		d := r.node
		_, err = stmt.ExecContext(
			ctx,
			r.ts, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
			d.Mode, d.State, d.Trgt, d.Actl, d.Co2, d.Temp, d.Rh,
			d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
		)
		if err != nil {
			return fmt.Errorf("exec box node insert: %w", err)
		}
	}
	return nil
}

func (s *DucoStore) insertValveRows(ctx context.Context, tx *sql.Tx, rows []ducoValveRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(
		ctx, fmt.Sprintf(
			`INSERT INTO %s_valve
            (ts, node_id, location, device, connection_type, serial_no, sw_version,
             mode, state, trgt, actl, snsr, cerr, ovrl, cntdwn, show, link)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.base,
		),
	)
	if err != nil {
		return fmt.Errorf("prepare valve insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, r := range rows {
		d := r.node
		_, err = stmt.ExecContext(
			ctx,
			r.ts, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
			d.Mode, d.State, d.Trgt, d.Actl, d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
		)
		if err != nil {
			return fmt.Errorf("exec valve insert: %w", err)
		}
	}
	return nil
}

// Close flushes pending rows with a bounded timeout. The shared DB is closed
// via DB.Close().
func (s *DucoStore) Close() error { return closeWithFinalFlush(s.Flush) }
