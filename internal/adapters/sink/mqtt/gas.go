//nolint:dupl // one writer per subdevice kind; parallel by design, distinct domain types
package mqtt

import (
	"context"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GasWriter implements domain.GasRepository over MQTT.
type GasWriter struct {
	client      *Client
	measurement string
	logger      *slog.Logger
	announced   *onceRegistry
}

// NewGasWriter creates a gas reading writer publishing on
// <TopicPrefix>/<measurement>.
func NewGasWriter(client *Client, measurement string, logger *slog.Logger) *GasWriter {
	return &GasWriter{client: client, measurement: measurement, logger: logger, announced: newOnceRegistry()}
}

// gasPayload is the flat JSON state message for one gas reading. Field names
// match the SQL sink columns.
type gasPayload struct {
	TS         string  `json:"ts"`
	ReceivedAt string  `json:"received_at"`
	Channel    int     `json:"channel"`
	DeviceType int     `json:"device_type"`
	SerialNo   string  `json:"serial_no"`
	ReadingM3  float64 `json:"reading_m3"`
}

// StoreGasReading publishes one gas reading. On the first reading that
// carries the meter serial it also announces the Home Assistant sensor.
func (w *GasWriter) StoreGasReading(ctx context.Context, r domain.GasReading) error {
	topic := w.client.stateTopic(w.measurement)
	if err := w.announce(ctx, topic, r); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing gas reading", slog.String("topic", topic))
	return w.client.publishState(ctx, topic, gasPayload{
		TS:         r.CapturedAt.Format(time.RFC3339),
		ReceivedAt: r.ReceivedAt.Format(time.RFC3339),
		Channel:    r.Channel,
		DeviceType: r.DeviceType,
		SerialNo:   r.SerialNo,
		ReadingM3:  r.ReadingM3,
	})
}

func (w *GasWriter) announce(ctx context.Context, topic string, r domain.GasReading) error {
	if r.SerialNo == "" || !w.announced.claim(r.SerialNo) {
		return nil
	}
	id := sanitizeID("meterlogger_gas_" + r.SerialNo)
	dev := deviceInfo{
		Identifiers: []string{id},
		Name:        "Gas meter " + r.SerialNo,
	}
	sensors := []sensor{
		{
			field:       fieldReadingM3,
			name:        "Gas consumption",
			deviceClass: classGas,
			stateClass:  stateTotalIncreasing,
			unit:        unitM3,
		},
	}
	if err := w.client.publishDiscovery(ctx, id, dev, topic, sensors); err != nil {
		w.announced.release(r.SerialNo)
		return err
	}
	return nil
}

// Flush is a no-op; every publish goes straight to the broker.
func (w *GasWriter) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared Client is closed at process shutdown.
func (w *GasWriter) Close() error { return nil }
