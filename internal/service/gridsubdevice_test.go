package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

type mockWaterRepo struct {
	mu       sync.Mutex
	stored   []domain.WaterReading
	storeErr error
	flushed  int
	closed   bool
}

func (m *mockWaterRepo) StoreWaterReading(_ context.Context, r domain.WaterReading) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storeErr != nil {
		return m.storeErr
	}
	m.stored = append(m.stored, r)
	return nil
}

func (m *mockWaterRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return nil
}

func (m *mockWaterRepo) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockWaterRepo) storedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stored)
}

type mockThermalRepo struct {
	mu       sync.Mutex
	stored   []domain.ThermalReading
	storeErr error
	flushed  int
	closed   bool
}

func (m *mockThermalRepo) StoreThermalReading(_ context.Context, r domain.ThermalReading) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.storeErr != nil {
		return m.storeErr
	}
	m.stored = append(m.stored, r)
	return nil
}

func (m *mockThermalRepo) Flush(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushed++
	return nil
}

func (m *mockThermalRepo) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockThermalRepo) storedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stored)
}

const subdeviceTestSerial = "subdevice-serial-1"

// subdeviceTelegram builds a telegram carrying one M-Bus subdevice reading.
func subdeviceTelegram(
	channel, deviceType int, capturedAt time.Time, value float64, unit string,
) domain.GridTelegram {
	return domain.GridTelegram{
		Time: capturedAt.Add(30 * time.Second),
		MBusDevices: []domain.MBusDeviceReading{{
			Channel:    channel,
			DeviceType: deviceType,
			SerialNo:   subdeviceTestSerial,
			CapturedAt: capturedAt,
			Value:      value,
			Unit:       unit,
		}},
	}
}

func newSubdeviceTestService() *GridLoggingService {
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1)}
	return NewGridLoggingService(reader, &mockGridRepo{}, time.Hour, testLogger())
}

func TestGridLoggingService_StoreWaterReadings_RoutedAndDeduped(t *testing.T) {
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)
	tests := []struct {
		name       string
		deviceType int
	}{
		{name: "water", deviceType: domain.DeviceTypeWater},
		{name: "warm water", deviceType: domain.DeviceTypeWaterWarm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			water := &mockWaterRepo{}
			svc := newSubdeviceTestService().WithWater(water)
			telegram := subdeviceTelegram(2, tt.deviceType, capturedAt, 872.234, "m3")

			// The same capture repeats in many telegrams; only the first one stores.
			for range 3 {
				if failed := svc.storeMBusReadings(context.Background(), telegram); failed != 0 {
					t.Fatalf("storeMBusReadings() failed = %d, want 0", failed)
				}
			}
			if water.storedCount() != 1 {
				t.Fatalf("stored %d water readings, want 1", water.storedCount())
			}
			got := water.stored[0]
			want := domain.WaterReading{
				CapturedAt: capturedAt,
				ReceivedAt: telegram.Time,
				Channel:    2,
				DeviceType: tt.deviceType,
				SerialNo:   subdeviceTestSerial,
				ReadingM3:  872.234,
			}
			if got != want {
				t.Errorf("stored reading = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGridLoggingService_StoreThermalReadings_RoutedAndDeduped(t *testing.T) {
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)
	tests := []struct {
		name       string
		deviceType int
	}{
		{name: "heat", deviceType: domain.DeviceTypeHeat},
		{name: "cooling outlet", deviceType: domain.DeviceTypeCoolingOutlet},
		{name: "cooling inlet", deviceType: domain.DeviceTypeCoolingInlet},
		{name: "heat cool combined", deviceType: domain.DeviceTypeHeatCool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thermal := &mockThermalRepo{}
			svc := newSubdeviceTestService().WithThermal(thermal)
			telegram := subdeviceTelegram(3, tt.deviceType, capturedAt, 12.345, "GJ")

			for range 3 {
				if failed := svc.storeMBusReadings(context.Background(), telegram); failed != 0 {
					t.Fatalf("storeMBusReadings() failed = %d, want 0", failed)
				}
			}
			if thermal.storedCount() != 1 {
				t.Fatalf("stored %d thermal readings, want 1", thermal.storedCount())
			}
			got := thermal.stored[0]
			want := domain.ThermalReading{
				CapturedAt: capturedAt,
				ReceivedAt: telegram.Time,
				Channel:    3,
				DeviceType: tt.deviceType,
				SerialNo:   subdeviceTestSerial,
				ReadingGJ:  12.345,
			}
			if got != want {
				t.Errorf("stored reading = %+v, want %+v", got, want)
			}
		})
	}
}

func TestGridLoggingService_StoreSubdeviceReadings_NewCaptureStored(t *testing.T) {
	water := &mockWaterRepo{}
	svc := newSubdeviceTestService().WithWater(water)
	first := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)

	svc.storeMBusReadings(context.Background(),
		subdeviceTelegram(2, domain.DeviceTypeWater, first, 1.0, "m3"))
	svc.storeMBusReadings(context.Background(),
		subdeviceTelegram(2, domain.DeviceTypeWater, first.Add(5*time.Minute), 1.1, "m3"))

	if water.storedCount() != 2 {
		t.Errorf("stored %d water readings, want 2", water.storedCount())
	}
}

