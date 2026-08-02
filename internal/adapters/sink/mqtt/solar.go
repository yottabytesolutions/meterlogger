package mqtt

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// SolarWriter implements domain.EnvoySolarRepository over MQTT.
type SolarWriter struct {
	client      *Client
	measurement string
	logger      *slog.Logger
	announced   *onceRegistry
}

// NewSolarWriter creates a solar snapshot writer. The gateway snapshot is
// published on <TopicPrefix>/<measurement>; each microinverter on
// <TopicPrefix>/<measurement>_inverters/<inverter_serial>.
func NewSolarWriter(client *Client, measurement string, logger *slog.Logger) *SolarWriter {
	return &SolarWriter{client: client, measurement: measurement, logger: logger, announced: newOnceRegistry()}
}

// solarPayload is the flat JSON state message for one Envoy snapshot. Field
// names match the SQL sink columns.
type solarPayload struct {
	TS           string  `json:"ts"`
	EnvoySerial  string  `json:"envoy_serial"`
	ProductionWh float64 `json:"production_wh"`
	Watt         float64 `json:"watt"`
	PanelCount   int     `json:"panel_count"`
}

// inverterPayload is the flat JSON state message for one microinverter row.
// Field names match the SQL sink's inverter table columns.
type inverterPayload struct {
	TS             string  `json:"ts"`
	EnvoySerial    string  `json:"envoy_serial"`
	InverterSerial string  `json:"inverter_serial"`
	ChannelID      int     `json:"channel_id"`
	Operating      bool    `json:"operating"`
	Communicating  bool    `json:"communicating"`
	Producing      bool    `json:"producing"`
	Phase          string  `json:"phase"`
	Watts          int     `json:"watts"`
	PeakWatts      int     `json:"peak_watts"`
	Status         string  `json:"status"`
	DCVoltage      float64 `json:"dc_voltage"`
	DCCurrent      float64 `json:"dc_current"`
	ACVoltage      float64 `json:"ac_voltage"`
	ACCurrent      float64 `json:"ac_current"`
	ACFrequency    float64 `json:"ac_frequency"`
	TemperatureC   int     `json:"temperature_c"`
	LeadingVArs    int     `json:"leading_vars"`
	LaggingVArs    int     `json:"lagging_vars"`
	WhToday        int     `json:"wh_today"`
	WhYesterday    int     `json:"wh_yesterday"`
	WhWeek         int     `json:"wh_week"`
	WhLifetime     float64 `json:"wh_lifetime"`
	RSSI           int     `json:"rssi"`
	ISSI           int     `json:"issi"`
}

// StoreEnvoySolarData publishes the gateway snapshot and one message per
// microinverter. Sensors are announced to Home Assistant on first sight of
// each serial.
func (w *SolarWriter) StoreEnvoySolarData(ctx context.Context, d domain.EnvoySolarData) error {
	topic := w.client.stateTopic(w.measurement)
	if err := w.announceEnvoy(ctx, topic, d); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing solar snapshot", slog.String("topic", topic))
	err := w.client.publishState(ctx, topic, solarPayload{
		TS:           d.ReadingTime.Format(time.RFC3339),
		EnvoySerial:  d.EnvoySerial,
		ProductionWh: d.ProductionWh,
		Watt:         d.Watt,
		PanelCount:   d.PanelCount,
	})

	errs := []error{err}
	for _, inv := range d.Inverters {
		errs = append(errs, w.storeInverter(ctx, d, inv))
	}
	return errors.Join(errs...)
}

func (w *SolarWriter) storeInverter(
	ctx context.Context, d domain.EnvoySolarData, inv domain.InverterDetails,
) error {
	topic := w.client.stateTopic(w.measurement+"_inverters", sanitizeID(inv.SerialNumber))
	if err := w.announceInverter(ctx, topic, d, inv); err != nil {
		return err
	}
	return w.client.publishState(ctx, topic, inverterPayload{
		TS:             inv.ReportTime.Format(time.RFC3339),
		EnvoySerial:    d.EnvoySerial,
		InverterSerial: inv.SerialNumber,
		ChannelID:      inv.Chaneid,
		Operating:      inv.Operating,
		Communicating:  inv.Communicating,
		Producing:      inv.Producing,
		Phase:          inv.Phase,
		Watts:          inv.LastReportedWatts,
		PeakWatts:      inv.MaxReportWatts,
		Status:         strings.Join(inv.DeviceStatus, ","),
		DCVoltage:      inv.DCVoltage,
		DCCurrent:      inv.DCCurrent,
		ACVoltage:      inv.ACVoltage,
		ACCurrent:      inv.ACCurrent,
		ACFrequency:    inv.ACFrequency,
		TemperatureC:   inv.TemperatureC,
		LeadingVArs:    inv.LeadingVArs,
		LaggingVArs:    inv.LaggingVArs,
		WhToday:        inv.WhToday,
		WhYesterday:    inv.WhYesterday,
		WhWeek:         inv.WhWeek,
		WhLifetime:     inv.WhLifetime,
		RSSI:           inv.RSSI,
		ISSI:           inv.ISSI,
	})
}

