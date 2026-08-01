package domain

import (
	"context"
	"time"
)

// EN 13757-3 medium codes for the M-Bus subdevices a P1 telegram can carry.
// DeviceTypeGas lives in gas.go.
const (
	// DeviceTypeSlaveEMeter is a slave electricity meter. It is never stored
	// from the master's telegram: read the slave meter from its own P1 port.
	DeviceTypeSlaveEMeter = 2
	// DeviceTypeHeat is a heat (district heating) meter.
	DeviceTypeHeat = 4
	// DeviceTypeWaterWarm is a warm water meter.
	DeviceTypeWaterWarm = 6
	// DeviceTypeWater is a (cold) water meter.
	DeviceTypeWater = 7
	// DeviceTypeCoolingOutlet is a cooling meter measured at the outlet.
	DeviceTypeCoolingOutlet = 10
	// DeviceTypeCoolingInlet is a cooling meter measured at the inlet.
	DeviceTypeCoolingInlet = 11
	// DeviceTypeHeatCool is a combined heat and cooling meter.
	DeviceTypeHeatCool = 12
)

// WaterReading is one deduplicated water meter capture carried in a grid
// meter's P1 telegram. CapturedAt is the deduplication key, exactly as for
// GasReading.
type WaterReading struct {
	CapturedAt time.Time // meter-supplied capture time
	ReceivedAt time.Time // when the carrying telegram was read
	Channel    int
	DeviceType int // 6 warm water, 7 water
	SerialNo   string
	ReadingM3  float64
}

// ThermalReading is one deduplicated heat or cooling meter capture carried in
// a grid meter's P1 telegram. CapturedAt is the deduplication key, exactly as
// for GasReading.
type ThermalReading struct {
	CapturedAt time.Time // meter-supplied capture time
	ReceivedAt time.Time // when the carrying telegram was read
	Channel    int
	DeviceType int // 4 heat, 10/11 cooling, 12 heat/cool combined
	SerialNo   string
	ReadingGJ  float64
}

// WaterRepository stores water meter readings.
type WaterRepository interface {
	StoreWaterReading(ctx context.Context, r WaterReading) error
	Flush(ctx context.Context) error
	Close() error
}

// ThermalRepository stores heat and cooling meter readings.
type ThermalRepository interface {
	StoreThermalReading(ctx context.Context, r ThermalReading) error
	Flush(ctx context.Context) error
	Close() error
}
