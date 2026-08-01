package kamstrup

import (
	"fmt"
	"math"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// quantity is the physical dimension a register value carries. Each quantity
// has one canonical unit the sink schema expects; unitTable converts into it.
type quantity int

const (
	quantityEnergy      quantity = iota // canonical: joules
	quantityVolume                      // canonical: cubic metres
	quantityTemperature                 // canonical: degrees Celsius
	quantityFlow                        // canonical: cubic metres per hour
	quantityPower                       // canonical: watts
	quantityDuration                    // canonical: seconds
)

// unitSpec describes one KMP unit code: its name, dimension, and the factor
// that converts a value in this unit to the quantity's canonical unit.
type unitSpec struct {
	name     string
	quantity quantity
	factor   float64
}

// unitTable maps KMP unit codes to conversions. The code numbers follow the
// units table in Poul-Henning Kamp's kamstrup.py. One entry per line so a
// wrong code number is a one-line fix. Code 21 is listed as kW there, but the
// surrounding sequences (1=Wh, 33=V) make W the consistent reading; confirm
// against a physical meter. Kelvin maps to Celsius with factor 1 because the
// only register that reports it (t1-t2) is a temperature difference.
func unitTable() map[byte]unitSpec {
	return map[byte]unitSpec{
		1:  {"Wh", quantityEnergy, 3600},
		2:  {"kWh", quantityEnergy, 3.6e6},
		3:  {"MWh", quantityEnergy, 3.6e9},
		4:  {"GWh", quantityEnergy, 3.6e12},
		5:  {"J", quantityEnergy, 1},
		6:  {"kJ", quantityEnergy, 1e3},
		7:  {"MJ", quantityEnergy, 1e6},
		8:  {"GJ", quantityEnergy, 1e9},
		21: {"W", quantityPower, 1},
		22: {"kW", quantityPower, 1e3},
		23: {"MW", quantityPower, 1e6},
		37: {"C", quantityTemperature, 1},
		38: {"K", quantityTemperature, 1},
		39: {"l", quantityVolume, 1e-3},
		40: {"m3", quantityVolume, 1},
		41: {"l/h", quantityFlow, 1e-3},
		42: {"m3/h", quantityFlow, 1},
		46: {"h", quantityDuration, 3600},
		57: {"GJx10", quantityEnergy, 1e10},
		58: {"min", quantityDuration, 60},
		60: {"s", quantityDuration, 1},
	}
}

// registerSpec ties one KMP register ID to the HeatTelegram field it fills.
type registerSpec struct {
	id       uint16
	name     string
	quantity quantity
	assign   func(t *domain.HeatTelegram, v float64)
}

// heatRegisters lists the registers read per telegram, using the common KMP
// heat meter register map (Kamstrup doc 5512-447). IDs are decimal with hex
// comments: note that decimal 80 (current power, 0x0050) is not hex 0x80
// (decimal 128, max power). VolumeCm3 holds cubic metres; the field name is
// historical and matches what the M-Bus path stores there. The maxima are
// this-year values, counted since the meter's target date.
func heatRegisters() []registerSpec {
	setJoules := func(t *domain.HeatTelegram, v float64) { t.Joules = int64(math.Round(v)) }
	setVolume := func(t *domain.HeatTelegram, v float64) { t.VolumeCm3 = v }
	setTforward := func(t *domain.HeatTelegram, v float64) { t.Tforward = v }
	setTreturn := func(t *domain.HeatTelegram, v float64) { t.Treturn = v }
	setTdiff := func(t *domain.HeatTelegram, v float64) { t.Tdiff = v }
	setFlow := func(t *domain.HeatTelegram, v float64) { t.ActualFlow = v }
	setPower := func(t *domain.HeatTelegram, v float64) { t.ActualPower = int64(math.Round(v)) }
	setMaxFlow := func(t *domain.HeatTelegram, v float64) { t.MaxFlow = v }
	setMaxPower := func(t *domain.HeatTelegram, v float64) { t.MaxPower = int64(math.Round(v)) }
	setSeconds := func(t *domain.HeatTelegram, v float64) { t.SecondsCounter = int64(math.Round(v)) }
	return []registerSpec{
		{60, "heat energy E1", quantityEnergy, setJoules},                // 0x003C
		{68, "volume V1", quantityVolume, setVolume},                     // 0x0044
		{86, "t1 forward temperature", quantityTemperature, setTforward}, // 0x0056
		{87, "t2 return temperature", quantityTemperature, setTreturn},   // 0x0057
		{89, "t1-t2 differential", quantityTemperature, setTdiff},        // 0x0059
		{74, "current flow", quantityFlow, setFlow},                      // 0x004A
		{80, "current power", quantityPower, setPower},                   // 0x0050
		{124, "max flow this year", quantityFlow, setMaxFlow},            // 0x007C
		{128, "max power this year", quantityPower, setMaxPower},         // 0x0080
		{1004, "hour counter", quantityDuration, setSeconds},             // 0x03EC
	}
}

// decodeValue converts a register's mantissa and exponent byte to a float.
// The mantissa is an unsigned big-endian integer. In siEx, the low 6 bits are
// the exponent magnitude, bit 0x40 makes the exponent negative, and bit 0x80
// makes the value negative.
func decodeValue(siEx byte, mantissa []byte) float64 {
	const (
		byteBase        = 256
		exponentMask    = 0x3F
		exponentSignBit = 0x40
		valueSignBit    = 0x80
		decimalExpBase  = 10
	)
	var m float64
	for _, b := range mantissa {
		m = m*byteBase + float64(b)
	}
	exp := float64(siEx & exponentMask)
	if siEx&exponentSignBit != 0 {
		exp = -exp
	}
	v := m * math.Pow(decimalExpBase, exp)
	if siEx&valueSignBit != 0 {
		v = -v
	}
	return v
}

// convertToCanonical converts a raw register value with a KMP unit code to
// the canonical unit of the expected quantity. An unknown unit code or a code
// of the wrong dimension is an error: silently misinterpreting a unit would
// corrupt the stored series.
func convertToCanonical(reg registerSpec, unitCode byte, raw float64) (float64, error) {
	unit, ok := unitTable()[unitCode]
	if !ok {
		return 0, fmt.Errorf("unknown unit code %d", unitCode)
	}
	if unit.quantity != reg.quantity {
		return 0, fmt.Errorf("unit %s does not fit register dimension", unit.name)
	}
	return raw * unit.factor, nil
}
