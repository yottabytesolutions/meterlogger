package service

import "syscall"

// processKiller terminates the current process with SIGTERM.
// It is a variable, so tests can replace it with a no-op.
//
//nolint:gochecknoglobals // testability pattern: allows tests to replace with no-op
var processKiller = func() {
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
}
