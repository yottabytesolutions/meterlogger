package main

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

func testHelperLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// mockService is a minimal service for testing.
type mockService struct {
	startCalled bool
}

func (m *mockService) Start(ctx context.Context) {
	m.startCalled = true
	<-ctx.Done()
}

func TestStartService(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	svc := &mockService{}
	l := testHelperLogger()

	done := make(chan struct{})
	go func() {
		startService(ctx, l, "test-svc", svc)
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("startService did not return after context cancel")
	}

	if !svc.startCalled {
		t.Error("service.Start was not called")
	}
}

func TestInterruptAwareContext(t *testing.T) {
	ctx := interruptAwareContext()
	if ctx == nil {
		t.Error("interruptAwareContext() returned nil context")
	}

	// Simulate an interrupt by sending a signal
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()

	select {
	case <-ctx.Done():
		// Context was cancelled as expected
	case <-time.After(2 * time.Second):
		t.Error("context was not cancelled after interrupt signal")
	}
}

func TestDoWork_ContextCancel(t *testing.T) {
	l := testHelperLogger()
	ctx, cancel := context.WithCancel(context.Background())

	serviceCallCount := 0
	serviceFn := func(c context.Context) {
		serviceCallCount++
		// Block until context is cancelled so doWork loops back
		<-c.Done()
	}

	done := make(chan struct{})
	go func() {
		doWork(ctx, l, "test-service", serviceFn)
		close(done)
	}()

	// Give the goroutine time to start the service
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("doWork did not exit after context cancel")
	}

	if serviceCallCount == 0 {
		t.Error("service function was never called")
	}
}

func TestDoWork_ServiceCompletesAndRestarts(t *testing.T) {
	l := testHelperLogger()
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	serviceFn := func(_ context.Context) {
		callCount++
		if callCount >= 2 {
			// After 2 calls, cancel context
			cancel()
		}
		// Return immediately to trigger restart
	}

	done := make(chan struct{})
	go func() {
		doWork(ctx, l, "restart-service", serviceFn)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Error("doWork did not exit")
	}

	if callCount < 2 {
		t.Errorf("service was called %d times, want at least 2", callCount)
	}
}
