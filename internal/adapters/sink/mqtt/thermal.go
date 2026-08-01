package mqtt

import (
	"context"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// ThermalWriter implements domain.ThermalRepository over MQTT.
type ThermalWriter struct {
	client      *Client
	measurement string
	logger      *slog.Logger
	announced   *onceRegistry
}

// NewThermalWriter creates a thermal reading writer publishing on
// <TopicPrefix>/<measurement>.
func NewThermalWriter(client *Client, measurement string, logger *slog.Logger) *ThermalWriter {
	return &ThermalWriter{client: client, measurement: measurement, logger: logger, announced: newOnceRegistry()}
}

// thermalPayload is the flat JSON state message for one thermal reading. Field names
// match the SQL sink columns.
type thermalPayload struct {
	TS         string  `json:"ts"`
	ReceivedAt string  `json:"received_at"`
	Channel    int     `json:"channel"`
	DeviceType int     `json:"device_type"`
	SerialNo   string  `json:"serial_no"`
	ReadingGJ  float64 `json:"reading_gj"`
}

// StoreThermalReading publishes one thermal reading. On the first reading that
// carries the meter serial it also announces the Home Assistant sensor.
func (w *ThermalWriter) StoreThermalReading(ctx context.Context, r domain.ThermalReading) error {
	topic := w.client.stateTopic(w.measurement)
	if err := w.announce(ctx, topic, r); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing thermal reading", slog.String("topic", topic))
	return w.client.publishState(ctx, topic, thermalPayload{
		TS:         r.CapturedAt.Format(time.RFC3339),
		ReceivedAt: r.ReceivedAt.Format(time.RFC3339),
		Channel:    r.Channel,
		DeviceType: r.DeviceType,
		SerialNo:   r.SerialNo,
		ReadingGJ:  r.ReadingGJ,
	})
}

func (w *ThermalWriter) announce(ctx context.Context, topic string, r domain.ThermalReading) error {
	if r.SerialNo == "" || !w.announced.claim(r.SerialNo) {
		return nil
	}
	id := sanitizeID("meterlogger_thermal_" + r.SerialNo)
	dev := deviceInfo{
		Identifiers: []string{id},
		Name:        "Thermal meter " + r.SerialNo,
	}
	sensors := []sensor{
		{
			field:       "reading_gj",
			name:        "Thermal energy",
			deviceClass: classEnergy,
			stateClass:  stateTotalIncreasing,
			unit:        unitGJ,
		},
	}
	if err := w.client.publishDiscovery(ctx, id, dev, topic, sensors); err != nil {
		w.announced.release(r.SerialNo)
		return err
	}
	return nil
}

// Flush is a no-op; every publish goes straight to the broker.
func (w *ThermalWriter) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared Client is closed at process shutdown.
func (w *ThermalWriter) Close() error { return nil }
