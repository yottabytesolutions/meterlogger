package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

// --- Mock implementations ---

type mockHeatReader struct {
	mu      sync.Mutex
	reading domain.HeatTelegram
	readErr error
}

func (m *mockHeatReader) ReadHeatTelegram(_ context.Context) (domain.HeatTelegram, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reading, m.readErr
}

type mockHeatRepo struct {
	mu       sync.Mutex
	stored   []domain.HeatTelegram
	storeErr error
	flushErr error
	flushed  int
}

func (m *mockHeatRepo) StoreHeatTelegram(_ context.Context, t domain.HeatTelegram) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = append(m.stored, t)
	return m.storeErr
}

func (m *mockHeatRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return m.flushErr
}

func (m *mockHeatRepo) Close() error { return nil }

type mockSolarReader struct {
	mu      sync.Mutex
	data    domain.EnvoySolarData
	readErr error
}

func (m *mockSolarReader) ReadEnvoySolarData(_ context.Context) (domain.EnvoySolarData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data, m.readErr
}

type mockSolarRepo struct {
	mu       sync.Mutex
	stored   []domain.EnvoySolarData
	storeErr error
	flushed  int
	flushErr error
}

func (m *mockSolarRepo) StoreEnvoySolarData(_ context.Context, d domain.EnvoySolarData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = append(m.stored, d)
	return m.storeErr
}

func (m *mockSolarRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return m.flushErr
}

func (m *mockSolarRepo) Close() error { return nil }

type mockGridReader struct {
	ch      chan domain.GridTelegram
	readErr error
	done    chan struct{}
}

func (m *mockGridReader) Telegrams() <-chan domain.GridTelegram { return m.ch }

func (m *mockGridReader) ReadGridTelegrams(ctx context.Context) error {
	if m.ch != nil {
		defer close(m.ch)
	}
	if m.done != nil {
		select {
		case <-m.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.readErr
}

type mockGridRepo struct {
	mu       sync.Mutex
	stored   []domain.GridTelegram
	storeErr error
	flushed  int
	flushErr error
}

func (m *mockGridRepo) StoreGridTelegram(_ context.Context, t domain.GridTelegram) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = append(m.stored, t)
	return m.storeErr
}

func (m *mockGridRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return m.flushErr
}

func (m *mockGridRepo) Close() error { return nil }

type mockDucoReader struct {
	mu        sync.Mutex
	boxStatus domain.DucoBoxStatus
	boxErr    error
	nodeData  domain.DucoNodeStatus
	nodeErr   error
}

func (m *mockDucoReader) ReadBoxStatus(_ context.Context) (domain.DucoBoxStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.boxStatus, m.boxErr
}

func (m *mockDucoReader) ReadNodeStatus(_ context.Context, _ int) (domain.DucoNodeStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nodeData, m.nodeErr
}

type mockDucoRepo struct {
	mu           sync.Mutex
	storedBox    []domain.DucoBoxStatus
	storedNodes  []domain.DucoNodeStatus
	storeBoxErr  error
	storeNodeErr error
	flushed      int
	flushErr     error
}

func (m *mockDucoRepo) StoreBoxStatus(_ context.Context, s domain.DucoBoxStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storedBox = append(m.storedBox, s)
	return m.storeBoxErr
}

func (m *mockDucoRepo) StoreNodeData(_ context.Context, d domain.DucoNodeStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storedNodes = append(m.storedNodes, d)
	return m.storeNodeErr
}

func (m *mockDucoRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return m.flushErr
}

func (m *mockDucoRepo) Close() error { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// waitFor polls cond every few milliseconds until it returns true or a one
// second timeout expires, in which case the test fails with msg.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

// --- HeatMeterLoggingService tests ---

func TestNewHeatMeterLoggingService(t *testing.T) {
	reader := &mockHeatReader{}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, time.Second, time.Second, testLogger())
	if svc == nil {
		t.Error("NewHeatMeterLoggingService() returned nil")
	}
}

func TestHeatMeterLoggingService_Start_ContextCancel(t *testing.T) {
	reader := &mockHeatReader{reading: domain.HeatTelegram{MeterID: "m1"}}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, time.Hour, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("HeatMeterLoggingService.Start() did not stop on context cancel")
	}
}

