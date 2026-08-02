package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Home Assistant device classes, state classes, and units used by the sensor
// definitions. Reference: https://www.home-assistant.io/integrations/sensor.mqtt/
const (
	classEnergy      = "energy"
	classPower       = "power"
	classTemperature = "temperature"
	classVoltage     = "voltage"
	classCurrent     = "current"
	classGas         = "gas"
	classWater       = "water"
	classCO2         = "carbon_dioxide"
	classHumidity    = "humidity"
	classFrequency   = "frequency"
	classSignal      = "signal_strength"

	stateMeasurement     = "measurement"
	stateTotalIncreasing = "total_increasing"

	unitKWh     = "kWh"
	unitWh      = "Wh"
	unitW       = "W"
	unitCelsius = "°C"
	unitVolt    = "V"
	unitAmpere  = "A"
	unitHertz   = "Hz"
	unitM3      = "m³"
	unitGJ      = "GJ"

	// fieldReadingM3 is the JSON field and HA value key shared by the gas
	// and water writers.
	fieldReadingM3 = "reading_m3"
	unitPPM        = "ppm"
	unitPercent    = "%"
	unitRPM        = "rpm"
)

// sensor describes one Home Assistant entity derived from a state topic. id
// is the object and unique id suffix; when empty, field is used. field is the
// JSON key the value_template extracts from the state payload.
type sensor struct {
	id          string
	field       string
	name        string
	deviceClass string
	stateClass  string
	unit        string
	diagnostic  bool
}

func (s sensor) objectID() string {
	if s.id != "" {
		return s.id
	}
	return s.field
}

// deviceInfo is the Home Assistant device block that groups all sensors of
// one physical meter.
type deviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
}

// availability points Home Assistant at the sink's status topic so entities
// go unavailable when the process stops or loses the broker.
type availability struct {
	Topic string `json:"topic"`
}

// discoveryConfig is one Home Assistant MQTT discovery config message.
type discoveryConfig struct {
	UniqueID            string         `json:"unique_id"`
	Name                string         `json:"name"`
	StateTopic          string         `json:"state_topic"`
	ValueTemplate       string         `json:"value_template"`
	Availability        []availability `json:"availability"`
	PayloadAvailable    string         `json:"payload_available"`
	PayloadNotAvailable string         `json:"payload_not_available"`
	Device              deviceInfo     `json:"device"`
	DeviceClass         string         `json:"device_class,omitempty"`
	StateClass          string         `json:"state_class,omitempty"`
	UnitOfMeasurement   string         `json:"unit_of_measurement,omitempty"`
	EntityCategory      string         `json:"entity_category,omitempty"`
}

// publishDiscovery announces one retained config message per sensor under
// <DiscoveryPrefix>/sensor/<nodeID>/<objectID>/config. nodeID doubles as the
// discovery node id and must be unique per device; it is sanitized by the
// caller. No-op when discovery is disabled.
func (c *Client) publishDiscovery(
	ctx context.Context, nodeID string, dev deviceInfo, stateTopic string, sensors []sensor,
) error {
	if !c.cfg.HomeAssistantDiscovery {
		return nil
	}
	var errs []error
	for _, s := range sensors {
		msg := discoveryConfig{
			UniqueID:            nodeID + "_" + s.objectID(),
			Name:                s.name,
			StateTopic:          stateTopic,
			ValueTemplate:       "{{ value_json." + s.field + " }}",
			Availability:        []availability{{Topic: c.statusTopic()}},
			PayloadAvailable:    payloadOnline,
			PayloadNotAvailable: payloadOffline,
			Device:              dev,
			DeviceClass:         s.deviceClass,
			StateClass:          s.stateClass,
			UnitOfMeasurement:   s.unit,
		}
		if s.diagnostic {
			msg.EntityCategory = "diagnostic"
		}
		topic := c.cfg.DiscoveryPrefix + "/sensor/" + nodeID + "/" + s.objectID() + "/config"
		body, err := json.Marshal(msg)
		if err != nil {
			errs = append(errs, fmt.Errorf("mqtt marshal discovery for %s: %w", topic, err))
			continue
		}
		// Discovery configs are always retained so Home Assistant restores
		// the entities after its own restart, regardless of RetainState.
		if pubErr := c.publish(ctx, topic, true, body); pubErr != nil {
			errs = append(errs, pubErr)
		}
	}
	return errors.Join(errs...)
}

// sanitizeID keeps only characters that are valid in discovery node ids and
// MQTT topic levels, replacing everything else with an underscore.
func sanitizeID(s string) string {
	out := []rune(s)
	for i, r := range out {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			out[i] = '_'
		}
	}
	return string(out)
}
