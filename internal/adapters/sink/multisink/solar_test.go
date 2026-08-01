//nolint:dupl // solar and thermal fan-out tests share the same shape but cover distinct domain types
package multisink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockSolarSink struct {
	storeErr error
	flushErr error
	closeErr error
	stored   int
	flushed  int
	closed   int
}

func (m *mockSolarSink) StoreEnvoySolarData(_ context.Context, _ domain.EnvoySolarData) error {
	m.stored++
	return m.storeErr
}

func (m *mockSolarSink) Flush(_ context.Context) error {
	m.flushed++
	return m.flushErr
}

func (m *mockSolarSink) Close() error {
	m.closed++
	return m.closeErr
}

func TestSolarRepository_StoreEnvoySolarData_AllSucceed(t *testing.T) {
	s1 := &mockSolarSink{}
	s2 := &mockSolarSink{}
	repo := multisink.NewSolarRepository([]domain.EnvoySolarRepository{s1, s2}, testLogger())

	data := domain.EnvoySolarData{ReadingTime: time.Now()}
	if err := repo.StoreEnvoySolarData(context.Background(), data); err != nil {
		t.Errorf("StoreEnvoySolarData: %v", err)
	}
	if s1.stored != 1 || s2.stored != 1 {
		t.Errorf("expected both sinks stored once; got s1=%d s2=%d", s1.stored, s2.stored)
	}
}

func TestSolarRepository_StoreEnvoySolarData_OneFails(t *testing.T) {
	s1 := &mockSolarSink{storeErr: errors.New("solar error")}
	s2 := &mockSolarSink{}
	repo := multisink.NewSolarRepository([]domain.EnvoySolarRepository{s1, s2}, testLogger())

	err := repo.StoreEnvoySolarData(context.Background(), domain.EnvoySolarData{})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if s2.stored != 1 {
		t.Errorf("s2 should have been called; stored=%d", s2.stored)
	}
}

func TestSolarRepository_Flush(t *testing.T) {
	s1 := &mockSolarSink{}
	s2 := &mockSolarSink{}
	repo := multisink.NewSolarRepository([]domain.EnvoySolarRepository{s1, s2}, testLogger())
	if err := repo.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if s1.flushed != 1 || s2.flushed != 1 {
		t.Errorf("expected both sinks flushed once; got s1=%d s2=%d", s1.flushed, s2.flushed)
	}
}

func TestSolarRepository_Close(t *testing.T) {
	s1 := &mockSolarSink{}
	s2 := &mockSolarSink{}
	repo := multisink.NewSolarRepository([]domain.EnvoySolarRepository{s1, s2}, testLogger())
	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if s1.closed != 1 || s2.closed != 1 {
		t.Errorf("expected both sinks closed once; got s1=%d s2=%d", s1.closed, s2.closed)
	}
}

func TestSolarRepository_Panic_EmptySinks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty sinks")
		}
	}()
	multisink.NewSolarRepository([]domain.EnvoySolarRepository{}, testLogger())
}