func TestHeatMeterLoggingService_Start_StoresTelegram(t *testing.T) {
	telegram := domain.HeatTelegram{MeterID: "heat-meter-1", Joules: 5000}
	reader := &mockHeatReader{reading: telegram}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	count := len(repo.stored)
	repo.mu.Unlock()
	if count == 0 {
		t.Error("HeatMeterLoggingService did not store any telegrams")
	}
}

func TestHeatMeterLoggingService_RunReadAndStore_LogsSuccessfulDataFlow(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	telegram := domain.HeatTelegram{MeterID: "heat-meter-1", SerialNo: "serial-1", Joules: 5000}
	reader := &mockHeatReader{reading: telegram}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, time.Hour, time.Hour, logger)

	if err := svc.runReadAndStore(context.Background()); err != nil {
		t.Fatalf("runReadAndStore() error = %v", err)
	}

	if !strings.Contains(logOutput.String(), "heat meter data flow started successfully") {
		t.Fatalf("expected successful data flow log, got %q", logOutput.String())
	}
}

func TestHeatMeterLoggingService_Start_Flushes(t *testing.T) {
	reader := &mockHeatReader{reading: domain.HeatTelegram{MeterID: "m1"}}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, time.Hour, 10*time.Millisecond, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	flushed := repo.flushed
	repo.mu.Unlock()
	if flushed == 0 {
		t.Error("HeatMeterLoggingService did not flush")
	}
}

// --- SolarLoggingService tests ---

func TestNewSolarLoggingService(t *testing.T) {
	reader := &mockSolarReader{}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, time.Second, time.Second, testLogger())
	if svc == nil {
		t.Error("NewSolarLoggingService() returned nil")
	}
}

func TestSolarLoggingService_Start_ContextCancel(t *testing.T) {
	reader := &mockSolarReader{data: domain.EnvoySolarData{Watt: 250}}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, time.Hour, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("SolarLoggingService.Start() did not stop on context cancel")
	}
}

func TestSolarLoggingService_Start_StoresData(t *testing.T) {
	reader := &mockSolarReader{data: domain.EnvoySolarData{Watt: 300}}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	count := len(repo.stored)
	repo.mu.Unlock()
	if count == 0 {
		t.Error("SolarLoggingService did not store any data")
	}
}

func TestSolarLoggingService_RunReadAndStore_LogsSuccessfulDataFlow(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reader := &mockSolarReader{data: domain.EnvoySolarData{Watt: 300}}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, time.Hour, time.Hour, logger)

	if err := svc.runReadAndStore(context.Background()); err != nil {
		t.Fatalf("runReadAndStore() error = %v", err)
	}

	if !strings.Contains(logOutput.String(), "solar data flow started successfully") {
		t.Fatalf("expected successful data flow log, got %q", logOutput.String())
	}
}

func TestSolarLoggingService_Start_Flushes(t *testing.T) {
	reader := &mockSolarReader{data: domain.EnvoySolarData{}}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, time.Hour, 10*time.Millisecond, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	flushed := repo.flushed
	repo.mu.Unlock()
	if flushed == 0 {
		t.Error("SolarLoggingService did not flush")
	}
}

// --- GridLoggingService tests ---

func TestNewGridLoggingService(t *testing.T) {
	done := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1), done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Second, testLogger())
	if svc == nil {
		t.Error("NewGridLoggingService() returned nil")
	}
	close(done)
}

func TestGridLoggingService_Start_ContextCancel(t *testing.T) {
	done := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 10), done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(stopped)
	}()

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Error("GridLoggingService.Start() did not stop on context cancel")
	}
	close(done)
}

