package healthserver

import "time"

// SetNow replaces the server's clock for tests in the healthserver_test
// package. It must not be called concurrently with running probes.
func SetNow(s *Server, now func() time.Time) {
	s.now = now
}

// SetCheckTimeout replaces the per-checker timeout for tests. It must not be
// called concurrently with running probes.
func SetCheckTimeout(s *Server, d time.Duration) {
	s.checkTimeout = d
}
