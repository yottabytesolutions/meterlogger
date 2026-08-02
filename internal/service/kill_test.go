package service

import "testing"

// TestFatalFlag verifies the fatal state used to decide a non-zero exit.
// processKiller sets it via markFatal before sending SIGTERM; the entrypoint
// reads it after shutdown.
func TestFatalFlag(t *testing.T) {
	// Restore global fatal state so other tests are unaffected. No other test
	// sets it: the fatal-path tests replace processKiller with a no-op.
	t.Cleanup(func() {
		fatalState.mu.Lock()
		fatalState.hit = false
		fatalState.mu.Unlock()
	})

	if FatalOccurred() {
		t.Fatal("FatalOccurred() true before any fatal condition")
	}
	markFatal()
	if !FatalOccurred() {
		t.Error("FatalOccurred() false after markFatal()")
	}
}
