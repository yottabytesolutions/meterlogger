package domain

import (
	"context"
	"time"
)

// MBusDeviceReading is one M-Bus subdevice line set from a P1 telegram:
// a device attached to the electricity meter (gas, water, thermal) on
// channels 1 to 4. The channel is assigned by installation order, so the
// medium is identified by DeviceType, never by channel number.
type MBusDeviceReading struct {
	Channel    int
	DeviceType int // EN 13757-3 medium code: 3 gas, 4 heat, 7 water
	SerialNo   string
	CapturedAt time.Time // meter-supplied capture time
	Value      float64
	Unit       string // as printed in the telegram: m3, GJ, kWh
}

// DeviceTypeGas is the EN 13757-3 medium code for a gas meter.
const DeviceTypeGas = 3

// GasReading is one deduplicated gas meter capture. The meter updates the
// value every 5 minutes (DSMR 5) or hourly (DSMR 4 and older) while the
// telegram repeats it far more often; CapturedAt is the deduplication key.
type GasReading struct {
	CapturedAt time.Time // meter-supplied capture time
	ReceivedAt time.Time // when the carrying telegram was read
	Channel    int
	DeviceType int
	SerialNo   string
	ReadingM3  float64
}

// GasRepository stores gas meter readings.
type GasRepository interface {
	StoreGasReading(ctx context.Context, r GasReading) error
	Flush(ctx context.Context) error
	Close() error
}
