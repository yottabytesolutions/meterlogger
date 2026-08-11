package qdb

import (
	"context"
	"math/big"
	"time"

	qdbclient "github.com/questdb/go-questdb-client/v3"
)

// RowBuilder writes one ILP row into sender. It receives its own context
// because a buffered row is replayed long after the store call that produced
// it has returned and that call's timeout has expired.
type RowBuilder func(ctx context.Context, sender qdbclient.LineSender) error

// bufferedRow is one row waiting for the connection to come back, together
// with the estimated size of its ILP encoding.
type bufferedRow struct {
	build RowBuilder
	size  int
}

// rowBuffer holds rows written while the ILP connection is down, oldest first,
// under a byte cap. Once the cap is reached the oldest rows are evicted: the
// newest data is the most useful, and an unbounded buffer would trade a data
// gap for an OOM kill.
type rowBuffer struct {
	maxBytes int

	rows    []bufferedRow
	bytes   int
	dropped int64
}

func newRowBuffer(maxBytes int) *rowBuffer {
	return &rowBuffer{maxBytes: maxBytes}
}

// enabled reports whether rows are buffered at all. A zero cap restores the
// drop-on-disconnect behaviour.
func (b *rowBuffer) enabled() bool { return b.maxBytes > 0 }

// add appends a row, evicting the oldest rows if it does not fit. It reports
// whether the row was stored without evicting anything.
func (b *rowBuffer) add(row bufferedRow) bool {
	if !b.enabled() || row.size > b.maxBytes {
		b.dropped++
		return false
	}

	evicted := false
	for b.bytes+row.size > b.maxBytes && len(b.rows) > 0 {
		b.bytes -= b.rows[0].size
		b.rows = b.rows[1:]
		b.dropped++
		evicted = true
	}

	b.rows = append(b.rows, row)
	b.bytes += row.size
	return !evicted
}

// take removes and returns up to n rows from the front.
func (b *rowBuffer) take(n int) []bufferedRow {
	if n > len(b.rows) {
		n = len(b.rows)
	}
	batch := b.rows[:n]
	for _, row := range batch {
		b.bytes -= row.size
	}
	// Copy so the retained tail does not keep the batch alive through the
	// shared backing array.
	b.rows = append([]bufferedRow(nil), b.rows[n:]...)
	return batch
}

// pushFront puts an unsent batch back at the head, keeping insertion order.
func (b *rowBuffer) pushFront(batch []bufferedRow) {
	for _, row := range batch {
		b.bytes += row.size
	}
	b.rows = append(batch, b.rows...)
}

func (b *rowBuffer) len() int { return len(b.rows) }

// reset clears the buffer and the drop count after a successful drain.
func (b *rowBuffer) reset() {
	b.rows = nil
	b.bytes = 0
	b.dropped = 0
}

// Estimated ILP encoding sizes. Symbols and strings are measured exactly;
// numeric values use the widest formatting they can produce, so the estimate
// errs towards over-counting and the memory cap is never exceeded in practice.
const (
	sizeSeparators   = 3  // table/symbol-set/column-set/timestamp separators plus newline
	sizeInt64Value   = 21 // -9223372036854775808 plus the 'i' suffix
	sizeFloat64Value = 24 // widest strconv 'G' rendering
	sizeBoolValue    = 1
	sizeTimestampVal = 21 // microseconds since epoch plus the 't' suffix
	sizeLong256Value = 66 // "0x" plus 64 hex digits
	sizeStringQuotes = 2
	sizeFieldName    = 2 // '=' and the ',' that separates fields
)

// sizingSender implements qdbclient.LineSender and measures the ILP encoding
// of a row without sending anything. The client does not expose the encoded
// length of a message, so the buffer estimates it here.
type sizingSender struct {
	size int
}

func (s *sizingSender) Table(name string) qdbclient.LineSender {
	s.size += len(name) + sizeSeparators
	return s
}

func (s *sizingSender) Symbol(name, val string) qdbclient.LineSender {
	s.size += len(name) + len(val) + sizeFieldName
	return s
}

func (s *sizingSender) Int64Column(name string, _ int64) qdbclient.LineSender {
	s.size += len(name) + sizeInt64Value + sizeFieldName
	return s
}

func (s *sizingSender) Long256Column(name string, _ *big.Int) qdbclient.LineSender {
	s.size += len(name) + sizeLong256Value + sizeFieldName
	return s
}

func (s *sizingSender) TimestampColumn(name string, _ time.Time) qdbclient.LineSender {
	s.size += len(name) + sizeTimestampVal + sizeFieldName
	return s
}

func (s *sizingSender) Float64Column(name string, _ float64) qdbclient.LineSender {
	s.size += len(name) + sizeFloat64Value + sizeFieldName
	return s
}

func (s *sizingSender) StringColumn(name, val string) qdbclient.LineSender {
	s.size += len(name) + len(val) + sizeStringQuotes + sizeFieldName
	return s
}

func (s *sizingSender) BoolColumn(name string, _ bool) qdbclient.LineSender {
	s.size += len(name) + sizeBoolValue + sizeFieldName
	return s
}

func (s *sizingSender) At(_ context.Context, _ time.Time) error {
	s.size += sizeTimestampVal
	return nil
}

func (s *sizingSender) AtNow(_ context.Context) error { return nil }
func (s *sizingSender) Flush(_ context.Context) error { return nil }
func (s *sizingSender) Close(_ context.Context) error { return nil }

// measure runs build against a sizing sender and reports the estimated ILP
// size of the row it writes.
func measure(ctx context.Context, build RowBuilder) (int, error) {
	s := &sizingSender{}
	if err := build(ctx, s); err != nil {
		return 0, err
	}
	return s.size, nil
}
