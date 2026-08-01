//nolint:dupl // gas, water and thermal writers share the same shape but persist distinct domain types
package qdb

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// QuestDBThermalWriter implements domain.ThermalRepository for QuestDB.
type QuestDBThermalWriter struct {
	client      *DBClient
	measurement string
	logger      *slog.Logger
}

// NewQuestDBThermalWriter creates a thermal reading writer for the given measurement.
func NewQuestDBThermalWriter(client *DBClient, measurement string, logger *slog.Logger) *QuestDBThermalWriter {
	return &QuestDBThermalWriter{
		client:      client,
		measurement: measurement,
		logger:      logger,
	}
}

// StoreThermalReading buffers one thermal reading into the ILP sender.
func (w *QuestDBThermalWriter) StoreThermalReading(ctx context.Context, r domain.ThermalReading) error {
	w.logger.DebugContext(ctx, "qdb: buffering thermal reading",
		slog.String("serial_no", r.SerialNo),
		slog.Float64("reading_gj", r.ReadingGJ),
		slog.Time("captured_at", r.CapturedAt),
	)
	return w.client.sender.
		Table(w.measurement).
		Symbol("serial_no", r.SerialNo).
		Int64Column("channel", int64(r.Channel)).
		Int64Column("device_type", int64(r.DeviceType)).
		Float64Column("reading_gj", r.ReadingGJ).
		TimestampColumn("received_at", r.ReceivedAt).
		At(ctx, r.CapturedAt)
}

// Flush sends buffered thermal readings to QuestDB.
func (w *QuestDBThermalWriter) Flush(ctx context.Context) error {
	w.logger.DebugContext(ctx, "qdb: flushing thermal data to QuestDB")
	return w.client.Flush(ctx)
}

// Close closes the shared QuestDB client.
func (w *QuestDBThermalWriter) Close() error {
	w.client.Close()
	return nil
}
