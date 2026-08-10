package qdb

import (
	"context"
	"errors"
	"testing"
	"time"

	qdbclient "github.com/questdb/go-questdb-client/v3"
)

func TestDBClient_Name(t *testing.T) {
	c, _ := newTestDBClient()
	if got := c.Name(); got != "questdb" {
		t.Errorf("Name() = %q, want questdb", got)
	}
}

func TestDBClient_Check_CleanStateIsHealthy(t *testing.T) {
	c, _ := newTestDBClient()
	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() before any flush = %v, want nil", err)
	}
}

// A single failed flush is absorbed by the redial and must not flip readiness.
// Only a run of failures reports unhealthy.
func TestDBClient_Check_UnhealthyOnlyAfterThreshold(t *testing.T) {
	want := errors.New("broken pipe")
	c := newTestDBClientWith(&mockLineSender{flushErr: want})
	c.now = func() time.Time { return time.Unix(0, 0) }

	for i := 1; i < unhealthyAfterFailures; i++ {
		c.recordFailure(want)
		if err := c.Check(t.Context()); err != nil {
			t.Fatalf("Check() after %d failures = %v, want nil", i, err)
		}
	}

	c.recordFailure(want)
	err := c.Check(t.Context())
	if !errors.Is(err, want) {
		t.Errorf("Check() after %d failures = %v, want %v", unhealthyAfterFailures, err, want)
	}

	c.recordSuccess()
	if recoveredErr := c.Check(t.Context()); recoveredErr != nil {
		t.Errorf("Check() after recovery = %v, want nil", recoveredErr)
	}
}

// Regression: an empty flush writes zero bytes and therefore always succeeds,
// even on a dead socket. It must not clear a failure streak that has already
// crossed the threshold, or the pod stays Ready with a dead sink.
func TestDBClient_Check_EmptyFlushDoesNotHideDeadSink(t *testing.T) {
	writeErr := errors.New("broken pipe")
	c := newTestDBClientWith(&mockLineSender{})
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) { return nil, errNoDial }
	c.now = func() time.Time { return time.Unix(0, 0) }

	for range unhealthyAfterFailures {
		c.recordFailure(writeErr)
	}
	c.senderMu.Lock()
	c.connectionFailed(t.Context(), writeErr)
	c.senderMu.Unlock()

	// The flush loop keeps ticking with an empty buffer while disconnected.
	for range 10 {
		if err := c.Flush(t.Context()); err == nil {
			t.Fatal("Flush() while disconnected = nil, want an error")
		}
	}
	if err := c.Check(t.Context()); err == nil {
		t.Error("Check() with a dead connection = nil, want an error")
	}
}

func TestDBClient_Flush_RecordsOutcome(t *testing.T) {
	want := errors.New("flush failed")
	c := newTestDBClientWith(&mockLineSender{flushErr: want})
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }

	if err := c.Flush(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Flush() = %v, want %v", err, want)
	}

	// The failed flush tore the connection down and scheduled a redial.
	c.senderMu.Lock()
	gotSender := c.sender
	c.senderMu.Unlock()
	if gotSender != nil {
		t.Error("sender was kept after a failed flush, want it closed and dropped")
	}

	healthy := &mockLineSender{}
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) { return healthy, nil }
	now = now.Add(initialReconnectDelay)

	if err := c.Flush(t.Context()); err != nil {
		t.Fatalf("Flush() after redial = %v, want nil", err)
	}
	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() after successful Flush() = %v, want nil", err)
	}
}

// The core regression: after the server closes the connection, the client must
// dial a new sender instead of writing into the dead one forever.
func TestDBClient_Write_RedialsAfterConnectionLoss(t *testing.T) {
	dead := &mockLineSender{atErr: errors.New("write: broken pipe")}
	fresh := &mockLineSender{}
	dials := 0
	now := time.Unix(0, 0)

	c := newTestDBClientWith(dead)
	c.now = func() time.Time { return now }
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) {
		dials++
		return fresh, nil
	}

	writeRow := func() error {
		return c.Write(t.Context(), func(sender qdbclient.LineSender) error {
			return sender.Table("t").Symbol("s", "v").At(t.Context(), now)
		})
	}

	if err := writeRow(); err == nil {
		t.Fatal("Write() on a dead connection = nil, want an error")
	}
	if dials != 0 {
		t.Fatalf("dialled %d times immediately after the failure, want 0 (backoff)", dials)
	}

	// Still inside the backoff window: the row is dropped, no dial attempt.
	if err := writeRow(); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Write() during backoff = %v, want ErrDisconnected", err)
	}
	if dials != 0 {
		t.Fatalf("dialled %d times during the backoff window, want 0", dials)
	}
	c.senderMu.Lock()
	dropped := c.droppedRows
	c.senderMu.Unlock()
	if dropped != 1 {
		t.Errorf("droppedRows = %d, want 1", dropped)
	}

	now = now.Add(initialReconnectDelay)
	if err := writeRow(); err != nil {
		t.Fatalf("Write() after the backoff window = %v, want nil", err)
	}
	if dials != 1 {
		t.Fatalf("dialled %d times, want 1", dials)
	}
	requireRows(t, fresh, 1)

	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() after a successful redial = %v, want nil", err)
	}
}

// Backoff must grow and stay bounded so a long outage does not hammer QuestDB.
func TestDBClient_ReconnectBackoffGrowsAndIsBounded(t *testing.T) {
	now := time.Unix(0, 0)
	dials := 0

	c := newTestDBClientWith(&mockLineSender{})
	c.now = func() time.Time { return now }
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) {
		dials++
		return nil, errors.New("connection refused")
	}

	c.senderMu.Lock()
	c.connectionFailed(t.Context(), errors.New("broken pipe"))
	c.senderMu.Unlock()

	wantDelays := []time.Duration{
		initialReconnectDelay,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		maxReconnectDelay,
		maxReconnectDelay,
	}
	for i, delay := range wantDelays {
		now = now.Add(delay)
		if err := c.Flush(t.Context()); err == nil {
			t.Fatalf("attempt %d: Flush() = nil, want an error", i)
		}
		if dials != i+1 {
			t.Fatalf("attempt %d: dialled %d times, want %d", i, dials, i+1)
		}
		// Waking up one tick early must not dial again.
		if err := c.Flush(t.Context()); !errors.Is(err, ErrDisconnected) {
			t.Fatalf("attempt %d: Flush() inside the backoff window = %v, want ErrDisconnected", i, err)
		}
		if dials != i+1 {
			t.Fatalf("attempt %d: dialled during the backoff window", i)
		}
	}

	c.senderMu.Lock()
	got := c.reconnectDelay
	c.senderMu.Unlock()
	if got != maxReconnectDelay {
		t.Errorf("reconnectDelay = %v, want it capped at %v", got, maxReconnectDelay)
	}
}
