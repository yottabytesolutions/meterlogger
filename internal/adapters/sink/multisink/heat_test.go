package multisink_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type mockHeatSink struct {
	storeErr error
	flushErr error
	closeErr error
	stored   int
	flushed  int
	closed   int
}

func (m *mockHeatSink) StoreHeatTelegram(_ context.Context, _ domain.HeatTelegram) error {
	m.stored++
	return m.storeErr
}

func (m *mockHeatSink) Flush(_ context.Context) error {
	m.flushed++
	return m.flushErr
}

func (m *mockHeatSink) Close() error {
	m.closed++
	return m.closeErr
}

func TestHeatRepository_StoreHeatTelegram_AllSucceed(t *testing.T) {
	s1 := &mockHeatSink{}
	s2 := &mockHeatSink{}
	repo := multisink.NewHeatRepository([]domain.HeatMeterRepository{s1, s2}, testLogger())

	if err := repo.StoreHeatTelegram(context.Background(), domain.HeatTelegram{Timestamp: time.Now()}); err != nil {
		t.Errorf("StoreHeatTelegram: %v", err)
	}
	if s1.stored != 1 || s2.stored != 1 {
		t.Errorf("expected both sinks stored once; got s1=%d s2=%d", s1.stored, s2.stored)
	}
}

func TestHeatRepository_StoreHeatTelegram_OneFails(t *testing.T) {
	s1 := &mockHeatSink{storeErr: errors.New("sink1 error")}
	s2 := &mockHeatSink{}
	repo := multisink.NewHeatRepository([]domain.HeatMeterRepository{s1, s2}, testLogger())

	err := repo.StoreHeatTelegram(context.Background(), domain.HeatTelegram{})
	if err == nil {
		t.Error("expected error, got nil")
	}
	// s2 should still have been called.
	if s2.stored != 1 {
		t.Errorf("s2 should have been called; stored=%d", s2.stored)
	}
}

func TestHeatRepository_Flush(t *testing.T) {
	s1 := &mockHeatSink{}
	s2 := &mockHeatSink{}
	repo := multisink.NewHeatRepository([]domain.HeatMeterRepository{s1, s2}, testLogger())

	if err := repo.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if s1.flushed != 1 || s2.flushed != 1 {
		t.Errorf("expected both sinks flushed once; got s1=%d s2=%d", s1.flushed, s2.flushed)
	}
}

func TestHeatRepository_Close(t *testing.T) {
	s1 := &mockHeatSink{}
	s2 := &mockHeatSink{}
	repo := multisink.NewHeatRepository([]domain.HeatMeterRepository{s1, s2}, testLogger())

	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if s1.closed != 1 || s2.closed != 1 {
		t.Errorf("expected both sinks closed once; got s1=%d s2=%d", s1.closed, s2.closed)
	}
}

func TestHeatRepository_Panic_EmptySinks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty sinks")
		}
	}()
	multisink.NewHeatRepository([]domain.HeatMeterRepository{}, testLogger())
}
