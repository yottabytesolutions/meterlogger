package multical401

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// Config sets the value scaling for one meter. The 401/66C sends bare digit
// fields; the unit and decimal position depend on the meter's CCC
// configuration code, so they cannot be derived from the telegram.
// Temperatures are always hundredths of a degree and need no configuration.
type Config struct {
	// EnergyUnit is the unit of the energy field: GJ, kWh, or MWh. Empty
	// means GJ, the common Dutch district heating configuration.
	EnergyUnit string
	// The decimal counts divide the raw 7-digit field: a raw energy field
	// with EnergyDecimals 3 is raw/1000 in EnergyUnit. Volume is in m3,
	// power in kW, flow in l/h before their decimals apply.
	EnergyDecimals int
	VolumeDecimals int
	PowerDecimals  int
	FlowDecimals   int
}

// validate rejects settings the parser cannot apply. The config package
// performs the same checks at startup; this guards direct library use.
func (c Config) validate() error {
	if _, err := c.energyJoulesFactor(); err != nil {
		return err
	}
	const maxDecimals = 4
	for _, d := range []int{c.EnergyDecimals, c.VolumeDecimals, c.PowerDecimals, c.FlowDecimals} {
		if d < 0 || d > maxDecimals {
			return fmt.Errorf("decimal count %d out of range 0 to %d", d, maxDecimals)
		}
	}
	return nil
}

// energyJoulesFactor returns the factor from one EnergyUnit to joules.
func (c Config) energyJoulesFactor() (float64, error) {
	const (
		joulesPerGJ  = 1e9
		joulesPerKWh = 3.6e6
		joulesPerMWh = 3.6e9
	)
	switch c.EnergyUnit {
	case "", "GJ":
		return joulesPerGJ, nil
	case "kWh":
		return joulesPerKWh, nil
	case "MWh":
		return joulesPerMWh, nil
	}
	return 0, fmt.Errorf("unknown energy unit %q; valid values are GJ, kWh, MWh", c.EnergyUnit)
}

// dataFields is one decoded "/#1" telegram, still in raw meter integers.
type dataFields struct {
	energy    int64 // in EnergyUnit, scaled by EnergyDecimals
	volume    int64 // in m3, scaled by VolumeDecimals
	hours     int64 // operating hours, unscaled
	t1        int64 // forward temperature, hundredths of a degree C
	t2        int64 // return temperature, hundredths of a degree C
	tdiff     int64 // t1-t2, hundredths of a degree C
	power     int64 // current power in kW, scaled by PowerDecimals
	flow      int64 // current flow in l/h, scaled by FlowDecimals
	peakPower int64 // peak power this year in kW, scaled by PowerDecimals
	infoCode  int64 // nonzero signals a meter-reported problem
}

const (
	fieldCount = 10
	fieldWidth = 7
)

// parseDataLine decodes a "/#1" response: exactly ten fields of exactly
// seven decimal digits, separated by single spaces. Anything else is an
// error; the caller retries the exchange.
func parseDataLine(line []byte) (dataFields, error) {
	parts := strings.Split(string(line), " ")
	if len(parts) != fieldCount {
		return dataFields{}, fmt.Errorf("telegram has %d fields, want %d", len(parts), fieldCount)
	}
	values := make([]int64, fieldCount)
	for i, part := range parts {
		v, err := parseField(part)
		if err != nil {
			return dataFields{}, fmt.Errorf("field %d: %w", i+1, err)
		}
		values[i] = v
	}
	return dataFields{
		energy:    values[0],
		volume:    values[1],
		hours:     values[2],
		t1:        values[3],
		t2:        values[4],
		tdiff:     values[5],
		power:     values[6],
		flow:      values[7],
		peakPower: values[8],
		infoCode:  values[9],
	}, nil
}

// parseField decodes one 7-digit field. Width and character checks are
// explicit: strconv alone would accept signs and shorter strings.
func parseField(s string) (int64, error) {
	if len(s) != fieldWidth {
		return 0, fmt.Errorf("field %q is %d bytes, want %d", s, len(s), fieldWidth)
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("field %q contains a non-digit", s)
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse field %q: %w", s, err)
	}
	return v, nil
}

// parseSerialLine decodes a "/#2" response. Its first space-separated field
// is the customer number; the remaining fields vary by model and are
// ignored. The field must be plain digits.
func parseSerialLine(line []byte) (string, error) {
	first, _, _ := strings.Cut(string(line), " ")
	if first == "" {
		return "", errors.New("empty serial number response")
	}
	for _, c := range first {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("serial number field %q contains a non-digit", first)
		}
	}
	return first, nil
}

// buildTelegram converts raw meter integers to the domain's canonical units.
// MaxFlow stays 0: the 401/66C optical telegram does not carry a flow
// maximum. Identity and timestamp are filled by the caller.
func buildTelegram(f dataFields, cfg Config) (domain.HeatTelegram, error) {
	joulesPerUnit, err := cfg.energyJoulesFactor()
	if err != nil {
		return domain.HeatTelegram{}, err
	}
	const (
		centi          = 100  // temperatures are hundredths of a degree C
		wattsPerKW     = 1000 // power fields are in kW
		m3PerLitre     = 1e-3 // flow fields are in l/h
		secondsPerHour = 3600
	)
	energy := scale(f.energy, cfg.EnergyDecimals)
	power := scale(f.power, cfg.PowerDecimals)
	peakPower := scale(f.peakPower, cfg.PowerDecimals)
	return domain.HeatTelegram{
		Joules:         int64(math.Round(energy * joulesPerUnit)),
		VolumeCm3:      scale(f.volume, cfg.VolumeDecimals), // cubic metres; field name is historical
		SecondsCounter: f.hours * secondsPerHour,
		Tforward:       float64(f.t1) / centi,
		Treturn:        float64(f.t2) / centi,
		Tdiff:          float64(f.tdiff) / centi,
		ActualPower:    int64(math.Round(power * wattsPerKW)),
		MaxPower:       int64(math.Round(peakPower * wattsPerKW)),
		ActualFlow:     scale(f.flow, cfg.FlowDecimals) * m3PerLitre,
		MaxFlow:        0,
	}, nil
}

// scale divides a raw field by 10^decimals.
func scale(raw int64, decimals int) float64 {
	return float64(raw) / math.Pow10(decimals)
}
