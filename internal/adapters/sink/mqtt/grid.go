package mqtt

import (
	"context"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// GridWriter implements domain.GridTelegramRepository over MQTT.
type GridWriter struct {
	client      *Client
	measurement string
	logger      *slog.Logger
	announced   *onceRegistry
}

// NewGridWriter creates a grid telegram writer publishing on
// <TopicPrefix>/<measurement>.
func NewGridWriter(client *Client, measurement string, logger *slog.Logger) *GridWriter {
	return &GridWriter{client: client, measurement: measurement, logger: logger, announced: newOnceRegistry()}
}

// gridPayload is the flat JSON state message for one P1 telegram. Field names
// match the SQL sink columns. Counters are kWh, powers W, voltages V,
// currents A.
type gridPayload struct {
	TS               string  `json:"ts"`
	MeterType        string  `json:"meter_type"`
	SerialNo         string  `json:"serial_no"`
	UsageCounter1    float64 `json:"usage_counter1"`
	UsageCounter2    float64 `json:"usage_counter2"`
	OutputCounter1   float64 `json:"output_counter1"`
	OutputCounter2   float64 `json:"output_counter2"`
	TotalPowerUsage  int     `json:"total_power_usage"`
	TotalPowerOutput int     `json:"total_power_output"`
	BrownoutsP1      int     `json:"brownouts_p1"`
	BrownoutsP2      int     `json:"brownouts_p2"`
	BrownoutsP3      int     `json:"brownouts_p3"`
	SpikesP1         int     `json:"spikes_p1"`
	SpikesP2         int     `json:"spikes_p2"`
	SpikesP3         int     `json:"spikes_p3"`
	VoltageP1        float64 `json:"voltage_p1"`
	VoltageP2        float64 `json:"voltage_p2"`
	VoltageP3        float64 `json:"voltage_p3"`
	CurrentP1        int     `json:"current_p1"`
	CurrentP2        int     `json:"current_p2"`
	CurrentP3        int     `json:"current_p3"`
	PowerUsageP1     int     `json:"power_usage_p1"`
	PowerUsageP2     int     `json:"power_usage_p2"`
	PowerUsageP3     int     `json:"power_usage_p3"`
	PowerOutputP1    int     `json:"power_output_p1"`
	PowerOutputP2    int     `json:"power_output_p2"`
	PowerOutputP3    int     `json:"power_output_p3"`
}

// StoreGridTelegram publishes one grid telegram. On the first telegram that
// carries the meter serial it also announces the Home Assistant sensors.
func (w *GridWriter) StoreGridTelegram(ctx context.Context, t domain.GridTelegram) error {
	topic := w.client.stateTopic(w.measurement)
	if err := w.announce(ctx, topic, t); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing grid telegram", slog.String("topic", topic))
	return w.client.publishState(ctx, topic, gridPayload{
		TS:               t.Time.Format(time.RFC3339),
		MeterType:        t.MeterMerkType,
		SerialNo:         t.Serienummer,
		UsageCounter1:    t.UsageCounter1,
		UsageCounter2:    t.UsageCounter2,
		OutputCounter1:   t.OutputCounter1,
		OutputCounter2:   t.OutputCounter2,
		TotalPowerUsage:  t.TotalPowerUsage,
		TotalPowerOutput: t.TotalPowerOutput,
		BrownoutsP1:      t.BrownoutsP1,
		BrownoutsP2:      t.BrownoutsP2,
		BrownoutsP3:      t.BrownoutsP3,
		SpikesP1:         t.SpikesP1,
		SpikesP2:         t.SpikesP2,
		SpikesP3:         t.SpikesP3,
		VoltageP1:        t.VoltageP1,
		VoltageP2:        t.VoltageP2,
		VoltageP3:        t.VoltageP3,
		CurrentP1:        t.CurrentP1,
		CurrentP2:        t.CurrentP2,
		CurrentP3:        t.CurrentP3,
		PowerUsageP1:     t.PowerUsageP1,
		PowerUsageP2:     t.PowerUsageP2,
		PowerUsageP3:     t.PowerUsageP3,
		PowerOutputP1:    t.PowerOutputP1,
		PowerOutputP2:    t.PowerOutputP2,
		PowerOutputP3:    t.PowerOutputP3,
	})
}

func (w *GridWriter) announce(ctx context.Context, topic string, t domain.GridTelegram) error {
	if t.Serienummer == "" || !w.announced.claim(t.Serienummer) {
		return nil
	}
	id := sanitizeID("meterlogger_grid_" + t.Serienummer)
	dev := deviceInfo{
		Identifiers: []string{id},
		Name:        "Grid meter " + t.Serienummer,
		Model:       t.MeterMerkType,
	}
	if err := w.client.publishDiscovery(ctx, id, dev, topic, gridSensors()); err != nil {
		w.announced.release(t.Serienummer)
		return err
	}
	return nil
}

func gridSensors() []sensor {
	sensors := []sensor{
		{
			field: "usage_counter1", name: "Energy consumed tariff 1",
			deviceClass: classEnergy, stateClass: stateTotalIncreasing, unit: unitKWh,
		},
		{
			field: "usage_counter2", name: "Energy consumed tariff 2",
			deviceClass: classEnergy, stateClass: stateTotalIncreasing, unit: unitKWh,
		},
		{
			field: "output_counter1", name: "Energy produced tariff 1",
			deviceClass: classEnergy, stateClass: stateTotalIncreasing, unit: unitKWh,
		},
		{
			field: "output_counter2", name: "Energy produced tariff 2",
			deviceClass: classEnergy, stateClass: stateTotalIncreasing, unit: unitKWh,
		},
		{
			field: "total_power_usage", name: "Power consumption",
			deviceClass: classPower, stateClass: stateMeasurement, unit: unitW,
		},
		{
			field: "total_power_output", name: "Power production",
			deviceClass: classPower, stateClass: stateMeasurement, unit: unitW,
		},
	}
	for _, phase := range []string{"1", "2", "3"} {
		sensors = append(sensors,
			sensor{
				field: "voltage_p" + phase, name: "Voltage L" + phase,
				deviceClass: classVoltage, stateClass: stateMeasurement, unit: unitVolt,
			},
			sensor{
				field: "current_p" + phase, name: "Current L" + phase,
				deviceClass: classCurrent, stateClass: stateMeasurement, unit: unitAmpere,
			},
			sensor{
				field: "power_usage_p" + phase, name: "Power consumption L" + phase,
				deviceClass: classPower, stateClass: stateMeasurement, unit: unitW,
			},
			sensor{
				field: "power_output_p" + phase, name: "Power production L" + phase,
				deviceClass: classPower, stateClass: stateMeasurement, unit: unitW,
			},
			sensor{
				field: "brownouts_p" + phase, name: "Brownouts L" + phase,
				stateClass: stateTotalIncreasing, diagnostic: true,
			},
			sensor{
				field: "spikes_p" + phase, name: "Voltage spikes L" + phase,
				stateClass: stateTotalIncreasing, diagnostic: true,
			},
		)
	}
	return sensors
}

// Flush is a no-op; every publish goes straight to the broker.
func (w *GridWriter) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared Client is closed at process shutdown.
func (w *GridWriter) Close() error { return nil }
