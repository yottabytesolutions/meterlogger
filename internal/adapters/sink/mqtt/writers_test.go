package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	tsNoon        = "2026-07-01T12:00:00Z"
	heatTopic     = "meterlogger/heat"
	envoySerialNo = "ENV1"
	gridModel     = "ISKRA"
	fieldSerialNo = "serial_no"
)

// goldenDevice builds the expected Home Assistant device block. Empty
// manufacturer or model entries are omitted, matching the omitempty tags.
func goldenDevice(id, name, manufacturer, model string) map[string]any {
	m := map[string]any{
		"identifiers": []any{id},
		"name":        name,
	}
	if manufacturer != "" {
		m["manufacturer"] = manufacturer
	}
	if model != "" {
		m["model"] = model
	}
	return m
}

// goldenDiscovery builds the expected discovery config message for one
// sensor, mirroring what publishDiscovery emits.
func goldenDiscovery(
	uniqueID, name, stateTopic, field string, device map[string]any, class, stateClass, unit string,
) map[string]any {
	m := map[string]any{
		"unique_id":             uniqueID,
		"name":                  name,
		"state_topic":           stateTopic,
		"value_template":        "{{ value_json." + field + " }}",
		"availability":          []any{map[string]any{"topic": testStatusTopic}},
		"payload_available":     payloadOnline,
		"payload_not_available": payloadOffline,
		"device":                device,
	}
	if class != "" {
		m["device_class"] = class
	}
	if stateClass != "" {
		m["state_class"] = stateClass
	}
	if unit != "" {
		m["unit_of_measurement"] = unit
	}
	return m
}

func decodePayload(t *testing.T, p publication) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(p.payload, &m); err != nil {
		t.Fatalf("unmarshal payload on %s: %v", p.topic, err)
	}
	return m
}

func heatTelegram() domain.HeatTelegram {
	return domain.HeatTelegram{
		Timestamp:      time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		MeterID:        "403",
		SerialNo:       "SER123",
		Joules:         36e8, // 1000 kWh, 3.6 GJ
		VolumeCm3:      2_500_000,
		SecondsCounter: 7200,
		Tforward:       70.5,
		Treturn:        40.25,
		Tdiff:          30.25,
		ActualPower:    5000, // watts
		MaxPower:       9000,
		ActualFlow:     0.5,
		MaxFlow:        1.5,
	}
}

func TestHeatWriter_PublishesStateAndDiscoveryOnce(t *testing.T) {
	fp := newFakePaho()
	c := newTestClient(fp, testConfig())
	w := NewHeatWriter(c, "heat", testLogger())
	ctx := context.Background()

	if err := w.StoreHeatTelegram(ctx, heatTelegram()); err != nil {
		t.Fatalf("StoreHeatTelegram: %v", err)
	}
	if err := w.StoreHeatTelegram(ctx, heatTelegram()); err != nil {
		t.Fatalf("StoreHeatTelegram second call: %v", err)
	}

	states := fp.byTopic(heatTopic)
	if len(states) != 2 {
		t.Fatalf("state publications = %d, want 2", len(states))
	}
	if states[0].qos != 1 || states[0].retained {
		t.Errorf("state qos/retain = %d/%v, want 1/false", states[0].qos, states[0].retained)
	}
	got := decodePayload(t, states[0])
	want := map[string]any{
		"ts": tsNoon, "meter_id": "403", fieldSerialNo: "SER123",
		"power_w": 5000.0, "energy_gj": 3.6, "energy_kwh": 1000.0,
		"t_forward_c": 70.5, "t_return_c": 40.25, "t_diff_c": 30.25,
		"volume_cm3": 2500000.0, "volume_m3": 2.5, "seconds": 7200.0,
		"max_flow": 1.5, "max_power_w": 9000.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("state payload = %v, want %v", got, want)
	}

	// Discovery: published once, retained, one config per heat sensor.
	cfgTopic := "homeassistant/sensor/meterlogger_heat_SER123/energy_kwh/config"
	configs := fp.byTopic(cfgTopic)
	if len(configs) != 1 {
		t.Fatalf("discovery configs on %s = %d, want 1", cfgTopic, len(configs))
	}
	if !configs[0].retained {
		t.Error("discovery config must be retained")
	}
	gotCfg := decodePayload(t, configs[0])
	wantCfg := goldenDiscovery(
		"meterlogger_heat_SER123_energy_kwh", "Energy", heatTopic, "energy_kwh",
		goldenDevice("meterlogger_heat_SER123", "Heat meter SER123", "Kamstrup", "Multical 403"),
		classEnergy, stateTotalIncreasing, unitKWh,
	)
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Errorf("discovery config = %v, want %v", gotCfg, wantCfg)
	}
}

