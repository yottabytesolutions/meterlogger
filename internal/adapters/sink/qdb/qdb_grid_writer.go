package qdb

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type GridStore struct {
	client *DBClient
	table  string
	logger *slog.Logger
}

func (w *GridStore) StoreGridTelegram(ctx context.Context, telegram domain.GridTelegram) error {
	w.logger.DebugContext(ctx, "qdb: buffering grid telegram", debuglog.GridAttrs(telegram))
	sender := w.client.sender.
		Table(w.table).
		Symbol("MeterMerkType", telegram.MeterMerkType).
		Symbol("Serienummer", telegram.Serienummer).
		Float64Column("UsageCounter1", telegram.UsageCounter1).
		Float64Column("UsageCounter2", telegram.UsageCounter2).
		Float64Column("OutputCounter1", telegram.OutputCounter1).
		Float64Column("OutputCounter2", telegram.OutputCounter2).
		Int64Column("TotalPowerUsage", int64(telegram.TotalPowerUsage)).
		Int64Column("TotalPowerOutput", int64(telegram.TotalPowerOutput)).
		Int64Column("BrownoutsP1", int64(telegram.BrownoutsP1)).
		Int64Column("BrownoutsP2", int64(telegram.BrownoutsP2)).
		Int64Column("BrownoutsP3", int64(telegram.BrownoutsP3)).
		Int64Column("SpikesP1", int64(telegram.SpikesP1)).
		Int64Column("SpikesP2", int64(telegram.SpikesP2)).
		Int64Column("SpikesP3", int64(telegram.SpikesP3)).
		Float64Column("VoltageP1", telegram.VoltageP1).
		Float64Column("VoltageP2", telegram.VoltageP2).
		Float64Column("VoltageP3", telegram.VoltageP3).
		Int64Column("CurrentP1", int64(telegram.CurrentP1)).
		Int64Column("CurrentP2", int64(telegram.CurrentP2)).
		Int64Column("CurrentP3", int64(telegram.CurrentP3)).
		Int64Column("PowerUsageP1", int64(telegram.PowerUsageP1)).
		Int64Column("PowerUsageP2", int64(telegram.PowerUsageP2)).
		Int64Column("PowerUsageP3", int64(telegram.PowerUsageP3)).
		Int64Column("PowerOutputP1", int64(telegram.PowerOutputP1)).
		Int64Column("PowerOutputP2", int64(telegram.PowerOutputP2)).
		Int64Column("PowerOutputP3", int64(telegram.PowerOutputP3)).
		Int64Column("AvgDemand", int64(telegram.AvgDemand)).
		Int64Column("MaxDemandMonth", int64(telegram.MaxDemandMonth))
	// The zero time is not representable in ILP; only meters that publish
	// peak demand set MaxDemandMonthAt.
	if !telegram.MaxDemandMonthAt.IsZero() {
		sender = sender.TimestampColumn("MaxDemandMonthAt", telegram.MaxDemandMonthAt)
	}
	return sender.At(ctx, telegram.Time)
}

func (w *GridStore) Flush(ctx context.Context) error {
	w.logger.DebugContext(ctx, "qdb: flushing grid data to QuestDB")
	return w.client.Flush(ctx)
}

func NewQuestDBGridWriter(
	client *DBClient,
	table string,
	logger *slog.Logger,
) *GridStore {
	return &GridStore{
		client: client,
		table:  table,
		logger: logger,
	}
}

func (w *GridStore) Close() error {
	w.client.Close()
	return nil
}
