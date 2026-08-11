package qdb

import (
	"context"
	"errors"
	"testing"
	"time"

	qdbclient "github.com/questdb/go-questdb-client/v3"
)

// rowBuilder returns a builder that writes one small, recognisable row.
func rowBuilder(power int64) RowBuilder {
	return func(ctx context.Context, sender qdbclient.LineSender) error {
		return sender.Table("heat").Int64Column("power", power).At(ctx, time.Unix(power, 0))
	}
}

// rowSize is the measured size of a rowBuilder row, so tests can express a cap
// in rows without hardcoding the estimator's arithmetic.
func rowSize(t *testing.T) int {
	t.Helper()
	size, err := measure(t.Context(), rowBuilder(1))
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if size <= 0 {
		t.Fatalf("measured size = %d, want a positive estimate", size)
	}
	return size
}

func TestSizingSender_GrowsWithRowContent(t *testing.T) {
	small, err := measure(t.Context(), func(ctx context.Context, s qdbclient.LineSender) error {
		return s.Table("t").Symbol("a", "b").At(ctx, time.Unix(0, 0))
	})
	if err != nil {
		t.Fatalf("measure small: %v", err)
	}
	large, err := measure(t.Context(), func(ctx context.Context, s qdbclient.LineSender) error {
		return s.Table("t").
			Symbol("a", "b").
			StringColumn("long", "a considerably longer value than the small row carries").
			Float64Column("f", 1.5).
			BoolColumn("ok", true).
			At(ctx, time.Unix(0, 0))
	})
	if err != nil {
		t.Fatalf("measure large: %v", err)
	}
	if large <= small {
		t.Errorf("large row measured %d bytes, small row %d; want the estimate to grow", large, small)
	}
}

// A row whose builder fails is a data problem, not a connection problem, so it
// must not be buffered for a replay that would fail the same way.
func TestBufferRow_BuilderErrorIsNotBuffered(t *testing.T) {
	want := errors.New("bad row")
	c := newTestDBClientBuffered(&mockLineSender{}, 1<<20)
	c.now = func() time.Time { return time.Unix(0, 0) }

	err := c.Write(t.Context(), func(context.Context, qdbclient.LineSender) error { return want })

	c.senderMu.Lock()
	buffered := c.buffer.len()
	c.senderMu.Unlock()

	if !errors.Is(err, want) {
		t.Errorf("Write() with a failing builder = %v, want %v", err, want)
	}
	if buffered != 0 {
		t.Errorf("buffered %d rows, want 0", buffered)
	}
}

// The point of the buffer: rows written during an outage reach QuestDB once it
// comes back, in the order they were produced.
func TestDBClient_BuffersAndReplaysAcrossAnOutage(t *testing.T) {
	dead := &mockLineSender{atErr: errors.New("write: broken pipe")}
	fresh := &mockLineSender{}
	now := time.Unix(0, 0)

	c := newTestDBClientBuffered(dead, 1<<20)
	c.now = func() time.Time { return now }
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) { return fresh, nil }

	// The write that discovers the dead socket is itself buffered, not lost.
	if err := c.Write(t.Context(), rowBuilder(1)); err != nil {
		t.Fatalf("Write() that hit the dead connection = %v, want nil (buffered)", err)
	}
	for _, power := range []int64{2, 3} {
		if err := c.Write(t.Context(), rowBuilder(power)); err != nil {
			t.Fatalf("Write() while disconnected = %v, want nil (buffered)", err)
		}
	}

	c.senderMu.Lock()
	buffered := c.buffer.len()
	c.senderMu.Unlock()
	if buffered != 3 {
		t.Fatalf("buffered %d rows, want 3", buffered)
	}
	if !c.Degraded() {
		t.Error("Degraded() while buffering = false, want true")
	}

	now = now.Add(maxReconnectDelay)
	if err := c.Flush(t.Context()); err != nil {
		t.Fatalf("Flush() after the backoff window = %v, want nil", err)
	}

	rows := requireRows(t, fresh, 3)
	for i, want := range []int64{1, 2, 3} {
		if got := rows[i].columns["power"]; got != want {
			t.Errorf("replayed row %d power = %v, want %d", i, got, want)
		}
	}
	c.senderMu.Lock()
	remaining := c.buffer.len()
	c.senderMu.Unlock()
	if remaining != 0 {
		t.Errorf("%d rows left buffered after the replay, want 0", remaining)
	}
	if c.Degraded() {
		t.Error("Degraded() after the replay = true, want false")
	}
	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() after the replay = %v, want nil", err)
	}
}

// The buffer must not grow without limit. Past the cap the oldest rows go, the
// caller starts seeing errors, and the sink stops asking liveness for mercy so
// the pod is restarted.
func TestDBClient_BufferOverflowDropsOldestAndStopsShieldingLiveness(t *testing.T) {
	size := rowSize(t)
	const capRows = 3

	dead := &mockLineSender{atErr: errors.New("write: broken pipe")}
	fresh := &mockLineSender{}
	now := time.Unix(0, 0)

	c := newTestDBClientBuffered(dead, size*capRows)
	c.now = func() time.Time { return now }
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) { return fresh, nil }

	for power := int64(1); power <= capRows; power++ {
		if err := c.Write(t.Context(), rowBuilder(power)); err != nil {
			t.Fatalf("Write() of row %d = %v, want nil (buffered)", power, err)
		}
	}
	if !c.Degraded() {
		t.Error("Degraded() with a buffer that still has room = false, want true")
	}

	// One row past the cap.
	err := c.Write(t.Context(), rowBuilder(capRows+1))
	if err == nil {
		t.Fatal("Write() past the buffer cap = nil, want an error so the service can escalate")
	}
	if c.Degraded() {
		t.Error("Degraded() once rows are being dropped = true, want false so liveness restarts the pod")
	}

	c.senderMu.Lock()
	buffered, dropped, held := c.buffer.len(), c.buffer.dropped, c.buffer.bytes
	c.senderMu.Unlock()
	if buffered != capRows {
		t.Errorf("buffered %d rows, want the cap of %d", buffered, capRows)
	}
	if dropped != 1 {
		t.Errorf("dropped %d rows, want 1", dropped)
	}
	if held > size*capRows {
		t.Errorf("holding %d bytes, want at most %d", held, size*capRows)
	}

	// What survives is the newest data, oldest evicted.
	now = now.Add(maxReconnectDelay)
	if flushErr := c.Flush(t.Context()); flushErr != nil {
		t.Fatalf("Flush() after the backoff window = %v, want nil", flushErr)
	}
	rows := requireRows(t, fresh, capRows)
	for i, want := range []int64{2, 3, 4} {
		if got := rows[i].columns["power"]; got != want {
			t.Errorf("replayed row %d power = %v, want %d", i, got, want)
		}
	}
}

