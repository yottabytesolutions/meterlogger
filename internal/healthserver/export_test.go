package healthserver

import "time"

// SetNow replaces the server's clock for tests in the healthserver_test
// package. It must not be called concurrently with running probes.
func SetNow(s *Server, now func() time.Time) {
	s.now = now
}
