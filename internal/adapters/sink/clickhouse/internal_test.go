package clickhouse

import (
	"net/url"
	"testing"
)

func TestBatchBuffer_AddEvictsOldestBeyondCap(t *testing.T) {
	var buf batchBuffer[int]
	const extra = 3

	dropped := 0
	for i := range maxBufferedRows + extra {
		dropped += buf.add(i)
	}

	if dropped != extra {
		t.Errorf("dropped = %d, want %d", dropped, extra)
	}
	rows := buf.take()
	if len(rows) != maxBufferedRows {
		t.Fatalf("len(rows) = %d, want %d", len(rows), maxBufferedRows)
	}
	if rows[0] != extra {
		t.Errorf("rows[0] = %d, want %d (oldest rows dropped)", rows[0], extra)
	}
	if last := rows[len(rows)-1]; last != maxBufferedRows+extra-1 {
		t.Errorf("last row = %d, want %d", last, maxBufferedRows+extra-1)
	}
}

func TestBatchBuffer_RequeuePreservesOrder(t *testing.T) {
	var buf batchBuffer[int]
	buf.add(1)
	buf.add(2)
	batch := buf.take()

	// New rows arrive while the failed batch is out.
	buf.add(3)

	if dropped := buf.requeue(batch); dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	got := buf.take()
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestBatchBuffer_RequeueEvictsOldestBeyondCap(t *testing.T) {
	var buf batchBuffer[int]
	batch := make([]int, 10)
	for i := range batch {
		batch[i] = i
	}
	for i := range maxBufferedRows - 5 {
		buf.add(10 + i)
	}

	if dropped := buf.requeue(batch); dropped != 5 {
		t.Errorf("dropped = %d, want 5", dropped)
	}
	rows := buf.take()
	if len(rows) != maxBufferedRows {
		t.Fatalf("len(rows) = %d, want %d", len(rows), maxBufferedRows)
	}
	// The five oldest batch rows (0..4) are gone; 5 survives at the front.
	if rows[0] != 5 {
		t.Errorf("rows[0] = %d, want 5", rows[0])
	}
}

func TestBatchBuffer_RequeueEmptyIsNoop(t *testing.T) {
	var buf batchBuffer[int]
	buf.add(1)
	if dropped := buf.requeue(nil); dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
	if rows := buf.take(); len(rows) != 1 || rows[0] != 1 {
		t.Errorf("rows = %v, want [1]", rows)
	}
}

func TestBuildDSN_EscapesSpecialCharacters(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "plain",
			cfg:  Config{Host: "localhost", Port: 9000, User: "default", Password: "secret", Database: "metrics"},
		},
		{
			name: "special characters in password",
			//nolint:gosec // test fixture, not a real credential
			cfg: Config{Host: "ch.example", Port: 9440, User: "u@ser", Password: "p@ss/w?rd#1&x", Database: "metrics"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(buildDSN(tt.cfg))
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			assertDSN(t, u, tt.cfg)
		})
	}
}

func assertDSN(t *testing.T, u *url.URL, cfg Config) {
	t.Helper()
	if u.Scheme != driverName {
		t.Errorf("scheme = %q, want %q", u.Scheme, driverName)
	}
	if got := u.User.Username(); got != cfg.User {
		t.Errorf("user = %q, want %q", got, cfg.User)
	}
	pass, ok := u.User.Password()
	if !ok || pass != cfg.Password {
		t.Errorf("password = %q (set=%v), want %q", pass, ok, cfg.Password)
	}
	if got := u.Hostname(); got != cfg.Host {
		t.Errorf("host = %q, want %q", got, cfg.Host)
	}
	if got := u.Path; got != "/"+cfg.Database {
		t.Errorf("path = %q, want %q", got, "/"+cfg.Database)
	}
	if got := u.Query().Get("dial_timeout"); got != "5s" {
		t.Errorf("dial_timeout = %q, want 5s", got)
	}
}