// With buffering off the sink keeps the pre-1.6.0 behaviour: rows written
// during an outage are dropped and the caller is told immediately.
func TestDBClient_BufferingDisabledDropsRows(t *testing.T) {
	dead := &mockLineSender{atErr: errors.New("write: broken pipe")}
	c := newTestDBClientWith(dead)
	c.now = func() time.Time { return time.Unix(0, 0) }

	if err := c.Write(t.Context(), rowBuilder(1)); err == nil {
		t.Fatal("Write() with buffering disabled = nil, want an error")
	}
	if err := c.Write(t.Context(), rowBuilder(2)); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Write() with buffering disabled = %v, want ErrDisconnected", err)
	}
	if c.Degraded() {
		t.Error("Degraded() with buffering disabled = true, want false")
	}
	c.senderMu.Lock()
	buffered := c.buffer.len()
	c.senderMu.Unlock()
	if buffered != 0 {
		t.Errorf("buffered %d rows with buffering disabled, want 0", buffered)
	}
}

// A replay that fails partway keeps the rows it never handed over, so a second
// outage during recovery does not throw away everything still in hand.
func TestDBClient_ReplayFailureKeepsUnsentRows(t *testing.T) {
	dead := &mockLineSender{atErr: errors.New("write: broken pipe")}
	now := time.Unix(0, 0)

	c := newTestDBClientBuffered(dead, 1<<20)
	c.now = func() time.Time { return now }

	const buffered = 4
	for power := int64(1); power <= buffered; power++ {
		if err := c.Write(t.Context(), rowBuilder(power)); err != nil {
			t.Fatalf("Write() of row %d = %v, want nil (buffered)", power, err)
		}
	}

	// The redial succeeds but the connection dies again on the replay flush.
	stillBroken := &mockLineSender{flushErr: errors.New("write: broken pipe")}
	c.dial = func(context.Context, Config) (qdbclient.LineSender, error) { return stillBroken, nil }
	now = now.Add(maxReconnectDelay)

	if err := c.Flush(t.Context()); err == nil {
		t.Fatal("Flush() with a replay that fails = nil, want an error")
	}

	c.senderMu.Lock()
	remaining, dropped := c.buffer.len(), c.buffer.dropped
	sender := c.sender
	c.senderMu.Unlock()

	// One chunk was handed over and lost with the failed flush. There are
	// fewer rows than a chunk here, so all of them went in one batch.
	if remaining != 0 {
		t.Errorf("%d rows kept after the whole batch was handed over, want 0", remaining)
	}
	if dropped != buffered {
		t.Errorf("dropped %d rows, want %d", dropped, buffered)
	}
	if sender != nil {
		t.Error("sender kept after a failed replay, want it torn down for another redial")
	}
}

func TestRowBuffer_TakeAndPushFrontPreserveOrderAndAccounting(t *testing.T) {
	b := newRowBuffer(1000)
	for i := range 5 {
		if !b.add(bufferedRow{build: rowBuilder(int64(i)), size: 10}) {
			t.Fatalf("add(%d) evicted, want it to fit", i)
		}
	}
	if b.bytes != 50 {
		t.Fatalf("bytes = %d, want 50", b.bytes)
	}

	batch := b.take(2)
	if len(batch) != 2 || b.len() != 3 || b.bytes != 30 {
		t.Fatalf("after take(2): batch=%d left=%d bytes=%d, want 2/3/30", len(batch), b.len(), b.bytes)
	}

	b.pushFront(batch)
	if b.len() != 5 || b.bytes != 50 {
		t.Fatalf("after pushFront: left=%d bytes=%d, want 5/50", b.len(), b.bytes)
	}

	// take never asks for more than it holds.
	if got := len(b.take(99)); got != 5 {
		t.Errorf("take(99) returned %d rows, want 5", got)
	}
	if b.bytes != 0 {
		t.Errorf("bytes = %d after draining, want 0", b.bytes)
	}
}

// A single row larger than the whole cap can never be stored, and must be
// reported rather than silently discarded.
func TestRowBuffer_RowLargerThanCapIsRejected(t *testing.T) {
	b := newRowBuffer(10)
	if b.add(bufferedRow{build: rowBuilder(1), size: 11}) {
		t.Error("add() of a row larger than the cap reported success, want failure")
	}
	if b.len() != 0 || b.bytes != 0 {
		t.Errorf("buffer holds %d rows / %d bytes, want empty", b.len(), b.bytes)
	}
	if b.dropped != 1 {
		t.Errorf("dropped = %d, want 1", b.dropped)
	}
}