func TestHeatWriter_DiscoveryDisabled(t *testing.T) {
	fp := newFakePaho()
	cfg := testConfig()
	cfg.HomeAssistantDiscovery = false
	w := NewHeatWriter(newTestClient(fp, cfg), "heat", testLogger())

	if err := w.StoreHeatTelegram(context.Background(), heatTelegram()); err != nil {
		t.Fatalf("StoreHeatTelegram: %v", err)
	}
	for _, p := range fp.published {
		if p.topic != heatTopic {
			t.Errorf("unexpected publication on %s", p.topic)
		}
	}
}

func TestHeatWriter_DiscoveryErrorRetries(t *testing.T) {
	fp := newFakePaho()
	fp.publishErr = errors.New("broker gone")
	fp.publishErrOn = "homeassistant/"
	w := NewHeatWriter(newTestClient(fp, testConfig()), "heat", testLogger())
	ctx := context.Background()

	if err := w.StoreHeatTelegram(ctx, heatTelegram()); err == nil {
		t.Fatal("StoreHeatTelegram should return the discovery publish error")
	}
	fp.publishErr = nil
	if err := w.StoreHeatTelegram(ctx, heatTelegram()); err != nil {
		t.Fatalf("StoreHeatTelegram after recovery: %v", err)
	}
	cfgTopic := "homeassistant/sensor/meterlogger_heat_SER123/energy_kwh/config"
	// One failed attempt plus one successful retry.
	if got := len(fp.byTopic(cfgTopic)); got != 2 {
		t.Errorf("discovery attempts = %d, want 2", got)
	}
}

func TestHeatWriter_PublishErrorPropagates(t *testing.T) {
	fp := newFakePaho()
	fp.publishErr = errors.New("broker gone")
	fp.publishErrOn = heatTopic
	w := NewHeatWriter(newTestClient(fp, testConfig()), "heat", testLogger())

	if err := w.StoreHeatTelegram(context.Background(), heatTelegram()); err == nil {
		t.Error("StoreHeatTelegram should return the state publish error")
	}
}

