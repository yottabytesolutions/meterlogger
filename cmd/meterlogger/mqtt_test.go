package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/adapters/sink/mqtt"
	"github.com/yottabytesolutions/meterlogger/internal/config"
)

// resetSharedMQTT clears the process-wide MQTT state around a test and stubs
// the connect seam with the given function.
func resetSharedMQTT(t *testing.T, connect func(context.Context, mqtt.Config, *slog.Logger) (*mqtt.Client, error)) {
	t.Helper()
	origConnect := mqttConnect
	mqttConnect = connect
	sharedMQTT.client, sharedMQTT.err, sharedMQTT.inited = nil, nil, false
	t.Cleanup(func() {
		mqttConnect = origConnect
		sharedMQTT.client, sharedMQTT.err, sharedMQTT.inited = nil, nil, false
	})
}

func TestMQTTClientID(t *testing.T) {
	origCfg, origFilter := cfg, sourceFilter
	defer func() { cfg, sourceFilter = origCfg, origFilter }()

	tests := []struct {
		name     string
		clientID string
		filter   string
		want     string
	}{
		{"default", "", "", defaultMQTTClientID},
		{"default with source filter", "", config.SourceGrid, defaultMQTTClientID + "-" + config.SourceGrid},
		{"explicit id wins over filter", "custom", config.SourceGrid, "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg = config.Config{MQTT: config.MQTTConfig{ClientID: tt.clientID}}
			sourceFilter = tt.filter
			if got := mqttClientID(); got != tt.want {
				t.Errorf("mqttClientID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSourceSinks_MQTTOnly(t *testing.T) {
	origCfg := cfg
	cfg = config.Config{MQTT: config.MQTTConfig{Enabled: true, BrokerURL: "tcp://broker:1883"}}
	defer func() { cfg = origCfg }()

	connects := 0
	resetSharedMQTT(t, func(context.Context, mqtt.Config, *slog.Logger) (*mqtt.Client, error) {
		connects++
		return &mqtt.Client{}, nil
	})

	ctx := context.Background()
	l := testLogger()
	var dbs dbConnections

	if got := len(buildHeatSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("heat sinks = %d, want 1", got)
	}
	if got := len(buildGridSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("grid sinks = %d, want 1", got)
	}
	if got := len(buildGasSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("gas sinks = %d, want 1", got)
	}
	if got := len(buildSolarSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("solar sinks = %d, want 1", got)
	}
	if got := len(buildVentilationSinks(ctx, l, nil, dbs)); got != 1 {
		t.Errorf("ventilation sinks = %d, want 1", got)
	}
	if connects != 1 {
		t.Errorf("mqtt connects = %d, want 1 shared connection", connects)
	}
}

func TestSharedMQTTClient_CachesConnectionError(t *testing.T) {
	origCfg := cfg
	cfg = config.Config{MQTT: config.MQTTConfig{Enabled: true, BrokerURL: "tcp://broker:1883"}}
	defer func() { cfg = origCfg }()

	connects := 0
	resetSharedMQTT(t, func(context.Context, mqtt.Config, *slog.Logger) (*mqtt.Client, error) {
		connects++
		return nil, errors.New("connection refused")
	})

	ctx := context.Background()
	if _, err := sharedMQTTClient(ctx, testLogger(), nil); err == nil {
		t.Fatal("first call should return the connection error")
	}
	if _, err := sharedMQTTClient(ctx, testLogger(), nil); err == nil {
		t.Fatal("second call should return the cached connection error")
	}
	if connects != 1 {
		t.Errorf("connect attempts = %d, want 1", connects)
	}
}

func TestCloseMQTT_NoClientIsNoop(t *testing.T) {
	resetSharedMQTT(t, mqttConnect)
	closeMQTT() // must not panic
	if sharedMQTT.inited {
		t.Error("closeMQTT should reset the shared state")
	}
}
