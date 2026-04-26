package qdb

import (
	"context"
	"log/slog"
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type SolarWriter struct {
	client *DBClient
	table  string
	logger *slog.Logger
}

func (w *SolarWriter) StoreEnvoySolarData(ctx context.Context, data domain.EnvoySolarData) error {
	err := w.client.sender.Table(w.table).
		Symbol("EnvoySerialNumber", data.EnvoySerial).
		Float64Column("ProductionWattHours", data.ProductionWh).
		Float64Column("ProductionWatt", data.Watt).
		Int64Column("PanelCount", int64(data.PanelCount)).
		At(ctx, data.ReadingTime)
	if err != nil {
		w.logger.ErrorContext(ctx, "error writing data", slog.Any("error", err))
	}

	for _, inverter := range data.Inverters {
		inverterErr := w.client.sender.Table(w.table+"_inverters").
			Symbol("InverterSerialNumber", inverter.SerialNumber).
			StringColumn("EnvoySerialNumber", data.EnvoySerial).
			Int64Column("ChannelID", int64(inverter.Chaneid)).
			BoolColumn("Operating", inverter.Operating).
			BoolColumn("Communicating", inverter.Communicating).
			BoolColumn("Producing", inverter.Producing).
			StringColumn("Phase", inverter.Phase).
			StringColumn("Status", strings.Join(inverter.DeviceStatus, ",")).
			Int64Column("Watts", int64(inverter.LastReportedWatts)).
			Int64Column("PeakWatts", int64(inverter.MaxReportWatts)).
			At(ctx, inverter.ReportTime)
		if inverterErr != nil {
			w.logger.ErrorContext(ctx, "error writing inverter data", slog.Any("error", inverterErr))
		}
	}
	return err
}

func (w *SolarWriter) Flush(ctx context.Context) error {
	return w.client.Flush(ctx)
}

func NewQuestDBSolarWriter(
	client *DBClient,
	table string,
	logger *slog.Logger,
) *SolarWriter {
	return &SolarWriter{
		client: client,
		table:  table,
		logger: logger,
	}
}

func (w *SolarWriter) Close() error {
	w.client.Close()
	return nil
}
