package qdb

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	qdbclient "github.com/questdb/go-questdb-client/v3"
)

// ilpServer is a minimal stand-in for QuestDB's ILP/TCP port. It accepts
// connections one at a time and records the bytes of each.
type ilpServer struct {
	ln net.Listener

	mu       sync.Mutex
	received [][]byte
	conns    []net.Conn
	accepted int
}

func newILPServer(t *testing.T) *ilpServer {
	t.Helper()
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &ilpServer{ln: ln}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *ilpServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.accepted++
		idx := len(s.received)
		s.received = append(s.received, nil)
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		s.read(conn, idx)
	}
}

// read drains one connection until the peer or the test closes it.
func (s *ilpServer) read(conn net.Conn, idx int) {
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.received[idx] = append(s.received[idx], buf[:n]...)
			s.mu.Unlock()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = conn.Close()
			}
			return
		}
	}
}

func (s *ilpServer) hostPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(s.ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

func (s *ilpServer) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted
}

func (s *ilpServer) bytesOn(idx int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx >= len(s.received) {
		return 0
	}
	return len(s.received[idx])
}

// TestDBClient_RedialsAfterServerClosesConnection is the end-to-end regression
// for the reported bug: QuestDB restarts, the ILP socket dies, and the client
// used to write into it forever on the same source port. It exercises the real
// ILP sender against a real TCP listener, not a mock.
func TestDBClient_RedialsAfterServerClosesConnection(t *testing.T) {
	srv := newILPServer(t)
	host, port := srv.hostPort(t)

	client, err := NewDBClient(t.Context(), Config{Host: host, Port: port}, testLogger())
	if err != nil {
		t.Fatalf("NewDBClient: %v", err)
	}

	writeRow := func(value int64) error {
		return client.Write(t.Context(), func(ctx context.Context, sender qdbclient.LineSender) error {
			return sender.Table("heat").Int64Column("power", value).At(ctx, time.Unix(value, 0))
		})
	}

	if writeErr := writeRow(1); writeErr != nil {
		t.Fatalf("first write: %v", writeErr)
	}
	if flushErr := client.Flush(t.Context()); flushErr != nil {
		t.Fatalf("first flush: %v", flushErr)
	}
	waitFor(t, func() bool { return srv.bytesOn(0) > 0 }, "first row to reach the server")

	// QuestDB goes away. Freeze the clock so no redial happens while we prove
	// the writes into the dead socket fail.
	now := time.Now()
	client.now = func() time.Time { return now }
	closeAcceptedConns(t, srv)

	// A write to a socket the peer closed can succeed once before the RST
	// arrives, so keep going until the failure surfaces.
	var lastErr error
	for range 50 {
		if lastErr = writeRow(2); lastErr != nil {
			break
		}
		if lastErr = client.Flush(t.Context()); lastErr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr == nil {
		t.Fatal("writing to a closed connection never failed")
	}
	if srv.connections() != 1 {
		t.Fatalf("server accepted %d connections while the clock was frozen, want 1", srv.connections())
	}

	// Backoff elapses: the next write must land on a brand new connection.
	now = now.Add(maxReconnectDelay)
	if writeErr := writeRow(3); writeErr != nil {
		t.Fatalf("write after the backoff window: %v", writeErr)
	}
	if flushErr := client.Flush(t.Context()); flushErr != nil {
		t.Fatalf("flush after the backoff window: %v", flushErr)
	}
	waitFor(t, func() bool { return srv.bytesOn(1) > 0 }, "row to reach the server on the new connection")

	if got := srv.connections(); got != 2 {
		t.Errorf("server accepted %d connections, want 2 (the original plus one redial)", got)
	}
	if checkErr := client.Check(t.Context()); checkErr != nil {
		t.Errorf("Check() after the redial = %v, want nil", checkErr)
	}
}

// TestDBClient_ReplaysBufferedRowsOverTheWire is the end-to-end proof that a
// reading taken while QuestDB was down actually reaches QuestDB afterwards.
func TestDBClient_ReplaysBufferedRowsOverTheWire(t *testing.T) {
	srv := newILPServer(t)
	host, port := srv.hostPort(t)

	client, err := NewDBClient(
		t.Context(),
		Config{Host: host, Port: port, MaxBufferBytes: 1 << 20},
		testLogger(),
	)
	if err != nil {
		t.Fatalf("NewDBClient: %v", err)
	}

	writeRow := func(value int64) error {
		return client.Write(t.Context(), func(ctx context.Context, sender qdbclient.LineSender) error {
			return sender.Table("heat").Int64Column("power", value).At(ctx, time.Unix(value, 0))
		})
	}

	now := time.Now()
	client.now = func() time.Time { return now }
	waitFor(t, func() bool { return srv.connections() == 1 }, "the server to accept the first connection")
	closeAcceptedConns(t, srv)

	// Write and flush the way a source does until the flush reports the loss.
	// A write to a socket the peer closed can succeed once before the RST
	// arrives, and ILP over TCP has no server acknowledgement, so the rows
	// carried by that last falsely-successful flush are gone for good. From
	// the first reported failure on, every row is held for the replay.
	var flushErr error
	for value := int64(1); value <= 50 && flushErr == nil; value++ {
		if writeErr := writeRow(value); writeErr != nil {
			t.Fatalf("write %d during the outage: %v", value, writeErr)
		}
		flushErr = client.Flush(t.Context())
		time.Sleep(5 * time.Millisecond)
	}
	if flushErr == nil {
		t.Fatal("flushing into a closed connection never failed")
	}

	for value := int64(51); value <= 55; value++ {
		if writeErr := writeRow(value); writeErr != nil {
			t.Fatalf("write %d while disconnected: %v", value, writeErr)
		}
	}

	client.senderMu.Lock()
	buffered := client.buffer.len()
	client.senderMu.Unlock()
	if buffered == 0 {
		t.Fatal("nothing was buffered during the outage")
	}
	if !client.Degraded() {
		t.Error("Degraded() while buffering = false, want true")
	}

	// QuestDB comes back.
	now = now.Add(maxReconnectDelay)
	if recoveryErr := client.Flush(t.Context()); recoveryErr != nil {
		t.Fatalf("flush after the outage: %v", recoveryErr)
	}
	waitFor(t, func() bool { return srv.bytesOn(1) > 0 }, "buffered rows to reach the server")

	client.senderMu.Lock()
	remaining := client.buffer.len()
	client.senderMu.Unlock()
	if remaining != 0 {
		t.Errorf("%d rows still buffered after the replay, want 0", remaining)
	}
	if got := srv.bytesOn(1); got < buffered {
		t.Errorf("second connection received %d bytes for %d replayed rows", got, buffered)
	}
}

// closeAcceptedConns simulates a QuestDB restart by closing the listener's
// accepted connection from the server side.
func closeAcceptedConns(t *testing.T, srv *ilpServer) {
	t.Helper()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, c := range srv.conns {
		_ = c.Close()
	}
	srv.conns = nil
}

// waitFor polls cond for up to a second. The server reads on its own goroutine,
// so assertions on received bytes have to wait for it.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
