package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// ducoManufacturer is the manufacturer name in the Home Assistant device
// block for the DucoBox and its nodes.
const ducoManufacturer = "Duco"

// DucoWriter implements domain.DucoRepository over MQTT. The topic layout
// mirrors the table split of the database sinks: the box status goes to
// <TopicPrefix>/<base>_box_general, RF sensor nodes to
// <TopicPrefix>/<base>_node/<node_id>, box nodes to
// <TopicPrefix>/<base>_box_node/<node_id>, and valves to
// <TopicPrefix>/<base>_valve/<node_id>.
type DucoWriter struct {
	client    *Client
	base      string
	logger    *slog.Logger
	announced *onceRegistry
	now       func() time.Time
}

// NewDucoWriter creates a ventilation writer with the given measurement base
// name.
func NewDucoWriter(client *Client, base string, logger *slog.Logger) *DucoWriter {
	return &DucoWriter{client: client, base: base, logger: logger, announced: newOnceRegistry(), now: time.Now}
}

// ducoBoxPayload is the flat JSON state message for the main unit. Field
// names match the SQL sink's box general table columns.
type ducoBoxPayload struct {
	TS                      string `json:"ts"`
	RFHomeID                string `json:"rf_home_id"`
	ExhaustFanSpeed         int    `json:"exhaust_fan_speed"`
	SupplyFanSpeed          int    `json:"supply_fan_speed"`
	ExhaustFanPwmPercentage int    `json:"exhaust_fan_pwm_percentage"`
	SupplyFanPwmPercentage  int    `json:"supply_fan_pwm_percentage"`
	BypassStatus            int    `json:"bypass_status"`
	FilterRemainingTime     int    `json:"filter_remaining_time"`
	FrostProtState          bool   `json:"frost_prot_state"`
	TempEHA                 int    `json:"temp_eha"`
	TempETA                 int    `json:"temp_eta"`
	TempODA                 int    `json:"temp_oda"`
	TempSUP                 int    `json:"temp_sup"`
	InstallerState          string `json:"installer_state"`
	WeatherStationPresent   bool   `json:"weather_station_present"`
}

// ducoNodeIdentity carries the fields shared by every node payload, matching
// the SQL sink's node identity columns.
type ducoNodeIdentity struct {
	TS             string `json:"ts"`
	NodeID         int    `json:"node_id"`
	Location       string `json:"location"`
	Device         string `json:"device"`
	ConnectionType string `json:"connection_type"`
	SerialNo       string `json:"serial_no"`
	SwVersion      string `json:"sw_version"`
	Mode           string `json:"mode"`
	State          string `json:"state"`
}

type ducoNodePayload struct {
	ducoNodeIdentity

	Co2          float64 `json:"co2"`
	Temp         float64 `json:"temp"`
	Humidity     float64 `json:"humidity"`
	RssiDirect   int     `json:"rssi_direct"`
	RssiWithHops int     `json:"rssi_with_hops"`
	HopVia       int     `json:"hop_via"`
}

type ducoBoxNodePayload struct {
	ducoNodeIdentity

	Trgt     int     `json:"trgt"`
	Actl     int     `json:"actl"`
	Co2      float64 `json:"co2"`
	Temp     float64 `json:"temp"`
	Humidity float64 `json:"humidity"`
}

type ducoValvePayload struct {
	ducoNodeIdentity

	Trgt int `json:"trgt"`
	Actl int `json:"actl"`
}

// StoreBoxStatus publishes the main unit status and announces its Home
// Assistant sensors on the first call.
func (w *DucoWriter) StoreBoxStatus(ctx context.Context, b domain.DucoBoxStatus) error {
	topic := w.client.stateTopic(w.base + "_box_general")
	if err := w.announceBox(ctx, topic, b); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing duco box status", slog.String("topic", topic))
	return w.client.publishState(ctx, topic, ducoBoxPayload{
		TS:                      w.now().Format(time.RFC3339),
		RFHomeID:                b.General.RFHomeID,
		ExhaustFanSpeed:         b.EnergyFan.ExhaustFanSpeed,
		SupplyFanSpeed:          b.EnergyFan.SupplyFanSpeed,
		ExhaustFanPwmPercentage: b.EnergyFan.ExhaustFanPwmPercentage,
		SupplyFanPwmPercentage:  b.EnergyFan.SupplyFanPwmPercentage,
		BypassStatus:            b.EnergyInfo.BypassStatus,
		FilterRemainingTime:     b.EnergyInfo.FilterRemainingTime,
		FrostProtState:          b.EnergyInfo.FrostProtState,
		TempEHA:                 b.EnergyInfo.TempEHA,
		TempETA:                 b.EnergyInfo.TempETA,
		TempODA:                 b.EnergyInfo.TempODA,
		TempSUP:                 b.EnergyInfo.TempSUP,
		InstallerState:          b.General.InstallerState,
		WeatherStationPresent:   b.WeatherStation.Present,
	})
}