func (w *SolarWriter) announceEnvoy(ctx context.Context, topic string, d domain.EnvoySolarData) error {
	if d.EnvoySerial == "" || !w.announced.claim(d.EnvoySerial) {
		return nil
	}
	id := solarNodeID(d.EnvoySerial)
	sensors := []sensor{
		{
			field: "production_wh", name: "Lifetime production",
			deviceClass: classEnergy, stateClass: stateTotalIncreasing, unit: unitWh,
		},
		{field: "watt", name: "Production power", deviceClass: classPower, stateClass: stateMeasurement, unit: unitW},
		{field: "panel_count", name: "Panel count", stateClass: stateMeasurement, diagnostic: true},
	}
	if err := w.client.publishDiscovery(ctx, id, solarDevice(d.EnvoySerial), topic, sensors); err != nil {
		w.announced.release(d.EnvoySerial)
		return err
	}
	return nil
}

// announceInverter adds one power sensor per microinverter to the Envoy
// device, keyed by the inverter serial.
func (w *SolarWriter) announceInverter(
	ctx context.Context, topic string, d domain.EnvoySolarData, inv domain.InverterDetails,
) error {
	key := "inverter_" + inv.SerialNumber
	if inv.SerialNumber == "" || !w.announced.claim(key) {
		return nil
	}
	prefix := "inverter_" + sanitizeID(inv.SerialNumber) + "_"
	label := "Inverter " + inv.SerialNumber + " "
	sensors := []sensor{
		{
			id: prefix + "watts", field: "watts", name: label + "power",
			deviceClass: classPower, stateClass: stateMeasurement, unit: unitW,
		},
		{
			id: prefix + "wh_today", field: "wh_today", name: label + "energy today",
			deviceClass: classEnergy, stateClass: stateTotalIncreasing, unit: unitWh,
		},
		{
			id: prefix + "dc_voltage", field: "dc_voltage", name: label + "DC voltage",
			deviceClass: classVoltage, stateClass: stateMeasurement, unit: unitVolt, diagnostic: true,
		},
		{
			id: prefix + "dc_current", field: "dc_current", name: label + "DC current",
			deviceClass: classCurrent, stateClass: stateMeasurement, unit: unitAmpere, diagnostic: true,
		},
		{
			id: prefix + "ac_voltage", field: "ac_voltage", name: label + "AC voltage",
			deviceClass: classVoltage, stateClass: stateMeasurement, unit: unitVolt, diagnostic: true,
		},
		{
			id: prefix + "ac_frequency", field: "ac_frequency", name: label + "AC frequency",
			deviceClass: classFrequency, stateClass: stateMeasurement, unit: unitHertz, diagnostic: true,
		},
		{
			id: prefix + "temperature_c", field: "temperature_c", name: label + "temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius, diagnostic: true,
		},
		{
			id: prefix + "rssi", field: "rssi", name: label + "signal",
			deviceClass: classSignal, stateClass: stateMeasurement, diagnostic: true,
		},
	}
	if err := w.client.publishDiscovery(
		ctx, solarNodeID(d.EnvoySerial), solarDevice(d.EnvoySerial), topic, sensors,
	); err != nil {
		w.announced.release(key)
		return err
	}
	return nil
}

func solarNodeID(envoySerial string) string {
	return sanitizeID("meterlogger_solar_" + envoySerial)
}

func solarDevice(envoySerial string) deviceInfo {
	return deviceInfo{
		Identifiers:  []string{solarNodeID(envoySerial)},
		Name:         "Enphase Envoy " + envoySerial,
		Manufacturer: "Enphase",
		Model:        "Envoy",
	}
}

// Flush is a no-op; every publish goes straight to the broker.
func (w *SolarWriter) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared Client is closed at process shutdown.
func (w *SolarWriter) Close() error { return nil }