func TestGridLoggingService_StoreSubdeviceReadings_UnitMismatchSkipped(t *testing.T) {
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)
	water := &mockWaterRepo{}
	thermal := &mockThermalRepo{}
	svc := newSubdeviceTestService().WithWater(water).WithThermal(thermal)

	// Water must report m3 and thermal must report GJ; anything else is a
	// warn-and-skip, never a failure.
	waterTelegram := subdeviceTelegram(2, domain.DeviceTypeWater, capturedAt, 1.0, "GJ")
	if failed := svc.storeMBusReadings(context.Background(), waterTelegram); failed != 0 {
		t.Errorf("storeMBusReadings() failed = %d, want 0 for a skipped unit mismatch", failed)
	}
	thermalTelegram := subdeviceTelegram(3, domain.DeviceTypeHeat, capturedAt, 1.0, "m3")
	if failed := svc.storeMBusReadings(context.Background(), thermalTelegram); failed != 0 {
		t.Errorf("storeMBusReadings() failed = %d, want 0 for a skipped unit mismatch", failed)
	}
	if water.storedCount() != 0 || thermal.storedCount() != 0 {
		t.Errorf("stored water=%d thermal=%d readings, want 0 for unit mismatch",
			water.storedCount(), thermal.storedCount())
	}
}

func TestGridLoggingService_StoreSubdeviceReadings_NilReposSafe(t *testing.T) {
	svc := newSubdeviceTestService()
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)

	// Water and thermal devices with no repos attached: skipped with one log
	// per channel, never a failure.
	for range 2 {
		waterTelegram := subdeviceTelegram(2, domain.DeviceTypeWater, capturedAt, 1.0, "m3")
		if failed := svc.storeMBusReadings(context.Background(), waterTelegram); failed != 0 {
			t.Errorf("storeMBusReadings() failed = %d, want 0 with nil water repo", failed)
		}
		thermalTelegram := subdeviceTelegram(3, domain.DeviceTypeHeat, capturedAt, 1.0, "GJ")
		if failed := svc.storeMBusReadings(context.Background(), thermalTelegram); failed != 0 {
			t.Errorf("storeMBusReadings() failed = %d, want 0 with nil thermal repo", failed)
		}
	}
	if !svc.skipLogged[2] || !svc.skipLogged[3] {
		t.Error("skipped channels 2 and 3 were not marked as logged")
	}
}

