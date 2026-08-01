package qdb

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// QuestDBGasWriter implements domain.GasRepository for QuestDB.
type QuestDBGasWriter struct {
	client      *DBClient
	measurement string
	logger      *slog.Logger
}

// NewQuestDBGasWriter creates a gas reading writer for the given measurement.
func NewQuestDBGasWriter(client *DBClient, measurement string, logger *slog.Logger) *QuestDBGasWriter {
	return &QuestDBGasWriter{
		client:      client,
		measurement: measurement,
		logger:      logger,
	}
}

// StoreGasReading buffers one gas reading into the ILP sender.
func (w *QuestDBGasWriter) StoreGasReading(ctx context.Context, r domain.GasReading) error {
	w.logger.DebugContext(ctx, "qdb: buffering gas reading",
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

// Flush sends buffered gas readings to QuestDB.
func (w *QuestDBGasWriter) Flush(ctx context.Context) error {
	w.logger.DebugContext(ctx, "qdb: flushing gas data to QuestDB")
	return w.client.Flush(ctx)
}

// Close closes the shared QuestDB client.
func (w *QuestDBGasWriter) Close() error {
	w.client.Close()
	return nil
}