func TestGridLoggingService_Start_StoresTelegram(t *testing.T) {
	done := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 10), done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	reader.ch <- domain.GridTelegram{MeterMerkType: "ISK"}
	waitFor(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.stored) > 0
	}, "GridLoggingService did not store any telegrams")
	cancel()
	close(done)
}

func TestGridLoggingService_StoreData_LogsSuccessfulDataFlow(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	done := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1), done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, logger)

	if err := svc.storeData(context.Background(), domain.GridTelegram{Serienummer: "grid-meter-1"}); err != nil {
		t.Fatalf("storeData() error = %v", err)
	}
	close(done)

	if !strings.Contains(logOutput.String(), "grid meter data flow started successfully") {
		t.Fatalf("expected successful data flow log, got %q", logOutput.String())
	}
}

func TestGridLoggingService_Start_Flushes(t *testing.T) {
	done := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 10), done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, 10*time.Millisecond, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	waitFor(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.flushed > 0
	}, "GridLoggingService did not flush")
	cancel()
	close(done)
}

// --- DucoLoggingService tests ---

func TestNewDucoLoggingService(t *testing.T) {
	reader := &mockDucoReader{}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, time.Second, time.Second, []int{}, testLogger())
	if svc == nil {
		t.Error("NewDucoLoggingService() returned nil")
	}
}

func TestDucoLoggingService_Start_ContextCancel(t *testing.T) {
	reader := &mockDucoReader{}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, time.Hour, time.Hour, []int{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("DucoLoggingService.Start() did not stop on context cancel")
	}
}

func TestDucoLoggingService_Start_StoresBoxStatus(t *testing.T) {
	boxStatus := domain.DucoBoxStatus{}
	reader := &mockDucoReader{boxStatus: boxStatus}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	count := len(repo.storedBox)
	repo.mu.Unlock()
	if count == 0 {
		t.Error("DucoLoggingService did not store any box status")
	}
}

func TestDucoLoggingService_RunReadAndStore_LogsSuccessfulDataFlow(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reader := &mockDucoReader{boxStatus: domain.DucoBoxStatus{}}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, time.Hour, time.Hour, []int{}, logger)

	if err := svc.runReadAndStore(context.Background()); err != nil {
		t.Fatalf("runReadAndStore() error = %v", err)
	}

	if !strings.Contains(logOutput.String(), "ventilation data flow started successfully") {
		t.Fatalf("expected successful data flow log, got %q", logOutput.String())
	}
}

func TestDucoLoggingService_Start_Flushes(t *testing.T) {
	reader := &mockDucoReader{}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, time.Hour, 10*time.Millisecond, []int{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	flushed := repo.flushed
	repo.mu.Unlock()
	if flushed == 0 {
		t.Error("DucoLoggingService did not flush")
	}
}

func TestDucoLoggingService_Start_WithNodes(t *testing.T) {
	boxStatus := domain.DucoBoxStatus{}
	nodeData := domain.DucoNodeBoxStatus{
		BaseDucoNodeStatus: domain.BaseDucoNodeStatus{Node: 1, DevType: "BOX"},
	}
	reader := &mockDucoReader{
		boxStatus: boxStatus,
		nodeData:  nodeData,
	}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{1, 2}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	repo.mu.Lock()
	nodeCount := len(repo.storedNodes)
	repo.mu.Unlock()
	if nodeCount == 0 {
		t.Error("DucoLoggingService did not store any node data")
	}
}

func TestDucoLoggingService_Start_BoxReadError(t *testing.T) {
	reader := &mockDucoReader{
		boxErr: errors.New("connection refused"),
	}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	// Should not have stored anything
	repo.mu.Lock()
	count := len(repo.storedBox)
	repo.mu.Unlock()
	if count != 0 {
		t.Errorf("DucoLoggingService stored %d box statuses on error, want 0", count)
	}
}

func TestDucoLoggingService_Start_NodeErrorUNKN(t *testing.T) {
	boxStatus := domain.DucoBoxStatus{}
	reader := &mockDucoReader{
		boxStatus: boxStatus,
		nodeErr:   fmt.Errorf("%w: UNKN", domain.ErrUnknownDevType),
	}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{1}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	// Node errors with UNKN suffix should be silently skipped
	repo.mu.Lock()
	nodeCount := len(repo.storedNodes)
	repo.mu.Unlock()
	if nodeCount != 0 {
		t.Errorf("DucoLoggingService stored %d nodes despite UNKN error", nodeCount)
	}
}

func TestDucoLoggingService_Start_NodeErrorNonUNKN(t *testing.T) {
	boxStatus := domain.DucoBoxStatus{}
	reader := &mockDucoReader{
		boxStatus: boxStatus,
		nodeErr:   errors.New("connection timeout"),
	}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{1}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)

	// Non-UNKN errors should be logged but not crash the service
	repo.mu.Lock()
	boxCount := len(repo.storedBox)
	repo.mu.Unlock()
	if boxCount == 0 {
		t.Error("DucoLoggingService should still store box status despite node error")
	}
}

