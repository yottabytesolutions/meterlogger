package domain

import (
	"context"
	"time"
)

// HeatTelegram is one decoded reading from an M-Bus heat meter.
// Joules is cumulative energy, ActualPower is in milliwatts, and
// VolumeCm3 is cumulative volume in cubic centimetres. Temperatures
// are in degrees Celsius and flow is in litres per hour.
type HeatTelegram struct {
	Timestamp      time.Time
	MeterID        string
	SerialNo       string
	Joules         int64
	VolumeCm3      float64
	SecondsCounter int64
	Tforward       float64
	Treturn        float64
	Tdiff          float64
	ActualPower    int64
	MaxPower       int64
	ActualFlow     float64
	MaxFlow        float64
}

// HeatMeterReader reads one telegram from a heat meter. Implementations
// must be safe to call repeatedly. Returning a non-nil error means the
// caller should treat the read as failed; partial data is never returned.
type HeatMeterReader interface {
	ReadHeatTelegram(ctx context.Context) (HeatTelegram, error)
}

// HeatMeterRepository writes heat meter telegrams to a storage backend.
type HeatMeterRepository interface {
	StoreHeatTelegram(ctx context.Context, telegram HeatTelegram) error
	Flush(ctx context.Context) error
	Close() error
}
