// Package mqtt provides an MQTT sink that publishes every meter reading as a
// flat JSON state message and announces the sensors to Home Assistant via
// retained MQTT discovery config messages. One Client is shared by every
// writer in the process; the paho client serializes publishes internally.
package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

const (
	connectTimeout = 10 * time.Second
	closeTimeout   = 5 * time.Second
	// disconnectQuiesceMs is how long paho waits for in-flight messages on
	// Disconnect, in milliseconds.
	disconnectQuiesceMs = 250

	payloadOnline  = "online"
	payloadOffline = "offline"
)

// Config holds the connection and topic parameters for the MQTT sink.
type Config struct {
	BrokerURL              string
	Username               string
	Password               string
	ClientID               string
	TopicPrefix            string
	HomeAssistantDiscovery bool
	DiscoveryPrefix        string
	QoS                    byte
	RetainState            bool
}

// Client wraps one paho MQTT connection. It implements healthserver.Checker
// and is shared by every writer in the process.
type Client struct {
	pc     paho.Client
	cfg    Config
	logger *slog.Logger
}

// NewClient connects to the broker with a bounded timeout. The connection
// carries a retained last-will message ("offline" on <TopicPrefix>/status)
// and publishes "online" on every (re)connect. Auto-reconnect is enabled.
func NewClient(ctx context.Context, cfg Config, logger *slog.Logger) (*Client, error) {
	logger.InfoContext(ctx, "mqtt: connecting", slog.String("broker", cfg.BrokerURL))
	c := newClient(paho.NewClient(clientOptions(cfg, logger)), cfg, logger)
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// newClient wires a Client around an existing paho client. Test seam.
func newClient(pc paho.Client, cfg Config, logger *slog.Logger) *Client {
	return &Client{pc: pc, cfg: cfg, logger: logger}
}

// clientOptions translates Config into paho client options, including the
// last-will testament and the on-connect birth message.
func clientOptions(cfg Config, logger *slog.Logger) *paho.ClientOptions {
	statusTopic := cfg.TopicPrefix + "/status"
	opts := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetAutoReconnect(true).
		SetWill(statusTopic, payloadOffline, cfg.QoS, true).
		SetOnConnectHandler(func(pc paho.Client) {
			tok := pc.Publish(statusTopic, cfg.QoS, true, payloadOnline)
			if tok.WaitTimeout(connectTimeout) && tok.Error() != nil {
				logger.Error("mqtt: publishing online status failed", slog.Any("error", tok.Error()))
			}
		})
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	return opts
}

func (c *Client) connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	return c.wait(ctx, c.pc.Connect(), "connect")
}

// wait blocks until the token resolves or ctx expires, whichever comes first.
func (c *Client) wait(ctx context.Context, tok paho.Token, op string) error {
	select {
	case <-tok.Done():
		if err := tok.Error(); err != nil {
			return fmt.Errorf("mqtt %s: %w", op, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("mqtt %s: %w", op, ctx.Err())
	}
}

// statusTopic is the availability topic shared by the last will, the birth
// message, and every discovery config.
func (c *Client) statusTopic() string {
	return c.cfg.TopicPrefix + "/status"
}

// stateTopic joins the configured topic prefix with the given path segments.
func (c *Client) stateTopic(parts ...string) string {
	return strings.Join(append([]string{c.cfg.TopicPrefix}, parts...), "/")
}

// publishState marshals payload to JSON and publishes it on topic with the
// configured QoS and retain flag.
func (c *Client) publishState(ctx context.Context, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mqtt marshal for %s: %w", topic, err)
	}
	return c.publish(ctx, topic, c.cfg.RetainState, body)
}

func (c *Client) publish(ctx context.Context, topic string, retain bool, body []byte) error {
	return c.wait(ctx, c.pc.Publish(topic, c.cfg.QoS, retain, body), "publish "+topic)
}

// Name implements healthserver.Checker.
func (c *Client) Name() string { return "mqtt" }

// Check implements healthserver.Checker. It reports the current connection
// state; paho reconnects in the background, so a closed connection means the
// broker has been unreachable since the last reconnect attempt.
func (c *Client) Check(_ context.Context) error {
	if !c.pc.IsConnectionOpen() {
		return errors.New("mqtt: connection to broker not open")
	}
	return nil
}

// Close publishes a retained "offline" status and disconnects cleanly.
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	err := c.publish(ctx, c.statusTopic(), true, []byte(payloadOffline))
	c.pc.Disconnect(disconnectQuiesceMs)
	if err != nil {
		return fmt.Errorf("mqtt close: %w", err)
	}
	return nil
}

// onceRegistry tracks which discovery announcements have been made so each
// retained config set is published once per process. A failed announcement is
// released so the next reading retries it.
type onceRegistry struct {
	mu   sync.Mutex
	done map[string]bool
}

func newOnceRegistry() *onceRegistry {
	return &onceRegistry{done: make(map[string]bool)}
}

// claim marks key as announced and reports whether the caller won the claim.
func (r *onceRegistry) claim(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done[key] {
		return false
	}
	r.done[key] = true
	return true
}

// release undoes a claim after a failed announcement so it can be retried.
func (r *onceRegistry) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.done, key)
}
