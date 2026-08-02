package service

import (
	"sync"
	"syscall"
)

// fatalState records that a service terminated on an unrecoverable condition
// (the consecutive-error threshold), so the process can exit non-zero. Without
// it, processKiller's SIGTERM is handled by the normal graceful-shutdown path
// and the process exits 0, making a genuine failure look like a clean stop to
// Kubernetes and to alerting.
//
//nolint:gochecknoglobals // process-wide fatal state, read once by the entrypoint
var fatalState struct {
	mu  sync.Mutex
	hit bool
}

// FatalOccurred reports whether any service hit its consecutive-error threshold
// and terminated the process. The entrypoint checks it after shutdown to decide
// the exit code.
func FatalOccurred() bool {
	fatalState.mu.Lock()
	defer fatalState.mu.Unlock()
	return fatalState.hit
}

func markFatal() {
	fatalState.mu.Lock()
	fatalState.hit = true
	fatalState.mu.Unlock()
}

// processKiller records the fatal condition and terminates the current process
// with SIGTERM, so the normal signal handler still drains and flushes before
// exit. It is a variable, so tests can replace it with a no-op.
//
//nolint:gochecknoglobals // testability pattern: allows tests to replace with no-op
var processKiller = func() {
	markFatal()
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
}
