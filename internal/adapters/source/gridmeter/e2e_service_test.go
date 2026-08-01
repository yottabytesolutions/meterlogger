package gridmeter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/service"
)

// This file wires the real Fluvius telegram fixture through the grid reader
// into the grid logging service, covering the reader-to-repository path for
// the water subdevice on channel 2.

// eofTolerantReader wraps a GridReader whose port input is a finite string:
// it swallows the trailing io.EOF and blocks until ctx is cancelled so the
// service treats the end of input as a clean shutdown instead of escalating.
type eofTolerantReader struct {
	inner *GridReader
}

func (r *eofTolerantReader) Telegrams() <-chan domain.GridTelegram { return r.inner.Telegrams() }

func (r *eofTolerantReader) ReadGridTelegrams(ctx context.Context) error {
	if err := r.inner.ReadGridTelegrams(ctx); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

type e2eGridRepo struct{}

func (e2eGridRepo) StoreGridTelegram(_ context.Context, _ domain.GridTelegram) error { return nil }
func (e2eGridRepo) Flush(_ context.Context) error                                    { return nil }
func (e2eGridRepo) Close() error                                                     { return nil }

type e2eWaterRepo struct {
	mu     sync.Mutex
	stored []domain.WaterReading
}

func (m *e2eWaterRepo) StoreWaterReading(_ context.Context, r domain.WaterReading) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stored = append(m.stored, r)
	return nil
}

func (m *e2eWaterRepo) Flush(_ context.Context) error { return nil }
func (m *e2eWaterRepo) Close() error                  { return nil }

func (m *e2eWaterRepo) readings() []domain.WaterReading {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.WaterReading(nil), m.stored...)
}

// recordingHandler captures log messages so the test can count skip lines.
type recordingHandler struct {
	mu   sync.Mutex
	msgs []string
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, r.Message)
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingHandler) countContaining(needle string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.msgs {
		if strings.Contains(m, needle) {
			n++
		}
	}
	return n
}

// runE2EService runs the grid service over the given telegram input until
// check returns true or a timeout expires.
func runE2EService(t *testing.T, svc *service.GridLoggingService, check func() bool, timeoutMsg string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Start(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !check() {
		t.Error(timeoutMsg)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("grid service did not stop after ctx cancellation")
	}
}

func TestEndToEnd_FluviusWaterReadingStoredWhenEnabled(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(fluviusTelegram)
	water := &e2eWaterRepo{}
	svc := service.NewGridLoggingService(
		&eofTolerantReader{inner: reader}, e2eGridRepo{}, time.Hour, testLogger(),
	).WithWater(water)

	runE2EService(t, svc, func() bool { return len(water.readings()) >= 1 },
		"water reading from the Fluvius telegram was not stored")

	got := water.readings()
	if len(got) != 1 {
		t.Fatalf("stored %d water readings, want 1", len(got))
	}
	r := got[0]
	if r.Channel != 2 || r.DeviceType != domain.DeviceTypeWater {
		t.Errorf("stored reading = channel %d type %d, want channel 2 water (7)", r.Channel, r.DeviceType)
	}
	if r.ReadingM3 != 872.234 {
		t.Errorf("ReadingM3 = %v, want 872.234", r.ReadingM3)
	}
	if r.SerialNo != "3853414731323334353637383930" {
		t.Errorf("SerialNo = %q, want the 0-2:96.1.1 value", r.SerialNo)
	}
	wantCapturedAt := time.Date(2020, 5, 12, 13, 45, 58, 0, time.FixedZone("CEST", 2*60*60))
	if !r.CapturedAt.Equal(wantCapturedAt) {
		t.Errorf("CapturedAt = %v, want %v", r.CapturedAt, wantCapturedAt)
	}
}

func TestEndToEnd_FluviusWaterSkippedWithOneLogWhenDisabled(t *testing.T) {
	handler := &recordingHandler{}
	// The same telegram twice: the skip must be logged only once per channel.
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(fluviusTelegram + fluviusTelegram)

	stored := 0
	var mu sync.Mutex
	gridRepo := countingGridRepo{count: func() {
		mu.Lock()
		stored++
		mu.Unlock()
	}}
	svc := service.NewGridLoggingService(
		&eofTolerantReader{inner: reader}, gridRepo, time.Hour, slog.New(handler),
	)

	runE2EService(t, svc, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return stored >= 2
	}, "both Fluvius telegrams were not processed")

	if n := handler.countContaining("water storage not enabled"); n != 1 {
		t.Errorf("water skip logged %d times, want exactly 1", n)
	}
}

type countingGridRepo struct {
	count func()
}

func (r countingGridRepo) StoreGridTelegram(_ context.Context, _ domain.GridTelegram) error {
	r.count()
	return nil
}
func (r countingGridRepo) Flush(_ context.Context) error { return nil }
func (r countingGridRepo) Close() error                  { return nil }
