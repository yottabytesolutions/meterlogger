package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockGasRepo struct {
	mu       sync.Mutex
	stored   []domain.GasReading
	storeErr error
	flushed  int
	flushErr error
	closed   bool
}

func (m *mockGasRepo) StoreGasReading(_ context.Context, r domain.GasReading) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storeErr != nil {
		return m.storeErr
	}
	m.stored = append(m.stored, r)
	return nil
}

func (m *mockGasRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return m.flushErr
}

func (m *mockGasRepo) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockGasRepo) storedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stored)
}

// gasTelegram builds a telegram carrying a single gas subdevice reading.
func gasTelegram(capturedAt time.Time, value float64, unit string) domain.GridTelegram {
	return domain.GridTelegram{
		Time: capturedAt.Add(30 * time.Second),
		MBusDevices: []domain.MBusDeviceReading{{
			Channel:    1,
			DeviceType: domain.DeviceTypeGas,
			SerialNo:   "gas-serial-1",
			CapturedAt: capturedAt,
			Value:      value,
			Unit:       unit,
		}},
	}
}

func newGasTestService(gas domain.GasRepository) (*GridLoggingService, *mockGridRepo) {
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1)}
	repo := &mockGridRepo{}
	return NewGridLoggingService(reader, repo, time.Hour, testLogger()).WithGas(gas), repo
}

func TestGridLoggingService_StoreGasReadings_DedupsByCapturedAt(t *testing.T) {
	gas := &mockGasRepo{}
	svc, _ := newGasTestService(gas)
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)
	telegram := gasTelegram(capturedAt, 1234.567, "m3")

	// The same capture repeats in many telegrams; only the first one stores.
	for range 3 {
		if failed := svc.storeMBusReadings(context.Background(), telegram); failed != 0 {
			t.Fatalf("storeMBusReadings() failed = %d, want 0", failed)
		}
	}
	if gas.storedCount() != 1 {
		t.Fatalf("stored %d gas readings, want 1", gas.storedCount())
	}

	got := gas.stored[0]
	want := domain.GasReading{
		CapturedAt: capturedAt,
		ReceivedAt: telegram.Time,
		Channel:    1,
		DeviceType: domain.DeviceTypeGas,
		SerialNo:   "gas-serial-1",
		ReadingM3:  1234.567,
	}
	if got != want {
		t.Errorf("stored reading = %+v, want %+v", got, want)
	}
}

func TestGridLoggingService_StoreGasReadings_NewCaptureStored(t *testing.T) {
	gas := &mockGasRepo{}
	svc, _ := newGasTestService(gas)
	first := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)

	svc.storeMBusReadings(context.Background(), gasTelegram(first, 1.0, "m3"))
	svc.storeMBusReadings(context.Background(), gasTelegram(first.Add(5*time.Minute), 1.1, "m3"))

	if gas.storedCount() != 2 {
		t.Errorf("stored %d gas readings, want 2", gas.storedCount())
	}
}

func TestGridLoggingService_StoreGasReadings_UnitMismatchSkipped(t *testing.T) {
	gas := &mockGasRepo{}
	svc, _ := newGasTestService(gas)
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)

	failed := svc.storeMBusReadings(context.Background(), gasTelegram(capturedAt, 1.0, "GJ"))
	if failed != 0 {
		t.Errorf("storeMBusReadings() failed = %d, want 0 for a skipped unit mismatch", failed)
	}
	if gas.storedCount() != 0 {
		t.Errorf("stored %d gas readings, want 0 for unit mismatch", gas.storedCount())
	}
}

func TestGridLoggingService_StoreGasReadings_NilRepoSafe(t *testing.T) {
	svc, _ := newGasTestService(nil)
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)

	if failed := svc.storeMBusReadings(context.Background(), gasTelegram(capturedAt, 1.0, "m3")); failed != 0 {
		t.Errorf("storeMBusReadings() failed = %d, want 0 with nil gas repo", failed)
	}
}

