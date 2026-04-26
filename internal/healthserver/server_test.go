package healthserver_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type healthyChecker struct{ name string }

func (h *healthyChecker) Name() string                  { return h.name }
func (h *healthyChecker) Check(_ context.Context) error { return nil }

type unhealthyChecker struct{ name string }

func (u *unhealthyChecker) Name() string                  { return u.name }
func (u *unhealthyChecker) Check(_ context.Context) error { return errors.New("connection refused") }

func newGetRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	return req
}

func TestLiveness(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry())
	req := newGetRequest(t, "/healthz")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Errorf("body: %s", w.Body.String())
	}
}

func TestReadiness_AllHealthy(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry())
	srv.Register(&healthyChecker{name: "postgres"})
	req := newGetRequest(t, "/readyz")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestReadiness_Degraded(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry())
	srv.Register(&healthyChecker{name: "pg"})
	srv.Register(&unhealthyChecker{name: "mysql"})
	req := newGetRequest(t, "/readyz")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "degraded") {
		t.Errorf("body should contain 'degraded': %s", w.Body.String())
	}
}

func TestReadiness_NoCheckers(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry())
	req := newGetRequest(t, "/readyz")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("no checkers: want 200, got %d", w.Code)
	}
}

func TestServer_StartsAndShuts(t *testing.T) {
	srv := healthserver.New(":0", testLogger(), prometheus.NewRegistry())
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
	time.Sleep(100 * time.Millisecond)
}