// --- Error path tests using mocked processKiller ---

func withNoopKiller(fn func()) {
	orig := processKiller
	processKiller = func() {}
	defer func() { processKiller = orig }()
	fn()
}

// Heat service retries on transient errors but exits after maxConsecutiveHeatErrors.

func TestHeatMeterLoggingService_Start_ReadError(t *testing.T) {
	killerCalled := make(chan struct{}, 1)
	orig := processKiller
	processKiller = func() { killerCalled <- struct{}{} }
	defer func() { processKiller = orig }()

	reader := &mockHeatReader{readErr: errors.New("read failure")}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	// Service must call processKiller after maxConsecutiveErrors, then block until
	// ctx is cancelled (mirroring the real SIGTERM-then-ctx.Done() sequence).
	select {
	case <-killerCalled:
	case <-time.After(time.Second):
		t.Fatal("processKiller was not called after too many consecutive errors")
	}

	select {
	case <-done:
		t.Error("HeatMeterLoggingService returned before ctx was cancelled")
	default:
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("HeatMeterLoggingService did not exit after ctx cancellation")
	}
}

func TestHeatMeterLoggingService_Start_StoreError(t *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockHeatReader{reading: domain.HeatTelegram{MeterID: "m1"}}
			repo := &mockHeatRepo{storeErr: errors.New("store failure")}
			svc := NewHeatMeterLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			// Sleep for less than maxConsecutiveHeatErrors*interval (5*10ms=50ms) to
			// verify the service survives individual store errors without exiting early.
			time.Sleep(25 * time.Millisecond)
			select {
			case <-done:
				t.Error("HeatMeterLoggingService exited early on store error")
			default:
			}

			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("HeatMeterLoggingService did not stop after context cancel")
			}
		},
	)
}

func TestHeatMeterLoggingService_Start_FlushError(t *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockHeatReader{reading: domain.HeatTelegram{MeterID: "m1"}}
			repo := &mockHeatRepo{flushErr: errors.New("flush failure")}
			svc := NewHeatMeterLoggingService(reader, repo, time.Hour, 10*time.Millisecond, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			time.Sleep(50 * time.Millisecond)
			select {
			case <-done:
				t.Error("HeatMeterLoggingService exited early on flush error")
			default:
			}

			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("HeatMeterLoggingService did not stop after context cancel")
			}
		},
	)
}

// Consecutive read errors must escalate via processKiller after the threshold,
// with nothing stored.
func TestSolarLoggingService_Start_ReadError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		reader := &mockSolarReader{readErr: errors.New("solar read failure")}
		repo := &mockSolarRepo{}
		svc := NewSolarLoggingService(reader, repo, time.Millisecond, time.Hour, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()

		select {
		case <-killerCalled:
		case <-time.After(2 * time.Second):
			t.Fatal("processKiller was not called after consecutive read errors")
		}

		repo.mu.Lock()
		count := len(repo.stored)
		repo.mu.Unlock()
		if count != 0 {
			t.Errorf("SolarLoggingService stored %d readings on read errors, want 0", count)
		}

		cancel()
		<-done
	})
}

