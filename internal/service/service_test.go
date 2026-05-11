package service

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	readErr error
	done    chan struct{}
}

func (m *mockGridReader) ReadGridTelegrams(ctx context.Context) error {
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
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
	ch := make(chan domain.GridTelegram, 1)
	done := make(chan struct{})
	reader := &mockGridReader{done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Second, ch, testLogger())
	if svc == nil {
		t.Error("NewGridLoggingService() returned nil")
	}
	close(done)
}

func TestGridLoggingService_Start_ContextCancel(t *testing.T) {
	ch := make(chan domain.GridTelegram, 10)
	done := make(chan struct{})
	reader := &mockGridReader{done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, ch, testLogger())

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
	ch := make(chan domain.GridTelegram, 10)
	done := make(chan struct{})
	reader := &mockGridReader{done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, ch, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	ch <- domain.GridTelegram{MeterMerkType: "ISK"}
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(done)

	repo.mu.Lock()
	count := len(repo.stored)
	repo.mu.Unlock()
	if count == 0 {
		t.Error("GridLoggingService did not store any telegrams")
	}
}

func TestGridLoggingService_StoreData_LogsSuccessfulDataFlow(t *testing.T) {
	var logOutput bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ch := make(chan domain.GridTelegram, 1)
	done := make(chan struct{})
	reader := &mockGridReader{done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, ch, logger)

	if err := svc.storeData(context.Background(), domain.GridTelegram{Serienummer: "grid-meter-1"}); err != nil {
		t.Fatalf("storeData() error = %v", err)
	}
	close(done)

	if !strings.Contains(logOutput.String(), "grid meter data flow started successfully") {
		t.Fatalf("expected successful data flow log, got %q", logOutput.String())
	}
}

func TestGridLoggingService_Start_Flushes(t *testing.T) {
	ch := make(chan domain.GridTelegram, 10)
	done := make(chan struct{})
	reader := &mockGridReader{done: done}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, 10*time.Millisecond, ch, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(done)

	repo.mu.Lock()
	flushed := repo.flushed
	repo.mu.Unlock()
	if flushed == 0 {
		t.Error("GridLoggingService did not flush")
	}
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
		nodeErr:   errors.New("unknown devtype: UNKN"),
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

	done := make(chan struct{})
	go func() {
		svc.Start(t.Context())
		close(done)
	}()

	// Service must call processKiller and exit after maxConsecutiveHeatErrors.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("HeatMeterLoggingService did not exit after too many consecutive errors")
	}
	select {
	case <-killerCalled:
	default:
		t.Error("processKiller was not called")
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

func TestSolarLoggingService_Start_ReadError(_ *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockSolarReader{readErr: errors.New("solar read failure")}
			repo := &mockSolarRepo{}
			svc := NewSolarLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { svc.Start(ctx); close(done) }()
			time.Sleep(50 * time.Millisecond)
			cancel()
			<-done
		},
	)
}

func TestSolarLoggingService_Start_StoreError(_ *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockSolarReader{data: domain.EnvoySolarData{Watt: 100}}
			repo := &mockSolarRepo{storeErr: errors.New("store failure")}
			svc := NewSolarLoggingService(reader, repo, 10*time.Millisecond, time.Hour, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { svc.Start(ctx); close(done) }()
			time.Sleep(50 * time.Millisecond)
			cancel()
			<-done
		},
	)
}

func TestSolarLoggingService_Start_FlushError(_ *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockSolarReader{}
			repo := &mockSolarRepo{flushErr: errors.New("flush failure")}
			svc := NewSolarLoggingService(reader, repo, time.Hour, 10*time.Millisecond, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { svc.Start(ctx); close(done) }()
			time.Sleep(50 * time.Millisecond)
			cancel()
			<-done
		},
	)
}

func TestGridLoggingService_Start_ReadError(_ *testing.T) {
	withNoopKiller(
		func() {
			ch := make(chan domain.GridTelegram, 10)
			readerDone := make(chan struct{})
			reader := &mockGridReader{done: readerDone, readErr: errors.New("grid read failure")}
			repo := &mockGridRepo{}
			svc := NewGridLoggingService(reader, repo, time.Hour, ch, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { svc.Start(ctx); close(done) }()
			close(readerDone) // unblock the reader which then returns an error
			time.Sleep(50 * time.Millisecond)
			cancel()
			<-done
		},
	)
}

func TestGridLoggingService_Start_StoreError(_ *testing.T) {
	withNoopKiller(
		func() {
			ch := make(chan domain.GridTelegram, 10)
			readerDone := make(chan struct{})
			reader := &mockGridReader{done: readerDone}
			repo := &mockGridRepo{storeErr: errors.New("store failure")}
			svc := NewGridLoggingService(reader, repo, time.Hour, ch, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { svc.Start(ctx); close(done) }()
			ch <- domain.GridTelegram{}
			time.Sleep(50 * time.Millisecond)
			cancel()
			close(readerDone)
			<-done
		},
	)
}

func TestGridLoggingService_Start_FlushError(_ *testing.T) {
	withNoopKiller(
		func() {
			ch := make(chan domain.GridTelegram, 10)
			readerDone := make(chan struct{})
			reader := &mockGridReader{done: readerDone}
			repo := &mockGridRepo{flushErr: errors.New("flush failure")}
			svc := NewGridLoggingService(reader, repo, 10*time.Millisecond, ch, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() { svc.Start(ctx); close(done) }()
			time.Sleep(50 * time.Millisecond)
			cancel()
			close(readerDone)
			<-done
		},
	)
}

func TestDucoLoggingService_Start_FlushError(t *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockDucoReader{}
			repo := &mockDucoRepo{flushErr: errors.New("flush failure")}
			svc := NewDucoLoggingService(reader, repo, time.Hour, 10*time.Millisecond, []int{}, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("DucoLoggingService did not return after flush error")
			}
		},
	)
}

func TestDucoLoggingService_Start_StoreBoxError(t *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockDucoReader{boxStatus: domain.DucoBoxStatus{}}
			repo := &mockDucoRepo{storeBoxErr: errors.New("store box failure")}
			svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{}, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("DucoLoggingService did not return after store box error")
			}
		},
	)
}

func TestDucoLoggingService_Start_TooManyBoxErrors(t *testing.T) {
	withNoopKiller(
		func() {
			reader := &mockDucoReader{boxErr: errors.New("persistent failure")}
			repo := &mockDucoRepo{}
			svc := NewDucoLoggingService(reader, repo, time.Millisecond, time.Hour, []int{}, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("DucoLoggingService did not return after too many errors")
			}
		},
	)
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
	ch := make(chan domain.GridTelegram, 1)
	reader := &mockGridReader{}
	repo := &mockGridRepo{}
	svc := NewGridLoggingService(reader, repo, time.Hour, ch, testLogger())
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
	withNoopKiller(
		func() {
			reader := &mockDucoReader{
				boxStatus: domain.DucoBoxStatus{},
				nodeData:  domain.DucoNodeBoxStatus{},
			}
			repo := &mockDucoRepo{storeNodeErr: errors.New("node store failure")}
			svc := NewDucoLoggingService(reader, repo, 10*time.Millisecond, time.Hour, []int{1}, testLogger())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan struct{})
			go func() {
				svc.Start(ctx)
				close(done)
			}()

			select {
			case <-done:
				// processKiller was called - expected behaviour on node store error.
			case <-time.After(2 * time.Second):
				t.Error("DucoLoggingService did not return after node store error")
			}
		},
	)
}
