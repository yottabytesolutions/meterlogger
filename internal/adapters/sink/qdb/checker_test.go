package qdb

import (
	"errors"
	"testing"
)

func TestDBClient_Name(t *testing.T) {
	c := newTestDBClient()
	if got := c.Name(); got != "questdb" {
		t.Errorf("Name() = %q, want questdb", got)
	}
}

func TestDBClient_Check_CleanStateIsHealthy(t *testing.T) {
	c := newTestDBClient()
	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() before any flush = %v, want nil", err)
	}
}

func TestDBClient_Check_ReportsLastFlushError(t *testing.T) {
	c := newTestDBClient()
	want := errors.New("boom")

	c.mu.Lock()
	c.lastFlushErr = want
	c.mu.Unlock()

	if err := c.Check(t.Context()); !errors.Is(err, want) {
		t.Errorf("Check() after failed flush = %v, want %v", err, want)
	}

	// A subsequent successful flush clears the error.
	c.mu.Lock()
	c.lastFlushErr = nil
	c.mu.Unlock()

	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() after successful flush = %v, want nil", err)
	}
}

// TestDBClient_Flush_RecordsOutcome verifies that Flush updates the last
// flush state observed by Check, using a mock LineSender.
func TestDBClient_Flush_RecordsOutcome(t *testing.T) {
	want := errors.New("flush failed")
	c := &DBClient{
		sender: &mockLineSender{flushErr: want},
		logger: testLogger(),
	}

	if err := c.Flush(t.Context()); !errors.Is(err, want) {
		t.Fatalf("Flush() = %v, want %v", err, want)
	}
	if err := c.Check(t.Context()); !errors.Is(err, want) {
		t.Errorf("Check() after failing Flush() = %v, want %v", err, want)
	}

	// Replace the sender with a healthy one and flush again.
	c.sender = &mockLineSender{}
	if err := c.Flush(t.Context()); err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}
	if err := c.Check(t.Context()); err != nil {
		t.Errorf("Check() after successful Flush() = %v, want nil", err)
	}
}