func TestHeatWriter_FlushAndCloseAreNoops(t *testing.T) {
	fp := newFakePaho()
	w := NewHeatWriter(newTestClient(fp, testConfig()), "heat", testLogger())
	if err := w.Flush(context.Background()); err != nil {
		t.Errorf("Flush = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
	if fp.disconnected {
		t.Error("writer Close must not disconnect the shared client")
	}
}

func TestGridWriter_PublishesStateAndDiscovery(t *testing.T) {
	fp := newFakePaho()
	w := NewGridWriter(newTestClient(fp, testConfig()), "grid", testLogger())
	tel := domain.GridTelegram{
		Time:            time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		MeterMerkType:   gridModel,
		Serienummer:     "E123",
		UsageCounter1:   239.922,
		TotalPowerUsage: 577,
		VoltageP1:       227.4,
		CurrentP1:       2,
		PowerUsageP1:    298,
		BrownoutsP1:     3,
	}

	if err := w.StoreGridTelegram(context.Background(), tel); err != nil {
		t.Fatalf("StoreGridTelegram: %v", err)
	}

	states := fp.byTopic("meterlogger/grid")
	if len(states) != 1 {
		t.Fatalf("state publications = %d, want 1", len(states))
	}
	got := decodePayload(t, states[0])
	for key, want := range map[string]any{
		"ts": tsNoon, "meter_type": gridModel, fieldSerialNo: "E123",
		"usage_counter1": 239.922, "total_power_usage": 577.0,
		"voltage_p1": 227.4, "current_p1": 2.0, "power_usage_p1": 298.0, "brownouts_p1": 3.0,
	} {
		if got[key] != want {
			t.Errorf("payload[%q] = %v, want %v", key, got[key], want)
		}
	}

	// Golden discovery config for the tariff 1 energy sensor.
	configs := fp.byTopic("homeassistant/sensor/meterlogger_grid_E123/usage_counter1/config")
	if len(configs) != 1 {
		t.Fatalf("discovery configs = %d, want 1", len(configs))
	}
	gotCfg := decodePayload(t, configs[0])
	wantCfg := goldenDiscovery(
		"meterlogger_grid_E123_usage_counter1", "Energy consumed tariff 1", "meterlogger/grid", "usage_counter1",
		goldenDevice("meterlogger_grid_E123", "Grid meter E123", "", gridModel),
		classEnergy, stateTotalIncreasing, unitKWh,
	)
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Errorf("discovery config = %v, want %v", gotCfg, wantCfg)
	}

	// Diagnostics carry the entity_category marker.
	diag := fp.byTopic("homeassistant/sensor/meterlogger_grid_E123/brownouts_p1/config")
	if len(diag) != 1 {
		t.Fatalf("brownouts discovery configs = %d, want 1", len(diag))
	}
	if got := decodePayload(t, diag[0])["entity_category"]; got != "diagnostic" {
		t.Errorf("brownouts entity_category = %v, want diagnostic", got)
	}
}

func TestGasWriter_PublishesStateAndDiscovery(t *testing.T) {
	fp := newFakePaho()
	w := NewGasWriter(newTestClient(fp, testConfig()), "gas_meter", testLogger())
	r := domain.GasReading{
		CapturedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		ReceivedAt: time.Date(2026, 7, 1, 12, 0, 30, 0, time.UTC),
		Channel:    1,
		DeviceType: domain.DeviceTypeGas,
		SerialNo:   "G456",
		ReadingM3:  1234.567,
	}

	if err := w.StoreGasReading(context.Background(), r); err != nil {
		t.Fatalf("StoreGasReading: %v", err)
	}

	states := fp.byTopic("meterlogger/gas_meter")
	if len(states) != 1 {
		t.Fatalf("state publications = %d, want 1", len(states))
	}
	got := decodePayload(t, states[0])
	want := map[string]any{
		"ts": tsNoon, "received_at": "2026-07-01T12:00:30Z",
		"channel": 1.0, "device_type": 3.0, "serial_no": "G456", "reading_m3": 1234.567,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("state payload = %v, want %v", got, want)
	}

	configs := fp.byTopic("homeassistant/sensor/meterlogger_gas_G456/reading_m3/config")
	if len(configs) != 1 {
		t.Fatalf("discovery configs = %d, want 1", len(configs))
	}
	gotCfg := decodePayload(t, configs[0])
	wantCfg := goldenDiscovery(
		"meterlogger_gas_G456_reading_m3", "Gas consumption", "meterlogger/gas_meter", "reading_m3",
		goldenDevice("meterlogger_gas_G456", "Gas meter G456", "", ""),
		classGas, stateTotalIncreasing, unitM3,
	)
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Errorf("discovery config = %v, want %v", gotCfg, wantCfg)
	}
}

