package multisink_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockDucoSink struct {
	boxErr   error
	nodeErr  error
	flushErr error
	closeErr error
	box      int
	node     int
	flushed  int
	closed   int
}

func (m *mockDucoSink) StoreBoxStatus(_ context.Context, _ domain.DucoBoxStatus) error {
	m.box++
	return m.boxErr
}

func (m *mockDucoSink) StoreNodeData(_ context.Context, _ domain.DucoNodeStatus) error {
	m.node++
	return m.nodeErr
}

func (m *mockDucoSink) Flush(_ context.Context) error {
	m.flushed++
	return m.flushErr
}

func (m *mockDucoSink) Close() error {
	m.closed++
	return m.closeErr
}

func TestDucoRepository_StoreBoxStatus_AllSucceed(t *testing.T) {
	s1 := &mockDucoSink{}
	s2 := &mockDucoSink{}
	repo := multisink.NewDucoRepository([]domain.DucoRepository{s1, s2}, testLogger())

	if err := repo.StoreBoxStatus(context.Background(), domain.DucoBoxStatus{}); err != nil {
		t.Errorf("StoreBoxStatus: %v", err)
	}
	if s1.box != 1 || s2.box != 1 {
		t.Errorf("expected both sinks stored once; got s1=%d s2=%d", s1.box, s2.box)
	}
}

func TestDucoRepository_StoreBoxStatus_OneFails(t *testing.T) {
	s1 := &mockDucoSink{boxErr: errors.New("duco box error")}
	s2 := &mockDucoSink{}
	repo := multisink.NewDucoRepository([]domain.DucoRepository{s1, s2}, testLogger())

	err := repo.StoreBoxStatus(context.Background(), domain.DucoBoxStatus{})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if s2.box != 1 {
		t.Errorf("s2 should have been called; box=%d", s2.box)
	}
}

func TestDucoRepository_StoreNodeData(t *testing.T) {
	s1 := &mockDucoSink{}
	repo := multisink.NewDucoRepository([]domain.DucoRepository{s1}, testLogger())

	if err := repo.StoreNodeData(context.Background(), domain.DucoRFSensorStatus{}); err != nil {
		t.Errorf("StoreNodeData: %v", err)
	}
	if s1.node != 1 {
		t.Errorf("expected node stored once; got %d", s1.node)
	}
}

func TestDucoRepository_Flush(t *testing.T) {
	s1 := &mockDucoSink{}
	s2 := &mockDucoSink{}
	repo := multisink.NewDucoRepository([]domain.DucoRepository{s1, s2}, testLogger())
	if err := repo.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if s1.flushed != 1 || s2.flushed != 1 {
		t.Errorf("expected both sinks flushed once; got s1=%d s2=%d", s1.flushed, s2.flushed)
	}
}

func TestDucoRepository_Close(t *testing.T) {
	s1 := &mockDucoSink{}
	s2 := &mockDucoSink{}
	repo := multisink.NewDucoRepository([]domain.DucoRepository{s1, s2}, testLogger())
	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if s1.closed != 1 || s2.closed != 1 {
		t.Errorf("expected both sinks closed once; got s1=%d s2=%d", s1.closed, s2.closed)
	}
}

func TestDucoRepository_Panic_EmptySinks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty sinks")
		}
	}()
	multisink.NewDucoRepository([]domain.DucoRepository{}, testLogger())
}