// Every failing store attempt is recorded; after maxConsecutiveErrors of them
// the service escalates via processKiller.
func TestSolarLoggingService_Start_StoreError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		reader := &mockSolarReader{data: domain.EnvoySolarData{Watt: 100}}
		repo := &mockSolarRepo{storeErr: errors.New("store failure")}
		svc := NewSolarLoggingService(reader, repo, time.Millisecond, time.Hour, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()

		select {
		case <-killerCalled:
		case <-time.After(2 * time.Second):
			t.Fatal("processKiller was not called after consecutive store errors")
		}

		repo.mu.Lock()
		count := len(repo.stored)
		repo.mu.Unlock()
		if count < maxConsecutiveErrors {
			t.Errorf("store attempts = %d, want at least %d before escalation", count, maxConsecutiveErrors)
		}

		cancel()
		<-done
	})
}

// Solar service escalates via processKiller after maxConsecutiveErrors
// consecutive store failures, then blocks until ctx is cancelled.
func TestSolarLoggingService_Start_StoreErrorEscalates(t *testing.T) {
	killerCalled := make(chan struct{}, 1)
	orig := processKiller
	processKiller = func() {
		select {
		case killerCalled <- struct{}{}:
		default:
		}
	}
	defer func() { processKiller = orig }()

	reader := &mockSolarReader{data: domain.EnvoySolarData{Watt: 100}}
	repo := &mockSolarRepo{storeErr: errors.New("store failure")}
	svc := NewSolarLoggingService(reader, repo, time.Millisecond, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	select {
	case <-killerCalled:
	case <-time.After(time.Second):
		t.Fatal("processKiller was not called after too many consecutive store errors")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("SolarLoggingService did not return after ctx cancellation")
	}
}

// Flush errors are logged but never escalate: processKiller must not fire and
// the service keeps flushing.
func TestSolarLoggingService_Start_FlushError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		reader := &mockSolarReader{}
		repo := &mockSolarRepo{flushErr: errors.New("flush failure")}
		svc := NewSolarLoggingService(reader, repo, time.Hour, 10*time.Millisecond, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()

		waitFor(t, func() bool {
			repo.mu.Lock()
			defer repo.mu.Unlock()
			return repo.flushed >= 2
		}, "SolarLoggingService stopped flushing after a flush error")

		select {
		case <-killerCalled:
			t.Error("processKiller fired on flush errors; flush errors must not escalate")
		default:
		}

		cancel()
		<-done
	})
}

// A reader error must escalate via processKiller from the reader goroutine.
func TestGridLoggingService_Start_ReadError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		readerDone := make(chan struct{})
		reader := &mockGridReader{
			ch:      make(chan domain.GridTelegram, 10),
			done:    readerDone,
			readErr: errors.New("grid read failure"),
		}
		repo := &mockGridRepo{}
		svc := NewGridLoggingService(reader, repo, time.Hour, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()
		close(readerDone) // unblock the reader which then returns an error

		select {
		case <-killerCalled:
		case <-time.After(2 * time.Second):
			t.Fatal("processKiller was not called after grid reader error")
		}

		cancel()
		<-done
	})
}

// A single store error below the threshold is tolerated: the attempt is
// recorded and processKiller does not fire.
func TestGridLoggingService_Start_StoreError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		readerDone := make(chan struct{})
		reader := &mockGridReader{ch: make(chan domain.GridTelegram, 10), done: readerDone}
		repo := &mockGridRepo{storeErr: errors.New("store failure")}
		svc := NewGridLoggingService(reader, repo, time.Hour, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()

		reader.ch <- domain.GridTelegram{}
		waitFor(t, func() bool {
			repo.mu.Lock()
			defer repo.mu.Unlock()
			return len(repo.stored) == 1
		}, "GridLoggingService did not attempt to store the telegram")

		select {
		case <-killerCalled:
			t.Error("processKiller fired on a single store error below the threshold")
		default:
		}

		cancel()
		close(readerDone)
		<-done
	})
}

