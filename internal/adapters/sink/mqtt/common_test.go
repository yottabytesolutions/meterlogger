package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestClientOptions(t *testing.T) {
	cfg := testConfig()
	cfg.Username = "user"
	cfg.Password = "secret"
	opts := clientOptions(cfg, slog.New(slog.DiscardHandler))

	if len(opts.Servers) != 1 || opts.Servers[0].String() != "tcp://broker:1883" {
		t.Errorf("Servers = %v, want [tcp://broker:1883]", opts.Servers)
	}
	if opts.ClientID != "meterlogger-test" {
		t.Errorf("ClientID = %q", opts.ClientID)
	}
	if opts.Username != "user" || opts.Password != "secret" {
		t.Errorf("credentials = %q/%q", opts.Username, opts.Password)
	}
	if !opts.AutoReconnect {
		t.Error("AutoReconnect should be enabled")
	}
	if !opts.WillEnabled || opts.WillTopic != testStatusTopic ||
		string(opts.WillPayload) != payloadOffline || !opts.WillRetained || opts.WillQos != 1 {
		t.Errorf("will = enabled:%v topic:%q payload:%q retained:%v qos:%d",
			opts.WillEnabled, opts.WillTopic, opts.WillPayload, opts.WillRetained, opts.WillQos)
	}
}

func TestClientOptions_OnConnectPublishesOnline(t *testing.T) {
	opts := clientOptions(testConfig(), slog.New(slog.DiscardHandler))
	fp := newFakePaho()

	opts.OnConnect(fp)

	pubs := fp.byTopic(testStatusTopic)
	if len(pubs) != 1 {
		t.Fatalf("status publications = %d, want 1", len(pubs))
	}
	if string(pubs[0].payload) != payloadOnline || !pubs[0].retained || pubs[0].qos != 1 {
		t.Errorf("online publication = %+v", pubs[0])
	}
}

func TestConnect_Error(t *testing.T) {
	fp := newFakePaho()
	fp.connectErr = errors.New("refused")
	c := newTestClient(fp, testConfig())

	if err := c.connect(context.Background()); err == nil {
		t.Error("connect should propagate the token error")
	}
}

func TestCheck(t *testing.T) {
	fp := newFakePaho()
	c := newTestClient(fp, testConfig())

	if err := c.Check(context.Background()); err != nil {
		t.Errorf("Check on open connection = %v, want nil", err)
	}
	fp.open = false
	if err := c.Check(context.Background()); err == nil {
		t.Error("Check on closed connection should return an error")
	}
}

func TestClose_PublishesOfflineAndDisconnects(t *testing.T) {
	fp := newFakePaho()
	c := newTestClient(fp, testConfig())

	if err := c.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	pubs := fp.byTopic(testStatusTopic)
	if len(pubs) != 1 || string(pubs[0].payload) != payloadOffline || !pubs[0].retained {
		t.Errorf("offline publication = %v", pubs)
	}
	if !fp.disconnected {
		t.Error("Close should disconnect the paho client")
	}
}

func TestClose_ReturnsPublishError(t *testing.T) {
	fp := newFakePaho()
	fp.publishErr = errors.New("broker gone")
	c := newTestClient(fp, testConfig())

	if err := c.Close(); err == nil {
		t.Error("Close should return the publish error")
	}
	if !fp.disconnected {
		t.Error("Close should disconnect even when the offline publish fails")
	}
}

func TestName(t *testing.T) {
	c := newTestClient(newFakePaho(), testConfig())
	if got := c.Name(); got != "mqtt" {
		t.Errorf("Name() = %q, want mqtt", got)
	}
}

func TestOnceRegistry(t *testing.T) {
	r := newOnceRegistry()
	if !r.claim("a") {
		t.Error("first claim should succeed")
	}
	if r.claim("a") {
		t.Error("second claim should fail")
	}
	r.release("a")
	if !r.claim("a") {
		t.Error("claim after release should succeed")
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"abc-123_X", "abc-123_X"},
		{"a b/c#d", "a_b_c_d"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := sanitizeID(tt.in); got != tt.want {
			t.Errorf("sanitizeID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
