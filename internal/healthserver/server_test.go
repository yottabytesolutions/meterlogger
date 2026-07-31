package healthserver_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

const (
	testThreshold   = 90 * time.Second
	testCheckerName = "questdb"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type healthyChecker struct{ name string }

func (h *healthyChecker) Name() string                  { return h.name }
func (h *healthyChecker) Check(_ context.Context) error { return nil }

type unhealthyChecker struct{ name string }

func (u *unhealthyChecker) Name() string                  { return u.name }
func (u *unhealthyChecker) Check(_ context.Context) error { return errors.New("connection refused") }

// flakyChecker reports unhealthy iff its flag is true. Tests in this file
// drive it from the same goroutine as the probe call, so no synchronisation
// is needed.
type flakyChecker struct {
	name      string
	unhealthy bool
}

func (f *flakyChecker) Name() string { return f.name }

func (f *flakyChecker) Check(_ context.Context) error {
	if f.unhealthy {
		return errors.New("flaky: down")
	}
	return nil
}

// slowChecker blocks until its context expires, simulating a hung DB ping.
type slowChecker struct{ name string }

func (s *slowChecker) Name() string { return s.name }

func (s *slowChecker) Check(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// ctxAwareChecker is healthy, but honours context cancellation the way a real
// DB ping does: with an already-expired context it fails instead of checking.
type ctxAwareChecker struct{ name string }

func (c *ctxAwareChecker) Name() string { return c.name }

func (c *ctxAwareChecker) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func newGetRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	return req
}

func newServerWithClock(t *testing.T, threshold time.Duration, now func() time.Time) *healthserver.Server {
	t.Helper()
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), threshold)
	if now != nil {
		healthserver.SetNow(srv, now)
	}
	return srv
}

func TestLiveness_NoCheckers(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)
	req := newGetRequest(t, "/healthz")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("body: %s", w.Body.String())
	}
}

func TestLiveness_HealthyChecker(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)
	srv.Register(&healthyChecker{name: "postgres"})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/healthz"))
	if w.Code != http.StatusOK {
		t.Errorf("healthy checker: want 200, got %d", w.Code)
	}
}

// TestLiveness_TransientFailureStaysGreen is the regression test for the
// stuck-pod bug: a checker that just started failing must not immediately
// trip /healthz, otherwise we restart pods on every transient blip.
func TestLiveness_TransientFailureStaysGreen(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	srv := newServerWithClock(t, testThreshold, clock)
	srv.Register(&unhealthyChecker{name: testCheckerName})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/healthz"))
	if w.Code != http.StatusOK {
		t.Errorf("transient failure: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestLiveness_SustainedFailureTrips is the corresponding fix verification:
// once the failure has persisted past the threshold, /healthz must return
// 503 so the kubelet restarts the container.
func TestLiveness_SustainedFailureTrips(t *testing.T) {
	current := time.Now()
	clock := func() time.Time { return current }
	srv := newServerWithClock(t, testThreshold, clock)
	srv.Register(&unhealthyChecker{name: testCheckerName})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/healthz"))
	if w.Code != http.StatusOK {
		t.Fatalf("first probe: want 200, got %d", w.Code)
	}

	current = current.Add(testThreshold + time.Second)

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/healthz"))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("after threshold: want 503, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"stuck"`) {
		t.Errorf("body should contain stuck: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), testCheckerName) {
		t.Errorf("body should name failing checker: %s", w.Body.String())
	}
}

// TestLiveness_RecoveryClearsState ensures that once a checker recovers, the
// failure timer resets so a fresh blip later does not immediately trip
// liveness.
func TestLiveness_RecoveryClearsState(t *testing.T) {
	current := time.Now()
	clock := func() time.Time { return current }
	srv := newServerWithClock(t, testThreshold, clock)

	flaky := &flakyChecker{name: testCheckerName}
	flaky.unhealthy = true
	srv.Register(flaky)

	srv.ServeHTTP(httptest.NewRecorder(), newGetRequest(t, "/healthz"))

	flaky.unhealthy = false
	current = current.Add(testThreshold + time.Second)
	srv.ServeHTTP(httptest.NewRecorder(), newGetRequest(t, "/healthz"))

	flaky.unhealthy = true
	current = current.Add(time.Second)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/healthz"))
	if w.Code != http.StatusOK {
		t.Errorf("post-recovery transient failure: want 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestReadiness_FlipsImmediately verifies that /readyz, unlike /healthz,
// does not wait for the threshold; a single failure flips it to 503.
func TestReadiness_FlipsImmediately(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)
	srv.Register(&unhealthyChecker{name: testCheckerName})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/readyz"))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "degraded") {
		t.Errorf("body should contain degraded: %s", w.Body.String())
	}
}

func TestReadiness_AllHealthy(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)
	srv.Register(&healthyChecker{name: "postgres"})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/readyz"))
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestReadiness_NoCheckers(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/readyz"))
	if w.Code != http.StatusOK {
		t.Errorf("no checkers: want 200, got %d", w.Code)
	}
}

// TestReadiness_SlowCheckerDoesNotStarveOthers guards the per-checker timeout:
// a checker that hangs for its full budget must not leave the next checker
// with an expired context, so a healthy sink is still reported healthy.
func TestReadiness_SlowCheckerDoesNotStarveOthers(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)
	healthserver.SetCheckTimeout(srv, 20*time.Millisecond)
	srv.Register(&slowChecker{name: "slow-sink"})
	srv.Register(&ctxAwareChecker{name: "healthy-sink"})

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, newGetRequest(t, "/readyz"))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("slow checker should degrade readiness: want 503, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `{"name":"slow-sink","healthy":false`) {
		t.Errorf("slow checker should be unhealthy; body: %s", body)
	}
	if !strings.Contains(body, `{"name":"healthy-sink","healthy":true}`) {
		t.Errorf("healthy checker must not be starved by the slow one; body: %s", body)
	}
}

func TestServer_StartsAndShuts(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry(), testThreshold)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, err := srv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp, err := http.Get("http://" + addr + "/healthz") //nolint:noctx // integration test; context not needed
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}

	cancel()
	srv.Wait()
}
