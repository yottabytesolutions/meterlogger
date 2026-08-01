package qdb

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// Scale factors that preserve the original QuestDB schema values.
const (
	heatEnergyScale    = 1e6   // Joules field unit → stored unit
	heatTempScale      = 100.0 // temperature ×100 for schema
	heatVolumeScale    = 1000.0
	heatSecondsPerHour = 3600
	heatFlowScale      = 1000.0
)

type HeatTelegramStore struct {
	client *DBClient
	table  string
	logger *slog.Logger
}

func NewQuestDBHeatTelegramWriter(
	client *DBClient,
	table string,
	logger *slog.Logger,
) *HeatTelegramStore {
	return &HeatTelegramStore{
		client: client,
		table:  table,
		logger: logger,
	}
}

func (store *HeatTelegramStore) StoreHeatTelegram(ctx context.Context, telegram domain.HeatTelegram) error {
	store.logger.DebugContext(ctx, "qdb: buffering heat telegram", debuglog.HeatAttrs(telegram))
	return store.client.sender.Table(store.table).
		Symbol("device", fmt.Sprintf("Multical %s", telegram.MeterID)).
		Symbol("serial", telegram.SerialNo).
		Symbol("location", "meterkast").
		Int64Column("power", telegram.ActualPower).
		Int64Column("energy", telegram.Joules/heatEnergyScale).
		Float64Column("t1", telegram.Tforward*heatTempScale).
		Float64Column("t2", telegram.Treturn*heatTempScale).
		Float64Column("t1mint2", telegram.Tdiff*heatTempScale).
		Int64Column("volume", int64(telegram.VolumeCm3*heatVolumeScale)).
		Int64Column("hours", telegram.SecondsCounter/heatSecondsPerHour).
		Float64Column("max_flow", telegram.MaxFlow*heatFlowScale).
		Int64Column("max_power", telegram.MaxPower).
		Int64Column("seconds", telegram.SecondsCounter).
		At(ctx, telegram.Timestamp)
}

func (store *HeatTelegramStore) Flush(ctx context.Context) error {
	store.logger.DebugContext(ctx, "qdb: flushing heat data to QuestDB")
	return store.client.Flush(ctx)
}

func (store *HeatTelegramStore) Close() error {
	store.client.Close()
	return nil
}
