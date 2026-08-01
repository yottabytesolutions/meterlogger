package stdout

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func newTestStore() *Store {
	return NewStdoutStore(slog.New(slog.DiscardHandler))
}

func TestStoreHeatTelegram(t *testing.T) {
	store := newTestStore()
	telegram := domain.HeatTelegram{
		MeterID:  "test-meter",
		SerialNo: "12345",
		Joules:   1000,
	}
	err := store.StoreHeatTelegram(context.Background(), telegram)
	if err != nil {
		t.Errorf("StoreHeatTelegram() unexpected error: %v", err)
	}
}

func TestStoreGridTelegram(t *testing.T) {
	store := newTestStore()
	telegram := domain.GridTelegram{
		MeterMerkType: "ISK",
		Serienummer:   "00112233",
		UsageCounter1: 100.5,
	}
	err := store.StoreGridTelegram(context.Background(), telegram)
	if err != nil {
		t.Errorf("StoreGridTelegram() unexpected error: %v", err)
	}
}

func TestStoreGasReading(t *testing.T) {
	store := newTestStore()
	reading := domain.GasReading{
		SerialNo:  "4730303339",
		ReadingM3: 1234.567,
	}
	err := store.StoreGasReading(context.Background(), reading)
	if err != nil {
		t.Errorf("StoreGasReading() unexpected error: %v", err)
	}
}

func TestStoreEnvoySolarData(t *testing.T) {
	store := newTestStore()
	data := domain.EnvoySolarData{
		ProductionWh: 5000,
		Watt:         250.0,
		PanelCount:   10,
	}
	err := store.StoreEnvoySolarData(context.Background(), data)
	if err != nil {
		t.Errorf("StoreEnvoySolarData() unexpected error: %v", err)
	}
}

func TestNewStdoutStore(t *testing.T) {
	store := NewStdoutStore(slog.New(slog.DiscardHandler))
	if store == nil {
		t.Error("NewStdoutStore() returned nil")
	}
}

func TestStdoutStore_AllMethods(t *testing.T) {
	s := newTestStore()
	ctx := context.Background()

	if err := s.StoreHeatTelegram(ctx, domain.HeatTelegram{Timestamp: time.Now()}); err != nil {
		t.Errorf("StoreHeatTelegram: %v", err)
	}
	if err := s.StoreGridTelegram(ctx, domain.GridTelegram{}); err != nil {
		t.Errorf("StoreGridTelegram: %v", err)
	}
	if err := s.StoreGasReading(ctx, domain.GasReading{CapturedAt: time.Now()}); err != nil {
		t.Errorf("StoreGasReading: %v", err)
	}
	if err := s.StoreEnvoySolarData(ctx, domain.EnvoySolarData{}); err != nil {
		t.Errorf("StoreEnvoySolarData: %v", err)
	}
	if err := s.StoreBoxStatus(ctx, domain.DucoBoxStatus{}); err != nil {
		t.Errorf("StoreBoxStatus: %v", err)
	}
	if err := s.StoreNodeData(ctx, domain.DucoRFSensorStatus{}); err != nil {
		t.Errorf("StoreNodeData: %v", err)
	}
	if err := s.Flush(ctx); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
