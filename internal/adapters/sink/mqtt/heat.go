package mqtt

import (
	"context"
	"log/slog"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// Unit conversions from the domain telegram (Joules, watts, cm3) to the
// physical units published over MQTT. Home Assistant reads energy in kWh; the
// GJ total is published alongside it for consumers that prefer it.
const (
	joulesPerGigajoule = 1e9
	joulesPerKWh       = 3.6e6
	cm3PerM3           = 1e6
)

// HeatWriter implements domain.HeatMeterRepository over MQTT.
type HeatWriter struct {
	client      *Client
	measurement string
	logger      *slog.Logger
	announced   *onceRegistry
}

// NewHeatWriter creates a heat telegram writer publishing on
// <TopicPrefix>/<measurement>.
func NewHeatWriter(client *Client, measurement string, logger *slog.Logger) *HeatWriter {
	return &HeatWriter{client: client, measurement: measurement, logger: logger, announced: newOnceRegistry()}
}

// heatPayload is the flat JSON state message for one heat telegram. Field
// names match the SQL sink columns; derived kWh and m3 fields are added for
// Home Assistant.
type heatPayload struct {
	TS        string  `json:"ts"`
	MeterID   string  `json:"meter_id"`
	SerialNo  string  `json:"serial_no"`
	PowerW    float64 `json:"power_w"`
	EnergyGJ  float64 `json:"energy_gj"`
	EnergyKWh float64 `json:"energy_kwh"`
	TForwardC float64 `json:"t_forward_c"`
	TReturnC  float64 `json:"t_return_c"`
	TDiffC    float64 `json:"t_diff_c"`
	VolumeCm3 float64 `json:"volume_cm3"`
	VolumeM3  float64 `json:"volume_m3"`
	Seconds   int64   `json:"seconds"`
	MaxFlow   float64 `json:"max_flow"`
	MaxPowerW float64 `json:"max_power_w"`
}

// StoreHeatTelegram publishes one heat telegram. On the first telegram that
// carries the meter serial it also announces the Home Assistant sensors.
func (w *HeatWriter) StoreHeatTelegram(ctx context.Context, t domain.HeatTelegram) error {
	topic := w.client.stateTopic(w.measurement)
	if err := w.announce(ctx, topic, t); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing heat telegram", slog.String("topic", topic))
	return w.client.publishState(ctx, topic, heatPayload{
		TS:        t.Timestamp.Format(time.RFC3339),
		MeterID:   t.MeterID,
		SerialNo:  t.SerialNo,
		PowerW:    float64(t.ActualPower),
		EnergyGJ:  float64(t.Joules) / joulesPerGigajoule,
		EnergyKWh: float64(t.Joules) / joulesPerKWh,
		TForwardC: t.Tforward,
		TReturnC:  t.Treturn,
		TDiffC:    t.Tdiff,
		VolumeCm3: t.VolumeCm3,
		VolumeM3:  t.VolumeCm3 / cm3PerM3,
		Seconds:   t.SecondsCounter,
		MaxFlow:   t.MaxFlow,
		MaxPowerW: float64(t.MaxPower),
	})
}

func (w *HeatWriter) announce(ctx context.Context, topic string, t domain.HeatTelegram) error {
	if t.SerialNo == "" || !w.announced.claim(t.SerialNo) {
		return nil
	}
	id := sanitizeID("meterlogger_heat_" + t.SerialNo)
	dev := deviceInfo{
		Identifiers:  []string{id},
		Name:         "Heat meter " + t.SerialNo,
		Manufacturer: "Kamstrup",
		Model:        "Multical " + t.MeterID,
	}
	if err := w.client.publishDiscovery(ctx, id, dev, topic, heatSensors()); err != nil {
		w.announced.release(t.SerialNo)
		return err
	}
	return nil
}

func heatSensors() []sensor {
	return []sensor{
		{
			field:       "energy_kwh",
			name:        "Energy",
			deviceClass: classEnergy,
			stateClass:  stateTotalIncreasing,
			unit:        unitKWh,
		},
		{field: "power_w", name: "Power", deviceClass: classPower, stateClass: stateMeasurement, unit: unitW},
		{
			field: "t_forward_c", name: "Flow temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{
			field: "t_return_c", name: "Return temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{
			field: "t_diff_c", name: "Temperature difference",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{field: "volume_m3", name: "Volume", stateClass: stateTotalIncreasing, unit: unitM3},
	}
}

// Flush is a no-op; every publish goes straight to the broker.
func (w *HeatWriter) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared Client is closed at process shutdown.
func (w *HeatWriter) Close() error { return nil }
