//nolint:dupl // gas, water and thermal writers share the same shape but persist distinct domain types
package qdb

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// QuestDBWaterWriter implements domain.WaterRepository for QuestDB.
type QuestDBWaterWriter struct {
	client      *DBClient
	measurement string
	logger      *slog.Logger
}

// NewQuestDBWaterWriter creates a water reading writer for the given measurement.
func NewQuestDBWaterWriter(client *DBClient, measurement string, logger *slog.Logger) *QuestDBWaterWriter {
	return &QuestDBWaterWriter{
		client:      client,
		measurement: measurement,
		logger:      logger,
	}
}

// StoreWaterReading buffers one water reading into the ILP sender.
func (w *QuestDBWaterWriter) StoreWaterReading(ctx context.Context, r domain.WaterReading) error {
	w.logger.DebugContext(ctx, "qdb: buffering water reading",
		slog.String("serial_no", r.SerialNo),
		slog.Float64("reading_m3", r.ReadingM3),
		slog.Time("captured_at", r.CapturedAt),
	)
	return w.client.sender.
		Table(w.measurement).
		Symbol("serial_no", r.SerialNo).
		Int64Column("channel", int64(r.Channel)).
		Int64Column("device_type", int64(r.DeviceType)).
		Float64Column("reading_m3", r.ReadingM3).
		TimestampColumn("received_at", r.ReceivedAt).
		At(ctx, r.CapturedAt)
}

// Flush sends buffered water readings to QuestDB.
func (w *QuestDBWaterWriter) Flush(ctx context.Context) error {
	w.logger.DebugContext(ctx, "qdb: flushing water data to QuestDB")
	return w.client.Flush(ctx)
}

// Close closes the shared QuestDB client.
func (w *QuestDBWaterWriter) Close() error {
	w.client.Close()
	return nil
}
