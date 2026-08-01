//nolint:dupl // gas and grid fan-out tests share the same shape but cover distinct domain types
package multisink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockGasSink struct {
	storeErr error
	flushErr error
	closeErr error
	stored   int
	flushed  int
	closed   int
}

func (m *mockGasSink) StoreGasReading(_ context.Context, _ domain.GasReading) error {
	m.stored++
	return m.storeErr
}

func (m *mockGasSink) Flush(_ context.Context) error {
	m.flushed++
	return m.flushErr
}

func (m *mockGasSink) Close() error {
	m.closed++
	return m.closeErr
}

func TestGasRepository_StoreGasReading_AllSucceed(t *testing.T) {
	s1 := &mockGasSink{}
	s2 := &mockGasSink{}
	repo := multisink.NewGasRepository([]domain.GasRepository{s1, s2}, testLogger())

	if err := repo.StoreGasReading(context.Background(), domain.GasReading{CapturedAt: time.Now()}); err != nil {
		t.Errorf("StoreGasReading: %v", err)
	}
	if s1.stored != 1 || s2.stored != 1 {
		t.Errorf("expected both sinks stored once; got s1=%d s2=%d", s1.stored, s2.stored)
	}
}

func TestGasRepository_StoreGasReading_OneFails(t *testing.T) {
	s1 := &mockGasSink{storeErr: errors.New("gas error")}
	s2 := &mockGasSink{}
	repo := multisink.NewGasRepository([]domain.GasRepository{s1, s2}, testLogger())

	err := repo.StoreGasReading(context.Background(), domain.GasReading{})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if s2.stored != 1 {
		t.Errorf("s2 should have been called; stored=%d", s2.stored)
	}
}

func TestGasRepository_Flush(t *testing.T) {
	s1 := &mockGasSink{}
	s2 := &mockGasSink{}
	repo := multisink.NewGasRepository([]domain.GasRepository{s1, s2}, testLogger())
	if err := repo.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if s1.flushed != 1 || s2.flushed != 1 {
		t.Errorf("expected both sinks flushed once; got s1=%d s2=%d", s1.flushed, s2.flushed)
	}
}

func TestGasRepository_Close(t *testing.T) {
	s1 := &mockGasSink{}
	s2 := &mockGasSink{}
	repo := multisink.NewGasRepository([]domain.GasRepository{s1, s2}, testLogger())
	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if s1.closed != 1 || s2.closed != 1 {
		t.Errorf("expected both sinks closed once; got s1=%d s2=%d", s1.closed, s2.closed)
	}
}

func TestGasRepository_Panic_EmptySinks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty sinks")
		}
	}()
	multisink.NewGasRepository([]domain.GasRepository{}, testLogger())
}