// StoreNodeData publishes one node status message on the node type's topic.
// Unknown node types are logged and skipped, mirroring the database sinks.
func (w *DucoWriter) StoreNodeData(ctx context.Context, nodeData domain.DucoNodeStatus) error {
	ts := w.now().Format(time.RFC3339)
	switch d := nodeData.(type) {
	case domain.DucoRFSensorStatus:
		return w.storeNode(ctx, "_node", d.BaseDucoNodeStatus, ducoNodePayload{
			ducoNodeIdentity: nodeIdentity(ts, d.BaseDucoNodeStatus),
			Co2:              d.Co2, Temp: d.Temp, Humidity: d.Rh,
			RssiDirect: d.RssiN2M, RssiWithHops: d.RssiN2H, HopVia: d.HopVia,
		}, climateSensors())
	case domain.DucoNodeBoxStatus:
		return w.storeNode(ctx, "_box_node", d.BaseDucoNodeStatus, ducoBoxNodePayload{
			ducoNodeIdentity: nodeIdentity(ts, d.BaseDucoNodeStatus),
			Trgt:             d.Trgt, Actl: d.Actl,
			Co2: d.Co2, Temp: d.Temp, Humidity: d.Rh,
		}, append(climateSensors(), flowSensors()...))
	case domain.DucoNodeBoxValveStatus:
		return w.storeNode(ctx, "_valve", d.BaseDucoNodeStatus, ducoValvePayload{
			ducoNodeIdentity: nodeIdentity(ts, d.BaseDucoNodeStatus),
			Trgt:             d.Trgt, Actl: d.Actl,
		}, flowSensors())
	default:
		w.logger.WarnContext(ctx, "mqtt: unknown duco node type, skipping",
			slog.String("type", fmt.Sprintf("%T", nodeData)))
		return nil
	}
}

// storeNode announces the node's sensors on first sight and publishes its
// state message on <TopicPrefix>/<base><suffix>/<node_id>.
func (w *DucoWriter) storeNode(
	ctx context.Context, suffix string, base domain.BaseDucoNodeStatus, payload any, sensors []sensor,
) error {
	topic := w.client.stateTopic(w.base+suffix, strconv.Itoa(base.Node))
	if err := w.announceNode(ctx, topic, base, sensors); err != nil {
		return err
	}
	w.logger.DebugContext(ctx, "mqtt: publishing duco node status", slog.String("topic", topic))
	return w.client.publishState(ctx, topic, payload)
}

func nodeIdentity(ts string, b domain.BaseDucoNodeStatus) ducoNodeIdentity {
	return ducoNodeIdentity{
		TS:             ts,
		NodeID:         b.Node,
		Location:       b.Location,
		Device:         b.DevType,
		ConnectionType: b.Netw,
		SerialNo:       b.Serialnb,
		SwVersion:      b.Swversion,
		Mode:           b.Mode,
		State:          b.State,
	}
}

func (w *DucoWriter) announceBox(ctx context.Context, topic string, b domain.DucoBoxStatus) error {
	const key = "box"
	if !w.announced.claim(key) {
		return nil
	}
	id := sanitizeID("meterlogger_duco_box_" + b.General.RFHomeID)
	dev := deviceInfo{
		Identifiers:  []string{id},
		Name:         "DucoBox",
		Manufacturer: ducoManufacturer,
		Model:        "DucoBox Energy",
	}
	if err := w.client.publishDiscovery(ctx, id, dev, topic, ducoBoxSensors()); err != nil {
		w.announced.release(key)
		return err
	}
	return nil
}

func (w *DucoWriter) announceNode(
	ctx context.Context, topic string, b domain.BaseDucoNodeStatus, sensors []sensor,
) error {
	key := "node_" + strconv.Itoa(b.Node)
	if !w.announced.claim(key) {
		return nil
	}
	id := sanitizeID("meterlogger_duco_node_" + strconv.Itoa(b.Node) + "_" + b.Serialnb)
	name := ducoManufacturer + " " + b.DevType + " node " + strconv.Itoa(b.Node)
	if b.Location != "" {
		name = ducoManufacturer + " " + b.DevType + " " + b.Location
	}
	dev := deviceInfo{
		Identifiers:  []string{id},
		Name:         name,
		Manufacturer: ducoManufacturer,
		Model:        b.DevType,
	}
	if err := w.client.publishDiscovery(ctx, id, dev, topic, sensors); err != nil {
		w.announced.release(key)
		return err
	}
	return nil
}

func ducoBoxSensors() []sensor {
	return []sensor{
		{
			field: "temp_eha", name: "Exhaust air temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{
			field: "temp_eta", name: "Extract air temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{
			field: "temp_oda", name: "Outdoor air temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{
			field: "temp_sup", name: "Supply air temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{field: "exhaust_fan_speed", name: "Exhaust fan speed", stateClass: stateMeasurement, unit: unitRPM},
		{field: "supply_fan_speed", name: "Supply fan speed", stateClass: stateMeasurement, unit: unitRPM},
		{field: "exhaust_fan_pwm_percentage", name: "Exhaust fan PWM", stateClass: stateMeasurement, unit: unitPercent},
		{field: "supply_fan_pwm_percentage", name: "Supply fan PWM", stateClass: stateMeasurement, unit: unitPercent},
		{field: "filter_remaining_time", name: "Filter remaining time", diagnostic: true},
		{field: "bypass_status", name: "Bypass status", diagnostic: true},
	}
}

func climateSensors() []sensor {
	return []sensor{
		{field: "co2", name: "CO2", deviceClass: classCO2, stateClass: stateMeasurement, unit: unitPPM},
		{
			field: "temp", name: "Temperature",
			deviceClass: classTemperature, stateClass: stateMeasurement, unit: unitCelsius,
		},
		{
			field:       "humidity",
			name:        "Humidity",
			deviceClass: classHumidity,
			stateClass:  stateMeasurement,
			unit:        unitPercent,
		},
	}
}

func flowSensors() []sensor {
	return []sensor{
		{field: "trgt", name: "Target flow", stateClass: stateMeasurement, unit: unitPercent},
		{field: "actl", name: "Actual flow", stateClass: stateMeasurement, unit: unitPercent},
	}
}

// Flush is a no-op; every publish goes straight to the broker.
func (w *DucoWriter) Flush(_ context.Context) error { return nil }

// Close is a no-op; the shared Client is closed at process shutdown.
func (w *DucoWriter) Close() error { return nil }