// Grid service escalates via processKiller after maxConsecutiveErrors
// consecutive store failures, then blocks until ctx is cancelled.
func TestGridLoggingService_Start_StoreErrorEscalates(t *testing.T) {
	killerCalled := make(chan struct{}, 1)
	orig := processKiller
	processKiller = func() {
		select {
		case killerCalled <- struct{}{}:
		default:
		}
	}
	defer func() { processKiller = orig }()

	readerDone := make(chan struct{})
	reader := &mockGridReader{
		ch:   make(chan domain.GridTelegram, maxConsecutiveErrors),
		done: readerDone,
	}
	repo := &mockGridRepo{storeErr: errors.New("store failure")}
	svc := NewGridLoggingService(reader, repo, time.Hour, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	for range maxConsecutiveErrors {
		reader.ch <- domain.GridTelegram{}
	}

	select {
	case <-killerCalled:
	case <-time.After(time.Second):
		t.Fatal("processKiller was not called after too many consecutive store errors")
	}

	cancel()
	close(readerDone)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("GridLoggingService did not return after ctx cancellation")
	}
}

// Flush errors are logged but never escalate: processKiller must not fire and
// the service keeps flushing.
func TestGridLoggingService_Start_FlushError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		readerDone := make(chan struct{})
		reader := &mockGridReader{ch: make(chan domain.GridTelegram, 10), done: readerDone}
		repo := &mockGridRepo{flushErr: errors.New("flush failure")}
		svc := NewGridLoggingService(reader, repo, 10*time.Millisecond, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { svc.Start(ctx); close(done) }()

		waitFor(t, func() bool {
			repo.mu.Lock()
			defer repo.mu.Unlock()
			return repo.flushed >= 2
		}, "GridLoggingService stopped flushing after a flush error")

		select {
		case <-killerCalled:
			t.Error("processKiller fired on flush errors; flush errors must not escalate")
		default:
		}

		cancel()
		close(readerDone)
		<-done
	})
}

// Duco flush errors escalate only after maxConsecutiveErrors consecutive
// failures, matching the tolerant policy of the other services.
func TestDucoLoggingService_Start_FlushError(t *testing.T) {
	killerCalled := make(chan struct{}, 1)
	orig := processKiller
	processKiller = func() { killerCalled <- struct{}{} }
	defer func() { processKiller = orig }()

	reader := &mockDucoReader{}
	repo := &mockDucoRepo{flushErr: errors.New("flush failure")}
	svc := NewDucoLoggingService(reader, repo, time.Hour, 10*time.Millisecond, []int{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	select {
	case <-killerCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("processKiller was not called after consecutive flush errors")
	}

	repo.mu.Lock()
	flushed := repo.flushed
	repo.mu.Unlock()
	if flushed < maxConsecutiveErrors {
		t.Errorf("flush attempts = %d, want at least %d before escalation", flushed, maxConsecutiveErrors)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("DucoLoggingService did not return after ctx cancellation")
	}
}

// withSafeKillerSignal replaces processKiller with a variant that signals
// killerCalled (non-blocking, so repeated calls before the service actually
// stops never deadlock the service loop) and restores the original after fn
// returns.
func withSafeKillerSignal(fn func(killerCalled <-chan struct{})) {
	killerCalled := make(chan struct{}, 1)
	orig := processKiller
	processKiller = func() {
		select {
		case killerCalled <- struct{}{}:
		default:
		}
	}
	defer func() { processKiller = orig }()
	fn(killerCalled)
}

func TestDucoLoggingService_Start_StoreBoxError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		reader := &mockDucoReader{boxStatus: domain.DucoBoxStatus{}}
		repo := &mockDucoRepo{storeBoxErr: errors.New("store box failure")}
		svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{}, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			svc.Start(ctx)
			close(done)
		}()

		select {
		case <-killerCalled:
		case <-time.After(2 * time.Second):
			t.Fatal("processKiller was not called after store box error")
		}

		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("DucoLoggingService did not return after ctx cancellation")
		}
	})
}

