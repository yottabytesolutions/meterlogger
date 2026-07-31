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

import (
	"context"
	"time"
)

// maxConsecutiveErrors is the number of consecutive failures a polling
// service tolerates before escalating via processKiller. Shared by the heat,
// grid, and solar services and by the duco flush path. DucoLoggingService
// uses its own, more tolerant threshold for read-and-store cycles.
const maxConsecutiveErrors = 5

// storeFlushTimeout bounds every sink store and flush call. Without it a hung
// connection stalls the polling loop and the error counter never advances.
const storeFlushTimeout = 10 * time.Second

// withStoreTimeout runs op with a child context bounded by storeFlushTimeout
// and releases the timer immediately after op returns.
func withStoreTimeout(ctx context.Context, op func(context.Context) error) error {
	opCtx, cancel := context.WithTimeout(ctx, storeFlushTimeout)
	defer cancel()
	return op(opCtx)
}

// Service is the common shape for every per-source polling loop. Start runs
// until ctx is cancelled. Implementations are constructed once per process
// and own a single reader and a single repository.
type Service interface {
	Start(ctx context.Context)
}
