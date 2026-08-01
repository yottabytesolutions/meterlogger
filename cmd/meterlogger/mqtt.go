package main

import (
	"context"
	"log/slog"
	"sync"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mqtt"
	"github.com/yottabytesolutions/meterlogger/internal/healthserver"
)

// mqttConnect is a test seam over mqtt.NewClient so wiring tests can build
// the MQTT sink row without a real broker.
//
//nolint:gochecknoglobals // test seam, mirrors osExit
var mqttConnect = mqtt.NewClient

// sharedMQTT holds the single broker connection for the whole process. Unlike
// QuestDB, one MQTT client safely serves every source; the client serializes
// publishes internally.
//
//nolint:gochecknoglobals // process-wide shared connection, guarded by its mutex
var sharedMQTT struct {
	mu     sync.Mutex
	client *mqtt.Client
	err    error
	inited bool
}

// sharedMQTTClient lazily connects the process-wide MQTT client on first use
// and registers it with the health server. Subsequent callers get the same
// client (or the same connection error).
func sharedMQTTClient(
	ctx context.Context, l *slog.Logger, healthSrv *healthserver.Server,
) (*mqtt.Client, error) {
	sharedMQTT.mu.Lock()
	defer sharedMQTT.mu.Unlock()
	if sharedMQTT.inited {
		return sharedMQTT.client, sharedMQTT.err
	}
	sharedMQTT.inited = true

	client, err := mqttConnect(ctx, mqtt.Config{
		BrokerURL:              cfg.MQTT.BrokerURL,
		Username:               cfg.MQTT.Username,
		Password:               cfg.MQTT.Password,
		ClientID:               mqttClientID(),
		TopicPrefix:            cfg.MQTT.TopicPrefix,
		HomeAssistantDiscovery: cfg.MQTT.HomeAssistantDiscovery,
		DiscoveryPrefix:        cfg.MQTT.DiscoveryPrefix,
		QoS:                    byte(cfg.MQTT.QoS), //nolint:gosec // G115: validated to 0 or 1
		RetainState:            cfg.MQTT.RetainState,
	}, l)
	if err != nil {
		sharedMQTT.err = err
		return nil, err
	}
	if healthSrv != nil && client != nil {
		healthSrv.Register(client)
	}
	sharedMQTT.client = client
	return client, nil
}

// defaultMQTTClientID is the base MQTT client id when none is configured.
const defaultMQTTClientID = "meterlogger"

// mqttClientID resolves the configured client id, defaulting to "meterlogger"
// suffixed with the --source filter so the one-container-per-source model
// gets a unique id per process out of the box.
func mqttClientID() string {
	if cfg.MQTT.ClientID != "" {
		return cfg.MQTT.ClientID
	}
	if sourceFilter != "" {
		return defaultMQTTClientID + "-" + sourceFilter
	}
	return defaultMQTTClientID
}

// closeMQTT publishes the retained offline status and disconnects the shared
// client, if one was ever created.
func closeMQTT() {
	sharedMQTT.mu.Lock()
	defer sharedMQTT.mu.Unlock()
	if sharedMQTT.client != nil {
		if err := sharedMQTT.client.Close(); err != nil {
			logger.Error("mqtt close error", slog.Any("error", err))
		}
	}
	sharedMQTT.client, sharedMQTT.err, sharedMQTT.inited = nil, nil, false
}