func TestSolarWriter_PublishesSnapshotInvertersAndDiscovery(t *testing.T) {
	fp := newFakePaho()
	w := NewSolarWriter(newTestClient(fp, testConfig()), "solar", testLogger())
	data := domain.EnvoySolarData{
		ReadingTime:  time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		ProductionWh: 1_234_567,
		Watt:         2500,
		PanelCount:   10,
		EnvoySerial:  envoySerialNo,
		Inverters: []domain.InverterDetails{{
			SerialNumber:      "INV1",
			Chaneid:           7,
			Producing:         true,
			Operating:         true,
			Communicating:     true,
			Phase:             "ph-a",
			ReportTime:        time.Date(2026, 7, 1, 11, 59, 0, 0, time.UTC),
			LastReportedWatts: 250,
			MaxReportWatts:    300,
		}},
	}

	if err := w.StoreEnvoySolarData(context.Background(), data); err != nil {
		t.Fatalf("StoreEnvoySolarData: %v", err)
	}

	states := fp.byTopic("meterlogger/solar")
	if len(states) != 1 {
		t.Fatalf("snapshot publications = %d, want 1", len(states))
	}
	got := decodePayload(t, states[0])
	want := map[string]any{
		"ts": tsNoon, "envoy_serial": envoySerialNo,
		"production_wh": 1234567.0, "watt": 2500.0, "panel_count": 10.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot payload = %v, want %v", got, want)
	}

	invStates := fp.byTopic("meterlogger/solar_inverters/INV1")
	if len(invStates) != 1 {
		t.Fatalf("inverter publications = %d, want 1", len(invStates))
	}
	invGot := decodePayload(t, invStates[0])
	invWant := map[string]any{
		"ts": "2026-07-01T11:59:00Z", "envoy_serial": envoySerialNo, "inverter_serial": "INV1",
		"channel_id": 7.0, "operating": true, "communicating": true, "producing": true,
		"phase": "ph-a", "watts": 250.0, "peak_watts": 300.0,
	}
	if !reflect.DeepEqual(invGot, invWant) {
		t.Errorf("inverter payload = %v, want %v", invGot, invWant)
	}

	// Golden discovery config for the lifetime production sensor.
	configs := fp.byTopic("homeassistant/sensor/meterlogger_solar_ENV1/production_wh/config")
	if len(configs) != 1 {
		t.Fatalf("discovery configs = %d, want 1", len(configs))
	}
	gotCfg := decodePayload(t, configs[0])
	wantCfg := goldenDiscovery(
		"meterlogger_solar_ENV1_production_wh", "Lifetime production", "meterlogger/solar", "production_wh",
		goldenDevice("meterlogger_solar_ENV1", "Enphase Envoy "+envoySerialNo, "Enphase", "Envoy"),
		classEnergy, stateTotalIncreasing, unitWh,
	)
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Errorf("discovery config = %v, want %v", gotCfg, wantCfg)
	}

	// The per-inverter power sensor hangs off the envoy device.
	invCfg := fp.byTopic("homeassistant/sensor/meterlogger_solar_ENV1/inverter_INV1_watts/config")
	if len(invCfg) != 1 {
		t.Fatalf("inverter discovery configs = %d, want 1", len(invCfg))
	}
	if got := decodePayload(t, invCfg[0])["value_template"]; got != "{{ value_json.watts }}" {
		t.Errorf("inverter value_template = %v", got)
	}
}

func ducoBase() domain.BaseDucoNodeStatus {
	return domain.BaseDucoNodeStatus{
		Node: 2, DevType: "VLVRH", Netw: "wired", Location: "Bathroom",
		Serialnb: "DUCO2", Swversion: "1.2", Mode: "AUTO", State: "auto",
	}
}

func TestDucoWriter_BoxAndNodes(t *testing.T) {
	fp := newFakePaho()
	w := NewDucoWriter(newTestClient(fp, testConfig()), "duco", testLogger())
	w.now = func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	box := domain.DucoBoxStatus{
		General:    domain.General{RFHomeID: "RF1", InstallerState: "ok"},
		EnergyFan:  domain.EnergyFan{ExhaustFanSpeed: 1200, SupplyFanSpeed: 1100},
		EnergyInfo: domain.EnergyInfo{TempODA: 15, TempSUP: 18, FilterRemainingTime: 120},
	}
	if err := w.StoreBoxStatus(ctx, box); err != nil {
		t.Fatalf("StoreBoxStatus: %v", err)
	}
	states := fp.byTopic("meterlogger/duco_box_general")
	if len(states) != 1 {
		t.Fatalf("box publications = %d, want 1", len(states))
	}
	got := decodePayload(t, states[0])
	for key, want := range map[string]any{
		"ts": tsNoon, "rf_home_id": "RF1",
		"exhaust_fan_speed": 1200.0, "supply_fan_speed": 1100.0,
		"temp_oda": 15.0, "temp_sup": 18.0, "filter_remaining_time": 120.0,
	} {
		if got[key] != want {
			t.Errorf("box payload[%q] = %v, want %v", key, got[key], want)
		}
	}

	// Golden discovery config for the outdoor temperature sensor.
	configs := fp.byTopic("homeassistant/sensor/meterlogger_duco_box_RF1/temp_oda/config")
	if len(configs) != 1 {
		t.Fatalf("box discovery configs = %d, want 1", len(configs))
	}
	gotCfg := decodePayload(t, configs[0])
	wantCfg := goldenDiscovery(
		"meterlogger_duco_box_RF1_temp_oda", "Outdoor air temperature",
		"meterlogger/duco_box_general", "temp_oda",
		goldenDevice("meterlogger_duco_box_RF1", "DucoBox", ducoManufacturer, "DucoBox Energy"),
		classTemperature, stateMeasurement, unitCelsius,
	)
	if !reflect.DeepEqual(gotCfg, wantCfg) {
		t.Errorf("box discovery config = %v, want %v", gotCfg, wantCfg)
	}
}

