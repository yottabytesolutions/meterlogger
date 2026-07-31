package multisink_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/multisink"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockGridSink struct {
	storeErr error
	flushErr error
	closeErr error
	stored   int
	flushed  int
	closed   int
}

func (m *mockGridSink) StoreGridTelegram(_ context.Context, _ domain.GridTelegram) error {
	m.stored++
	return m.storeErr
}

func (m *mockGridSink) Flush(_ context.Context) error {
	m.flushed++
	return m.flushErr
}

func (m *mockGridSink) Close() error {
	m.closed++
	return m.closeErr
}

func TestGridRepository_StoreGridTelegram_AllSucceed(t *testing.T) {
	s1 := &mockGridSink{}
	s2 := &mockGridSink{}
	repo := multisink.NewGridRepository([]domain.GridTelegramRepository{s1, s2}, testLogger())

	if err := repo.StoreGridTelegram(context.Background(), domain.GridTelegram{Time: time.Now()}); err != nil {
		t.Errorf("StoreGridTelegram: %v", err)
	}
	if s1.stored != 1 || s2.stored != 1 {
		t.Errorf("expected both sinks stored once; got s1=%d s2=%d", s1.stored, s2.stored)
	}
}

func TestGridRepository_StoreGridTelegram_OneFails(t *testing.T) {
	s1 := &mockGridSink{storeErr: errors.New("grid error")}
	s2 := &mockGridSink{}
	repo := multisink.NewGridRepository([]domain.GridTelegramRepository{s1, s2}, testLogger())

	err := repo.StoreGridTelegram(context.Background(), domain.GridTelegram{})
	if err == nil {
		t.Error("expected error, got nil")
	}
	if s2.stored != 1 {
		t.Errorf("s2 should have been called; stored=%d", s2.stored)
	}
}

func TestGridRepository_Flush(t *testing.T) {
	s1 := &mockGridSink{}
	s2 := &mockGridSink{}
	repo := multisink.NewGridRepository([]domain.GridTelegramRepository{s1, s2}, testLogger())
	if err := repo.Flush(context.Background()); err != nil {
		t.Errorf("Flush: %v", err)
	}
	if s1.flushed != 1 || s2.flushed != 1 {
		t.Errorf("expected both sinks flushed once; got s1=%d s2=%d", s1.flushed, s2.flushed)
	}
}

func TestGridRepository_Close(t *testing.T) {
	s1 := &mockGridSink{}
	s2 := &mockGridSink{}
	repo := multisink.NewGridRepository([]domain.GridTelegramRepository{s1, s2}, testLogger())
	if err := repo.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if s1.closed != 1 || s2.closed != 1 {
		t.Errorf("expected both sinks closed once; got s1=%d s2=%d", s1.closed, s2.closed)
	}
}

func TestGridRepository_Panic_EmptySinks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty sinks")
		}
	}()
	multisink.NewGridRepository([]domain.GridTelegramRepository{}, testLogger())
}
