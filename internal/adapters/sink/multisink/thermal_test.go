//nolint:dupl // thermal and gas fan-out tests share the same shape but cover distinct domain types
package multisink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockThermalSink struct {
	storeErr error
	flushErr error
	closeErr error
	stored   int
	flushed  int
	closed   int
}

func (m *mockThermalSink) StoreThermalReading(_ context.Context, _ domain.ThermalReading) error {
	m.stored++
	return m.storeErr
}

func (m *mockThermalSink) Flush(_ context.Context) error {
	m.flushed++
	return m.flushErr
}

func (m *mockThermalSink) Close() error {
	m.closed++
	return m.closeErr
}

func TestThermalRepository_StoreThermalReading_AllSucceed(t *testing.T) {
	s1 := &mockThermalSink{}
	s2 := &mockThermalSink{}
	repo := multisink.NewThermalRepository([]domain.ThermalRepository{s1, s2}, testLogger())

	reading := domain.ThermalReading{CapturedAt: time.Now()}
	if err := repo.StoreThermalReading(context.Background(), reading); err != nil {
		t.Errorf("StoreThermalReading: %v", err)
	}
	if s1.stored != 1 || s2.stored != 1 {
		t.Errorf("expected both sinks stored once; got s1=%d s2=%d", s1.stored, s2.stored)
	}
}

func TestThermalRepository_StoreThermalReading_OneFails(t *testing.T) {
	s1 := &mockThermalSink{storeErr: errors.New("thermal error")}
	s2 := &mockThermalSink{}
	repo := multisink.NewThermalRepository([]domain.ThermalRepository{s1, s2}, testLogger())

	err := repo.StoreThermalReading(context.Background(), domain.ThermalReading{})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if s2.stored != 1 {
		t.Errorf("s2 should have been called; stored=%d", s2.stored)
	}
}

func TestThermalRepository_Flush(t *testing.T) {
	s1 := &mockThermalSink{}
	s2 := &mockThermalSink{}
	repo := multisink.NewThermalRepository([]domain.ThermalRepository{s1, s2}, testLogger())
	if err := repo.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if s1.flushed != 1 || s2.flushed != 1 {
		t.Errorf("expected both sinks flushed once; got s1=%d s2=%d", s1.flushed, s2.flushed)
	}
}

func TestThermalRepository_Close(t *testing.T) {
	s1 := &mockThermalSink{}
	s2 := &mockThermalSink{}
	repo := multisink.NewThermalRepository([]domain.ThermalRepository{s1, s2}, testLogger())
	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if s1.closed != 1 || s2.closed != 1 {
		t.Errorf("expected both sinks closed once; got s1=%d s2=%d", s1.closed, s2.closed)
	}
}

func TestThermalRepository_Panic_EmptySinks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty sinks")
		}
	}()
	multisink.NewThermalRepository([]domain.ThermalRepository{}, testLogger())
}
