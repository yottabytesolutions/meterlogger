package stdout

import (
	"context"
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// Store is a debug sink that logs all data to stdout. Not for production use.
type Store struct {
	logger *slog.Logger
}

// NewStdoutStore creates a new stdout Store using the provided logger.
func NewStdoutStore(logger *slog.Logger) *Store {
	return &Store{logger: logger}
}

// StoreHeatTelegram logs a heat telegram to stdout.
func (s *Store) StoreHeatTelegram(ctx context.Context, telegram domain.HeatTelegram) error {
	s.logger.DebugContext(ctx, "heat telegram received",
		slog.String("meterID", telegram.MeterID),
		slog.Int64("power", telegram.ActualPower),
		slog.Int64("energy", telegram.Joules),
	)
	return nil
}

// StoreGridTelegram logs a grid telegram to stdout.
func (s *Store) StoreGridTelegram(ctx context.Context, t domain.GridTelegram) error {
	s.logger.DebugContext(ctx, "grid telegram received",
		slog.String("serial", t.Serienummer),
		slog.Int("powerUsage", t.TotalPowerUsage),
	)
	return nil
}

// StoreEnvoySolarData logs solar data to stdout.
func (s *Store) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	s.logger.DebugContext(ctx, "solar data received",
		slog.String("serial", d.EnvoySerial),
		slog.Float64("watt", d.Watt),
	)
	return nil
}

// StoreBoxStatus logs duco box status to stdout.
func (s *Store) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	s.logger.DebugContext(ctx, "duco box status received", slog.String("rfHomeID", b.General.RFHomeID))
	return nil
}

// StoreNodeData logs duco node data to stdout.
func (s *Store) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	s.logger.DebugContext(ctx, "duco node data received", slog.Any("data", nodeData))
	return nil
}

// Flush is a no-op for the stdout sink.
func (s *Store) Flush(_ context.Context) error { return nil }

// Close is a no-op for the stdout sink.
func (s *Store) Close() error { return nil }