func TestDucoWriter_Nodes(t *testing.T) {
	fp := newFakePaho()
	w := NewDucoWriter(newTestClient(fp, testConfig()), "duco", testLogger())
	w.now = func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	// RF sensor node.
	rf := domain.DucoRFSensorStatus{BaseDucoNodeStatus: ducoBase(), Temp: 21.5, Co2: 600, Rh: 55}
	rf.Node = 3
	rf.DevType = "SENSO"
	if err := w.StoreNodeData(ctx, rf); err != nil {
		t.Fatalf("StoreNodeData rf: %v", err)
	}
	rfStates := fp.byTopic("meterlogger/duco_node/3")
	if len(rfStates) != 1 {
		t.Fatalf("rf node publications = %d, want 1", len(rfStates))
	}
	rfGot := decodePayload(t, rfStates[0])
	for key, want := range map[string]any{
		"node_id": 3.0, "device": "SENSO", "co2": 600.0, "temp": 21.5, "humidity": 55.0,
	} {
		if rfGot[key] != want {
			t.Errorf("rf payload[%q] = %v, want %v", key, rfGot[key], want)
		}
	}
	if got := len(fp.byTopic("homeassistant/sensor/meterlogger_duco_node_3_DUCO2/co2/config")); got != 1 {
		t.Errorf("rf co2 discovery configs = %d, want 1", got)
	}

	// Box node and valve node land on their own topics.
	boxNode := domain.DucoNodeBoxStatus{BaseDucoNodeStatus: ducoBase(), Trgt: 30, Actl: 20, Temp: 20, Co2: 450, Rh: 40}
	boxNode.Node = 1
	if err := w.StoreNodeData(ctx, boxNode); err != nil {
		t.Fatalf("StoreNodeData box node: %v", err)
	}
	if got := len(fp.byTopic("meterlogger/duco_box_node/1")); got != 1 {
		t.Errorf("box node publications = %d, want 1", got)
	}

	valve := domain.DucoNodeBoxValveStatus{BaseDucoNodeStatus: ducoBase(), Trgt: 50, Actl: 45}
	if err := w.StoreNodeData(ctx, valve); err != nil {
		t.Fatalf("StoreNodeData valve: %v", err)
	}
	valveStates := fp.byTopic("meterlogger/duco_valve/2")
	if len(valveStates) != 1 {
		t.Fatalf("valve publications = %d, want 1", len(valveStates))
	}
	valveGot := decodePayload(t, valveStates[0])
	if valveGot["trgt"] != 50.0 || valveGot["actl"] != 45.0 || valveGot[fieldSerialNo] != "DUCO2" {
		t.Errorf("valve payload = %v", valveGot)
	}
}

func TestDucoWriter_UnknownNodeTypeIsSkipped(t *testing.T) {
	fp := newFakePaho()
	w := NewDucoWriter(newTestClient(fp, testConfig()), "duco", testLogger())

	if err := w.StoreNodeData(context.Background(), unknownNode{}); err != nil {
		t.Fatalf("StoreNodeData unknown = %v, want nil", err)
	}
	if len(fp.published) != 0 {
		t.Errorf("unknown node type must not publish, got %d messages", len(fp.published))
	}
}

type unknownNode struct{}

func (unknownNode) NodeDevType() string { return "???" }