func TestGridLoggingService_StoreSubdeviceReadings_SlaveEMeterSkipped(t *testing.T) {
	water := &mockWaterRepo{}
	thermal := &mockThermalRepo{}
	svc := newSubdeviceTestService().WithWater(water).WithThermal(thermal)
	telegram := subdeviceTelegram(4, domain.DeviceTypeSlaveEMeter, time.Now(), 5.0, "kWh")

	// A slave e-meter is documented as read-from-its-own-port; both passes
	// must stay silent skips.
	for range 2 {
		if failed := svc.storeMBusReadings(context.Background(), telegram); failed != 0 {
			t.Fatalf("storeMBusReadings() failed = %d, want 0", failed)
		}
	}
	if water.storedCount() != 0 || thermal.storedCount() != 0 {
		t.Errorf("stored water=%d thermal=%d readings, want 0 for a slave e-meter",
			water.storedCount(), thermal.storedCount())
	}
	if !svc.skipLogged[4] {
		t.Error("slave e-meter channel 4 was not marked as logged")
	}
}

// Failed water and thermal stores count toward the shared consecutive-error
// handling and leave the dedup key unset so the capture is retried.
func TestGridLoggingService_HandleStore_SubdeviceStoreErrorCounts(t *testing.T) {
	orig := processKiller
	processKiller = func() {}
	defer func() { processKiller = orig }()

	water := &mockWaterRepo{storeErr: errors.New("water store failure")}
	svc := newSubdeviceTestService().WithWater(water)
	capturedAt := time.Date(2024, 1, 2, 12, 5, 0, 0, time.UTC)
	telegram := subdeviceTelegram(2, domain.DeviceTypeWater, capturedAt, 1.0, "m3")

	consecutiveErrors := 0
	if stop := svc.handleStore(context.Background(), telegram, &consecutiveErrors); stop {
		t.Fatal("handleStore() = true before reaching maxConsecutiveErrors")
	}
	if consecutiveErrors != 1 {
		t.Fatalf("consecutiveErrors = %d, want 1", consecutiveErrors)
	}

	// The repo recovers; the same capture must now store and reset the counter.
	water.mu.Lock()
	water.storeErr = nil
	water.mu.Unlock()
	if stop := svc.handleStore(context.Background(), telegram, &consecutiveErrors); stop {
		t.Fatal("handleStore() = true after water repo recovered")
	}
	if consecutiveErrors != 0 {
		t.Errorf("consecutiveErrors = %d after recovery, want 0", consecutiveErrors)
	}
	if water.storedCount() != 1 {
		t.Errorf("stored %d water readings after recovery, want 1", water.storedCount())
	}
}

func TestGridLoggingService_Start_FlushesSubdeviceRepos(t *testing.T) {
	water := &mockWaterRepo{}
	thermal := &mockThermalRepo{}
	readerDone := make(chan struct{})
	reader := &mockGridReader{ch: make(chan domain.GridTelegram, 1), done: readerDone}
	svc := NewGridLoggingService(reader, &mockGridRepo{}, 10*time.Millisecond, testLogger()).
		WithWater(water).
		WithThermal(thermal)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	waitFor(t, func() bool {
		water.mu.Lock()
		defer water.mu.Unlock()
		return water.flushed >= 1
	}, "GridLoggingService did not flush the water repo")
	waitFor(t, func() bool {
		thermal.mu.Lock()
		defer thermal.mu.Unlock()
		return thermal.flushed >= 1
	}, "GridLoggingService did not flush the thermal repo")

	cancel()
	close(readerDone)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GridLoggingService did not return after ctx cancellation")
	}

	// The composition root owns the repo lifetimes: the cmd supervisor
	// restarts Start after transient exits, so the service must not close
	// repositories it will use again.
	water.mu.Lock()
	waterClosed := water.closed
	water.mu.Unlock()
	if waterClosed {
		t.Error("service must not close the water repo; the composition root owns it")
	}
	thermal.mu.Lock()
	thermalClosed := thermal.closed
	thermal.mu.Unlock()
	if thermalClosed {
		t.Error("service must not close the thermal repo; the composition root owns it")
	}
}
