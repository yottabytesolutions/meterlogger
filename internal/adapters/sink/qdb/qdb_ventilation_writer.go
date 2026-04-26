package qdb

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type DucoQuestDBRepository struct {
	client *DBClient
	table  string
	logger *slog.Logger
}

func NewDucoQuestDBRepository(
	client *DBClient,
	table string,
	logger *slog.Logger,
) *DucoQuestDBRepository {
	return &DucoQuestDBRepository{
		client: client,
		table:  table,
		logger: logger,
	}
}

func (repo *DucoQuestDBRepository) StoreBoxStatus(ctx context.Context, boxStatus domain.DucoBoxStatus) error {
	return repo.client.sender.Table(repo.table+"_box_general").
		Symbol("rfHomeId", boxStatus.General.RFHomeID).
		Int64Column("CalibKinZone1", int64(boxStatus.EnergyCalib.CalibKinZone1)).
		Int64Column("CalibKinZone2", int64(boxStatus.EnergyCalib.CalibKinZone2)).
		Int64Column("CalibKout", int64(boxStatus.EnergyCalib.CalibKout)).
		Int64Column("CalibPinInternZone1", int64(boxStatus.EnergyCalib.CalibPinInternZone1)).
		Int64Column("CalibPinInternZone2", int64(boxStatus.EnergyCalib.CalibPinInternZone2)).
		Int64Column("CalibPinMaxZone1", int64(boxStatus.EnergyCalib.CalibPinMaxZone1)).
		Int64Column("CalibPinMaxZone2", int64(boxStatus.EnergyCalib.CalibPinMaxZone2)).
		Int64Column("CalibPinXZone1", int64(boxStatus.EnergyCalib.CalibPinXZone1)).
		Int64Column("CalibPinXZone2", int64(boxStatus.EnergyCalib.CalibPinXZone2)).
		Int64Column("CalibPout", int64(boxStatus.EnergyCalib.CalibPout)).
		Int64Column("CalibPoutMax", int64(boxStatus.EnergyCalib.CalibPoutMax)).
		Int64Column("CalibQinZone1", int64(boxStatus.EnergyCalib.CalibQinZone1)).
		Int64Column("CalibQinZone2", int64(boxStatus.EnergyCalib.CalibQinZone2)).
		Int64Column("CalibQout", int64(boxStatus.EnergyCalib.CalibQout)).
		StringColumn("CalibState", boxStatus.EnergyCalib.CalibState).
		Int64Column("ExhaustFanPressActual", int64(boxStatus.EnergyFan.ExhaustFanPressActual)).
		Int64Column("ExhaustFanPressTarget", int64(boxStatus.EnergyFan.ExhaustFanPressTarget)).
		Int64Column("ExhaustFanPwmLevel", int64(boxStatus.EnergyFan.ExhaustFanPwmLevel)).
		Int64Column("ExhaustFanPwmPercentage", int64(boxStatus.EnergyFan.ExhaustFanPwmPercentage)).
		Int64Column("ExhaustFanSpeed", int64(boxStatus.EnergyFan.ExhaustFanSpeed)).
		Int64Column("SupplyFanPressActual", int64(boxStatus.EnergyFan.SupplyFanPressActual)).
		Int64Column("SupplyFanPressTarget", int64(boxStatus.EnergyFan.SupplyFanPressTarget)).
		Int64Column("SupplyFanPwmLevel", int64(boxStatus.EnergyFan.SupplyFanPwmLevel)).
		Int64Column("SupplyFanPwmPercentage", int64(boxStatus.EnergyFan.SupplyFanPwmPercentage)).
		Int64Column("SupplyFanSpeed", int64(boxStatus.EnergyFan.SupplyFanSpeed)).
		Int64Column("BypassStatus", int64(boxStatus.EnergyInfo.BypassStatus)).
		Int64Column("FilterRemainingTime", int64(boxStatus.EnergyInfo.FilterRemainingTime)).
		Int64Column("FrostProtHeaterLevel", int64(boxStatus.EnergyInfo.FrostProtHeaterLevel)).
		Int64Column("FrostProtPressReduct", int64(boxStatus.EnergyInfo.FrostProtPressReduct)).
		BoolColumn("FrostProtState", boxStatus.EnergyInfo.FrostProtState).
		Int64Column("TempEHA", int64(boxStatus.EnergyInfo.TempEHA)).
		Int64Column("TempETA", int64(boxStatus.EnergyInfo.TempETA)).
		Int64Column("TempODA", int64(boxStatus.EnergyInfo.TempODA)).
		Int64Column("TempSUP", int64(boxStatus.EnergyInfo.TempSUP)).
		StringColumn("GeneralInstallerState", boxStatus.General.InstallerState).
		BoolColumn("WeatherStationPresent", boxStatus.WeatherStation.Present).
		At(ctx, time.Now())
}

