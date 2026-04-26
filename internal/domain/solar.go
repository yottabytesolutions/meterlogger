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

// InverterDetails is a per-microinverter status row taken from the Envoy
// inventory and production endpoints.
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