func TestGridLoggingService_StoreGasReadings_NonGasDeviceIgnored(t *testing.T) {
	gas := &mockGasRepo{}
	svc, _ := newGasTestService(gas)
	telegram := domain.GridTelegram{
		MBusDevices: []domain.MBusDeviceReading{{
			Channel:    2,
			DeviceType: 7, // water
			CapturedAt: time.Now(),
			Value:      5.0,
			Unit:       "m3",
		}},
	}

	// Repeat to exercise the log-once path; both passes must stay silent skips.
	for range 2 {
		if failed := svc.storeMBusReadings(context.Background(), telegram); failed != 0 {
			t.Fatalf("storeMBusReadings() failed = %d, want 0", failed)
		}
	}
	if gas.storedCount() != 0 {
		t.Errorf("stored %d gas readings, want 0 for non-gas device", gas.storedCount())
	}
	if !svc.skipLogged[2] {
		t.Error("non-gas channel 2 was not marked as logged")
	}
}

// A failed gas store counts toward the shared consecutive-error handling and
// leaves the dedup key unset so the capture is retried.
func TestGridLoggingService_HandleStore_GasStoreErrorCounts(t *testing.T) {
	orig := processKiller
	processKiller = func() {}
	defer func() { processKiller = orig }()

	gas := &mockGasRepo{storeErr: errors.New("gas store failure")}
	svc, gridRepo := newGasTestService(gas)
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)
	telegram := gasTelegram(capturedAt, 1.0, "m3")

	consecutiveErrors := 0
	for want := 1; want <= 2; want++ {
		if stop := svc.handleStore(context.Background(), telegram, &consecutiveErrors); stop {
			t.Fatal("handleStore() = true before reaching maxConsecutiveErrors")
		}
		if consecutiveErrors != want {
			t.Fatalf("consecutiveErrors = %d, want %d", consecutiveErrors, want)
		}
	}
	if len(gridRepo.stored) != 2 {
		t.Errorf("grid telegrams stored = %d, want 2 despite gas failures", len(gridRepo.stored))
	}

	// The repo recovers; the same capture must now store and reset the counter.
	gas.mu.Lock()
	gas.storeErr = nil
	gas.mu.Unlock()
	if stop := svc.handleStore(context.Background(), telegram, &consecutiveErrors); stop {
		t.Fatal("handleStore() = true after gas repo recovered")
	}
	if consecutiveErrors != 0 {
		t.Errorf("consecutiveErrors = %d after recovery, want 0", consecutiveErrors)
	}
	if gas.storedCount() != 1 {
		t.Errorf("stored %d gas readings after recovery, want 1", gas.storedCount())
	}
}

func TestGridLoggingService_HandleStore_GasErrorsEscalate(t *testing.T) {
	killerCalled := false
	orig := processKiller
	processKiller = func() { killerCalled = true }
	defer func() { processKiller = orig }()

	gas := &mockGasRepo{storeErr: errors.New("gas store failure")}
	svc, _ := newGasTestService(gas)
	telegram := gasTelegram(time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC), 1.0, "m3")

	// handleStore blocks on ctx.Done after escalating, so use a cancelled ctx.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	consecutiveErrors := maxConsecutiveErrors - 1
	if stop := svc.handleStore(ctx, telegram, &consecutiveErrors); !stop {
		t.Error("handleStore() = false, want true at maxConsecutiveErrors")
	}
	if !killerCalled {
		t.Error("processKiller was not called at maxConsecutiveErrors")
	}
}

func TestGridLoggingService_Start_FlushesGasRepo(t *testing.T) {
	gas := &mockGasRepo{}
	readerDone := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1), done: readerDone}
	svc := NewGridLoggingService(reader, &mockGridRepo{}, 10*time.Millisecond, testLogger()).WithGas(gas)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	waitFor(t, func() bool {
		gas.mu.Lock()
		defer gas.mu.Unlock()
		return gas.flushed >= 1
	}, "GridLoggingService did not flush the gas repo")

	cancel()
	close(readerDone)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GridLoggingService did not return after ctx cancellation")
	}

	// The composition root owns the repo lifetime: the cmd supervisor
	// restarts Start after transient exits, so the service must not close
	// a repository it will use again.
	gas.mu.Lock()
	defer gas.mu.Unlock()
	if gas.closed {
		t.Error("service must not close the gas repo; the composition root owns it")
	}
}
