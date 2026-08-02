package domain

import (
	"context"
	"time"
)

// EnvoySolarData is one snapshot from an Enphase Envoy gateway. ProductionWh
// is the cumulative lifetime production. Watt is the current production
// power. Inverters carries per-microinverter status when reachable.
type EnvoySolarData struct {
	ReadingTime  time.Time
	ProductionWh float64
	Watt         float64
	PanelCount   int
	EnvoySerial  string
	Inverters    []InverterDetails
}

// InverterDetails is a per-microinverter row taken from the Envoy inventory,
// production, and device_data endpoints. The electrical fields (DC and AC
// voltage, current, frequency, temperature, reactive power, energy counters,
// link quality) come from /ivp/pdm/device_data and are zero when that endpoint
// has not yet reported a reading for the panel. Milli-unit values from the
// Envoy are converted to base units (V, A, Hz) at the adapter boundary.
type InverterDetails struct {
	SerialNumber      string
	Chaneid           int
	Producing         bool
	Operating         bool
	Phase             string
	Communicating     bool
	DeviceStatus      []string
	ReportTime        time.Time
	LastReportedWatts int
	MaxReportWatts    int

	// Electrical measurements from device_data lastReading.
	DCVoltage    float64
	DCCurrent    float64
	ACVoltage    float64
	ACCurrent    float64
	ACFrequency  float64
	TemperatureC int
	LeadingVArs  int
	LaggingVArs  int

	// Energy counters from device_data.
	WhToday     int
	WhYesterday int
	WhWeek      int
	WhLifetime  float64

	// Powerline link quality from device_data lastReading.
	RSSI int
	ISSI int
}

// EnvoySolarRepository writes solar gateway snapshots to a storage backend.
type EnvoySolarRepository interface {
	StoreEnvoySolarData(ctx context.Context, data EnvoySolarData) error
	Flush(ctx context.Context) error
	Close() error
}

// EnvoySolarReader reads one snapshot from an Enphase Envoy gateway.
type EnvoySolarReader interface {
	ReadEnvoySolarData(ctx context.Context) (EnvoySolarData, error)
}
