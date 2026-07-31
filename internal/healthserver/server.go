// Package healthserver provides an HTTP server for health and metrics endpoints.
package healthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	readTimeout     = 5 * time.Second
	writeTimeout    = 10 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 5 * time.Second
	checkTimeout    = 1 * time.Second

	// DefaultLivenessFailureThreshold is the default duration a checker must
	// be continuously unhealthy before /healthz starts returning 503. Six
	// readiness probes at the standard 15s period.
	DefaultLivenessFailureThreshold = 90 * time.Second
)

// Checker reports health for one component.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Server is a small HTTP server exposing /healthz, /readyz, and /metrics.
//
// /readyz reflects the current state of every registered checker. /healthz
// reflects sustained failure: it stays green through transient blips so the
// kubelet does not restart pods on every short outage, but flips to 503 once
// any checker has been continuously unhealthy for livenessThreshold. That
// turns a stuck Running-but-NotReady pod into a CrashLoopBackOff that the
// orchestrator can act on.
type Server struct {
	addr              string
	checkers          []Checker
	logger            *slog.Logger
	mu                sync.RWMutex
	mux               *http.ServeMux
	wg                sync.WaitGroup
	livenessThreshold time.Duration
	failingSince      map[string]time.Time
	now               func() time.Time
	checkTimeout      time.Duration
}

// New creates a Server that will listen on addr (e.g. ":8080").
// reg is the Prometheus registry to expose on /metrics.
// livenessThreshold is the duration a checker must be continuously unhealthy
// before /healthz returns 503; pass DefaultLivenessFailureThreshold for the
// recommended default.
func New(addr string, logger *slog.Logger, reg *prometheus.Registry, livenessThreshold time.Duration) *Server {
	s := &Server{
		addr:              addr,
		logger:            logger,
		mux:               http.NewServeMux(),
		livenessThreshold: livenessThreshold,
		failingSince:      make(map[string]time.Time),
		now:               time.Now,
		checkTimeout:      checkTimeout,
	}
	s.mux.HandleFunc("/healthz", s.handleLiveness)
	s.mux.HandleFunc("/readyz", s.handleReadiness)
	s.mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	return s
}

// Register adds a health checker to both probes.
func (s *Server) Register(c Checker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkers = append(s.checkers, c)
}

// ServeHTTP allows the server to be used with httptest.NewRecorder in tests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Start begins listening and serving. Returns the actual bound address.
// The server shuts down gracefully when ctx is cancelled. Callers MUST call
// Wait after ctx is cancelled to block until in-flight requests are drained
// and the listener is closed; otherwise downstream resources (DB pools)
// closed by the caller may race against probes still being served.
func (s *Server) Start(ctx context.Context) (string, error) {
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return "", fmt.Errorf("health server failed to listen: %w", err)
	}
	addr := ln.Addr().String()

	srv := &http.Server{
		Handler:      s.mux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	s.wg.Go(func() {
		s.logger.InfoContext(ctx, "health server started", slog.String("addr", addr))
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.logger.ErrorContext(ctx, "health server error", slog.Any("error", serveErr))
		}
	})

	s.wg.Go(func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if shutErr := srv.Shutdown(shutCtx); shutErr != nil {
			s.logger.WarnContext(shutCtx, "health server shutdown error", slog.Any("error", shutErr))
		}
	})

	return addr, nil
}

// Wait blocks until the server has finished its graceful shutdown. Safe to
// call before or after ctx is cancelled. If Start was never called, returns
// immediately.
func (s *Server) Wait() {
	s.wg.Wait()
}

type checkResult struct {
	Name       string `json:"name"`
	Healthy    bool   `json:"healthy"`
	Error      string `json:"error,omitempty"`
	FailingFor string `json:"failingFor,omitempty"`

	// failingDur is the unexported duration that handlers use to decide
	// whether the failure has crossed the liveness threshold. encoding/json
	// ignores unexported fields.
	failingDur time.Duration
}

// runChecks runs every registered checker once, updates per-checker failure
// state, and returns the results. Both /healthz and /readyz call it so they
// observe a consistent snapshot.
func (s *Server) runChecks(ctx context.Context) []checkResult {
	s.mu.RLock()
	checkers := slices.Clone(s.checkers)
	s.mu.RUnlock()

	// Each checker gets its own timeout so one slow component cannot eat the
	// budget of the checkers that run after it and flap their readiness.
	results := make([]checkResult, 0, len(checkers))
	for _, c := range checkers {
		res := checkResult{Name: c.Name(), Healthy: true}
		if checkErr := s.runCheck(ctx, c); checkErr != nil {
			res.Healthy = false
			res.Error = checkErr.Error()
		}
		results = append(results, res)
	}

	now := s.now()
	s.mu.Lock()
	for i := range results {
		res := &results[i]
		if res.Healthy {
			delete(s.failingSince, res.Name)
			continue
		}
		since, ok := s.failingSince[res.Name]
		if !ok {
			since = now
			s.failingSince[res.Name] = since
		}
		res.failingDur = now.Sub(since)
		res.FailingFor = res.failingDur.String()
	}
	s.mu.Unlock()

	return results
}

// runCheck runs a single checker under its own timeout.
func (s *Server) runCheck(ctx context.Context, c Checker) error {
	ctx, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()
	return c.Check(ctx)
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	results := s.runChecks(r.Context())

	stuck := make([]string, 0)
	for _, res := range results {
		if !res.Healthy && res.failingDur >= s.livenessThreshold {
			stuck = append(stuck, res.Name)
		}
	}

	statusStr := "ok"
	statusCode := http.StatusOK
	if len(stuck) > 0 {
		statusStr = "stuck"
		statusCode = http.StatusServiceUnavailable
		s.logger.ErrorContext(
			r.Context(),
			"liveness failing: checker(s) unhealthy beyond threshold",
			slog.Any("stuck", stuck),
			slog.Duration("threshold", s.livenessThreshold),
		)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(
		map[string]any{
			"status": statusStr,
			"stuck":  stuck,
			"checks": results,
		},
	)
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	results := s.runChecks(r.Context())

	allHealthy := true
	for _, res := range results {
		if !res.Healthy {
			allHealthy = false
			break
		}
	}

	statusStr := "ok"
	statusCode := http.StatusOK
	if !allHealthy {
		statusStr = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(
		map[string]any{
			"status": statusStr,
			"checks": results,
		},
	)
}
