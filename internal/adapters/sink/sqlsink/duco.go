package sqlsink

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// DucoStore implements domain.DucoRepository.
type DucoStore struct {
	db            *DB
	base          string
	insertBox     string
	insertNode    string
	insertBoxNode string
	insertValve   string
	logger        *slog.Logger
}

// NewDucoStore creates and migrates a DucoStore.
func NewDucoStore(ctx context.Context, db *DB, base string, logger *slog.Logger) (*DucoStore, error) {
	tables := []migrationTable{
		{name: base + "_box_general", columns: ducoBoxGeneralColumns()},
		{name: base + "_node", columns: ducoNodeColumns()},
		{name: base + "_box_node", columns: ducoBoxNodeColumns()},
		{name: base + "_valve", columns: ducoValveColumns()},
	}
	if err := migrate(ctx, db, "duco", base, "create ventilation tables", tables, logger); err != nil {
		return nil, err
	}
	d := db.dialect
	return &DucoStore{
		db:            db,
		base:          base,
		insertBox:     insertSQL(d, base+"_box_general", ducoBoxGeneralColumns()),
		insertNode:    insertSQL(d, base+"_node", ducoNodeColumns()),
		insertBoxNode: insertSQL(d, base+"_box_node", ducoBoxNodeColumns()),
		insertValve:   insertSQL(d, base+"_valve", ducoValveColumns()),
		logger:        logger,
	}, nil
}

// StoreBoxStatus inserts duco box status.
func (s *DucoStore) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insertBox,
		time.Now(),
		b.General.RFHomeID,
		b.EnergyFan.ExhaustFanSpeed, b.EnergyFan.SupplyFanSpeed,
		b.EnergyFan.ExhaustFanPwmPercentage, b.EnergyFan.SupplyFanPwmPercentage,
		b.EnergyInfo.BypassStatus, b.EnergyInfo.FilterRemainingTime, b.EnergyInfo.FrostProtState,
		b.EnergyInfo.TempEHA, b.EnergyInfo.TempETA, b.EnergyInfo.TempODA, b.EnergyInfo.TempSUP,
		b.General.InstallerState, b.WeatherStation.Present,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": StoreBoxStatus failed", slog.Any("error", err))
	}
	return err
}

// StoreNodeData inserts duco node data into the appropriate table.
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
		s.logger.WarnContext(ctx, s.db.dialect.name+": unknown node type, skipping",
			slog.String("type", fmt.Sprintf("%T", nodeData)))
		return nil
	}
}

func (s *DucoStore) storeRFSensor(ctx context.Context, now time.Time, d domain.DucoRFSensorStatus) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insertNode,
		now, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
		d.Mode, d.State, d.Co2, d.Temp, d.Rh, d.RssiN2M, d.RssiN2H, d.HopVia,
		d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": store RF sensor node failed", slog.Any("error", err))
	}
	return err
}

func (s *DucoStore) storeBoxNode(ctx context.Context, now time.Time, d domain.DucoNodeBoxStatus) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insertBoxNode,
		now, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
		d.Mode, d.State, d.Trgt, d.Actl, d.Co2, d.Temp, d.Rh,
		d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": store box node failed", slog.Any("error", err))
	}
	return err
}

func (s *DucoStore) storeValveNode(ctx context.Context, now time.Time, d domain.DucoNodeBoxValveStatus) error {
	_, err := s.db.db.ExecContext(
		ctx, s.insertValve,
		now, d.Node, d.Location, d.DevType, d.Netw, d.Serialnb, d.Swversion,
		d.Mode, d.State, d.Trgt, d.Actl, d.Snsr, d.Cerr, d.Ovrl, d.Cntdwn, d.Show, d.Link,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, s.db.dialect.name+": store valve node failed", slog.Any("error", err))
	}
	return err
}

// Flush is a no-op; writes auto-commit.
func (s *DucoStore) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared DB is closed via DB.Close().
func (s *DucoStore) Close() error { return nil }
