package mqtt

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// fakeToken resolves immediately with a fixed error.
type fakeToken struct {
	err error
}

func (t *fakeToken) Wait() bool                     { return true }
func (t *fakeToken) WaitTimeout(time.Duration) bool { return true }
func (t *fakeToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (t *fakeToken) Error() error { return t.err }

// publication records one Publish call.
type publication struct {
	topic    string
	qos      byte
	retained bool
	payload  []byte
}

// fakePaho is a recording in-memory paho.Client.
type fakePaho struct {
	mu           sync.Mutex
	published    []publication
	publishErrOn string // topics containing this substring fail
	publishErr   error
	connectErr   error
	open         bool
	disconnected bool
}

func newFakePaho() *fakePaho { return &fakePaho{open: true} }

func (f *fakePaho) IsConnected() bool      { return f.open }
func (f *fakePaho) IsConnectionOpen() bool { return f.open }

func (f *fakePaho) Connect() paho.Token {
	if f.connectErr == nil {
		f.open = true
	}
	return &fakeToken{err: f.connectErr}
}

func (f *fakePaho) Disconnect(_ uint) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnected = true
	f.open = false
}

func (f *fakePaho) Publish(topic string, qos byte, retained bool, payload any) paho.Token {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body []byte
	switch p := payload.(type) {
	case []byte:
		body = p
	case string:
		body = []byte(p)
	}
	f.published = append(f.published, publication{topic: topic, qos: qos, retained: retained, payload: body})
	if f.publishErr != nil && (f.publishErrOn == "" || strings.Contains(topic, f.publishErrOn)) {
		return &fakeToken{err: f.publishErr}
	}
	return &fakeToken{}
}

func (f *fakePaho) Subscribe(string, byte, paho.MessageHandler) paho.Token { return &fakeToken{} }
func (f *fakePaho) SubscribeMultiple(map[string]byte, paho.MessageHandler) paho.Token {
	return &fakeToken{}
}
func (f *fakePaho) Unsubscribe(...string) paho.Token        { return &fakeToken{} }
func (f *fakePaho) AddRoute(string, paho.MessageHandler)    {}
func (f *fakePaho) OptionsReader() paho.ClientOptionsReader { return paho.ClientOptionsReader{} }

// byTopic returns every recorded publication whose topic matches exactly.
func (f *fakePaho) byTopic(topic string) []publication {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []publication
	for _, p := range f.published {
		if p.topic == topic {
			out = append(out, p)
		}
	}
	return out
}

func testConfig() Config {
	return Config{
		BrokerURL:              "tcp://broker:1883",
		ClientID:               "meterlogger-test",
		TopicPrefix:            "meterlogger",
		HomeAssistantDiscovery: true,
		DiscoveryPrefix:        "homeassistant",
		QoS:                    1,
	}
}

// testStatusTopic is the availability topic derived from testConfig.
const testStatusTopic = "meterlogger/status"

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// newTestClient wires a Client around a fake paho connection.
func newTestClient(fp *fakePaho, cfg Config) *Client {
	return newClient(fp, cfg, testLogger())
}