func TestDucoLoggingService_Start_TooManyBoxErrors(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		reader := &mockDucoReader{boxErr: errors.New("persistent failure")}
		repo := &mockDucoRepo{}
		svc := NewDucoLoggingService(reader, repo, time.Millisecond, time.Hour, []int{}, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			svc.Start(ctx)
			close(done)
		}()

		select {
		case <-killerCalled:
		case <-time.After(5 * time.Second):
			t.Fatal("processKiller was not called after too many errors")
		}

		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("DucoLoggingService did not return after ctx cancellation")
		}
	})
}

// --- WithMetrics tests ---

func TestHeatMeterLoggingService_WithMetrics(t *testing.T) {
	reader := &mockHeatReader{reading: domain.HeatTelegram{MeterID: "m1"}}
	repo := &mockHeatRepo{}
	svc := NewHeatMeterLoggingService(reader, repo, time.Hour, time.Hour, testLogger())
	m := metrics.New()
	got := svc.WithMetrics(m)
	if got != svc {
		t.Error("WithMetrics should return the same service pointer")
	}
}

func TestSolarLoggingService_WithMetrics(t *testing.T) {
	reader := &mockSolarReader{}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, time.Hour, time.Hour, testLogger())
	m := metrics.New()
	got := svc.WithMetrics(m)
	if got != svc {
		t.Error("WithMetrics should return the same service pointer")
	}
}

func TestGridLoggingService_WithMetrics(t *testing.T) {
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1)}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, testLogger())
	m := metrics.New()
	got := svc.WithMetrics(m)
	if got != svc {
		t.Error("WithMetrics should return the same service pointer")
	}
}

func TestDucoLoggingService_WithMetrics(t *testing.T) {
	reader := &mockDucoReader{}
	repo := &mockDucoRepo{}
	svc := NewDucoLoggingService(reader, repo, time.Hour, time.Hour, []int{}, testLogger())
	m := metrics.New()
	got := svc.WithMetrics(m)
	if got != svc {
		t.Error("WithMetrics should return the same service pointer")
	}
}

// WithMetrics: verify metrics are incremented during service operation.
func TestHeatMeterLoggingService_Start_MetricsIncremented(t *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockHeatReader{reading: domain.HeatTelegram{MeterID: "m1"}}
			repo := &mockHeatRepo{}
			m := metrics.New()
			svc := NewHeatMeterLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger()).
				WithMetrics(m)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			time.Sleep(60 * time.Millisecond)
			cancel()
			<-done

			// At least one read and write should have been recorded.
			reads := testutil.ToFloat64(m.ReadsTotal.WithLabelValues("heat"))
			if reads < 1 {
				t.Errorf("expected reads_total >= 1, got %v", reads)
			}
		},
	)
}

// TestDucoLoggingService_NodeStoreError exercises the node store error path in runReadAndStore.
func TestDucoLoggingService_NodeStoreError(t *testing.T) {
	withSafeKillerSignal(func(killerCalled <-chan struct{}) {
		reader := &mockDucoReader{
			boxStatus: domain.DucoBoxStatus{},
			nodeData:  domain.DucoNodeBoxStatus{},
		}
		repo := &mockDucoRepo{storeNodeErr: errors.New("node store failure")}
		svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{1}, testLogger())

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			svc.Start(ctx)
			close(done)
		}()

		select {
		case <-killerCalled:
			// processKiller was called - expected behaviour on node store error.
		case <-time.After(2 * time.Second):
			t.Fatal("processKiller was not called after node store error")
		}

		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("DucoLoggingService did not return after ctx cancellation")
		}
	})
}