//nolint:dupl // DucoRFSensorStatus and DucoNodeBoxStatus have similar but distinct fields
func (repo *DucoQuestDBRepository) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	sender := repo.client.sender

	switch data := nodeData.(type) {
	case domain.DucoRFSensorStatus:
		return sender.Table(repo.table+"_node").
			Symbol("node", strconv.Itoa(data.Node)).
			Symbol("location", data.Location).
			Symbol("device", data.DevType).
			Symbol("connection_type", data.Netw).
			Symbol("serialnumber", data.Serialnb).
			Symbol("sw_version", data.Swversion).
			StringColumn("mode", data.Mode).
			StringColumn("state", data.State).
			Int64Column("rssi_direct", int64(data.RssiN2M)).
			Int64Column("rssi_with_hops", int64(data.RssiN2H)).
			Float64Column("co2", data.Co2).
			Float64Column("temp", data.Temp).
			Float64Column("humidity", data.Rh).
			Int64Column("snsr", int64(data.Snsr)).
			Int64Column("cerr", int64(data.Cerr)).
			Int64Column("ovrl", int64(data.Ovrl)).
			Int64Column("cntdwn", int64(data.Cntdwn)).
			Int64Column("hop_via", int64(data.HopVia)).
			Int64Column("show", int64(data.Show)).
			Int64Column("link", int64(data.Link)).
			At(ctx, time.Now())

	case domain.DucoNodeBoxStatus: //nolint:dupl // similar to DucoRFSensorStatus but has different fields (trgt/actl)
		return sender.Table(repo.table+"_box_node").
			Symbol("node", strconv.Itoa(data.Node)).
			Symbol("location", data.Location).
			Symbol("device", data.DevType).
			Symbol("connection_type", data.Netw).
			Symbol("serialnumber", data.Serialnb).
			Symbol("sw_version", data.Swversion).
			StringColumn("mode", data.Mode).
			StringColumn("state", data.State).
			Int64Column("trgt", int64(data.Trgt)).
			Int64Column("actl", int64(data.Actl)).
			Float64Column("co2", data.Co2).
			Float64Column("temp", data.Temp).
			Float64Column("humidity", data.Rh).
			Int64Column("snsr", int64(data.Snsr)).
			Int64Column("cerr", int64(data.Cerr)).
			Int64Column("ovrl", int64(data.Ovrl)).
			Int64Column("cntdwn", int64(data.Cntdwn)).
			Int64Column("show", int64(data.Show)).
			Int64Column("link", int64(data.Link)).
			At(ctx, time.Now())

	case domain.DucoNodeBoxValveStatus:

		return sender.Table(repo.table+"_valve").
			Symbol("node", strconv.Itoa(data.Node)).
			Symbol("location", data.Location).
			Symbol("device", data.DevType).
			Symbol("connection_type", data.Netw).
			Symbol("serialnumber", data.Serialnb).
			Symbol("sw_version", data.Swversion).
			StringColumn("mode", data.Mode).
			StringColumn("state", data.State).
			Int64Column("trgt", int64(data.Trgt)).
			Int64Column("actl", int64(data.Actl)).
			Int64Column("snsr", int64(data.Snsr)).
			Int64Column("cerr", int64(data.Cerr)).
			Int64Column("ovrl", int64(data.Ovrl)).
			Int64Column("cntdwn", int64(data.Cntdwn)).
			Int64Column("show", int64(data.Show)).
			Int64Column("link", int64(data.Link)).
			At(ctx, time.Now())

	default:
		repo.logger.WarnContext(ctx, "Unknown node data type, skipping")
		return nil
	}
}

func (repo *DucoQuestDBRepository) Flush(ctx context.Context) error {
	return repo.client.Flush(ctx)
}

func (repo *DucoQuestDBRepository) Close() error {
	repo.client.Close()
	return nil
}
