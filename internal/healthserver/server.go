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
)

// Checker reports health for one component.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// Server is a small HTTP server exposing /healthz, /readyz, and /metrics.
type Server struct {
	addr     string
	checkers []Checker
	logger   *slog.Logger
	mu       sync.RWMutex
	mux      *http.ServeMux
	wg       sync.WaitGroup
}

// New creates a Server that will listen on addr (e.g. ":8080").
// reg is the Prometheus registry to expose on /metrics.
func New(addr string, logger *slog.Logger, reg *prometheus.Registry) *Server {
	s := &Server{
		addr:   addr,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.mux.HandleFunc("/healthz", s.handleLiveness)
	s.mux.HandleFunc("/readyz", s.handleReadiness)
	s.mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	return s
}

// Register adds a health checker to the readiness probe.
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
		s.logger.Info("health server started", slog.String("addr", addr))
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.logger.Error("health server error", slog.Any("error", serveErr))
		}
	})

	s.wg.Go(func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if shutErr := srv.Shutdown(shutCtx); shutErr != nil {
			s.logger.Warn("health server shutdown error", slog.Any("error", shutErr))
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
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	checkers := make([]Checker, len(s.checkers))
	copy(checkers, s.checkers)
	s.mu.RUnlock()

	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()

	results := make([]checkResult, 0, len(checkers))
	allHealthy := true

	for _, c := range checkers {
		res := checkResult{Name: c.Name(), Healthy: true}
		if checkErr := c.Check(ctx); checkErr != nil {
			res.Healthy = false
			res.Error = checkErr.Error()
			allHealthy = false
		}
		results = append(results, res)
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
