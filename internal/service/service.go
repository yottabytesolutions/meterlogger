// Package service contains the polling loops that connect a meter source to
// one or more sinks. Each source has its own service type, but they all
// follow the same shape: read on an interval, publish on a channel, drain
// the channel into the repository, flush periodically, and exit cleanly when
// ctx is cancelled.
//
// Services depend only on interfaces from internal/domain. They never import
// an adapter package directly. A non-recoverable error sends SIGTERM to the
// process via the processKiller seam so the container restarts and the fault
// is visible to the orchestrator.
package service

import "context"

// maxConsecutiveErrors is the number of consecutive read/store failures a
// polling service tolerates before escalating via processKiller. Shared by
// the heat, grid, and solar services. DucoLoggingService uses its own,
// more tolerant threshold for read errors specifically.
const maxConsecutiveErrors = 5

// Service is the common shape for every per-source polling loop. Start runs
// until ctx is cancelled. Implementations are constructed once per process
// and own a single reader and a single repository.
type Service interface {
	Start(ctx context.Context)
}
