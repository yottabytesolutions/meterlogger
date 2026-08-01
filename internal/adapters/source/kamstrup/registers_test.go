package kamstrup

import (
	"math"
	"testing"
)

func TestDecodeValue(t *testing.T) {
	tests := []struct {
		name     string
		siEx     byte
		mantissa []byte
		want     float64
	}{
		{"zero", 0x00, []byte{0x00}, 0},
		{"plain integer", 0x00, []byte{0x03, 0xE8}, 1000},
		{"positive exponent", 0x02, []byte{0x7B}, 12300},
		{"negative exponent", 0x42, []byte{0x00, 0x01, 0xE2, 0x40}, 1234.56},
		{"negative value", 0x80, []byte{0x64}, -100},
		{"negative value and exponent", 0xC1, []byte{0x64}, -10},
		{"four byte mantissa", 0x00, []byte{0xFF, 0xFF, 0xFF, 0xFF}, 4294967295},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeValue(tt.siEx, tt.mantissa)
			if math.Abs(got-tt.want) > 1e-9*math.Max(1, math.Abs(tt.want)) {
				t.Errorf("decodeValue(0x%02X, % X) = %v, want %v", tt.siEx, tt.mantissa, got, tt.want)
			}
		})
	}
}

func TestConvertToCanonical(t *testing.T) {
	energy := registerSpec{id: 60, name: "energy", quantity: quantityEnergy}
	tests := []struct {
		name     string
		reg      registerSpec
		unitCode byte
		raw      float64
		want     float64
		wantErr  bool
	}{
		{"GJ to J", energy, 8, 1.5, 1.5e9, false},
		{"kWh to J", energy, 2, 1, 3.6e6, false},
		{"unknown unit code", energy, 200, 1, 0, true},
		{"wrong dimension", energy, 37, 1, 0, true},
		{
			"litres per hour to m3 per hour",
			registerSpec{id: 74, quantity: quantityFlow},
			41, 850, 0.85, false,
		},
		{
			"hours to seconds",
			registerSpec{id: 1004, quantity: quantityDuration},
			46, 2, 7200, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToCanonical(tt.reg, tt.unitCode, tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("convertToCanonical() succeeded, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("convertToCanonical() error = %v", err)
			}
			if math.Abs(got-tt.want) > 1e-9*math.Max(1, math.Abs(tt.want)) {
				t.Errorf("convertToCanonical() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnitTable_QuantitiesConsistent guards the table against a unit being
// filed under the wrong dimension when a code number is corrected.
func TestUnitTable_QuantitiesConsistent(t *testing.T) {
	wantQuantity := map[string]quantity{
		"Wh": quantityEnergy, "kWh": quantityEnergy, "MWh": quantityEnergy,
		"GWh": quantityEnergy, "J": quantityEnergy, "kJ": quantityEnergy,
		"MJ": quantityEnergy, "GJ": quantityEnergy, "GJx10": quantityEnergy,
		"W": quantityPower, "kW": quantityPower, "MW": quantityPower,
		"C": quantityTemperature, "K": quantityTemperature,
		"l": quantityVolume, "m3": quantityVolume,
		"l/h": quantityFlow, "m3/h": quantityFlow,
		"h": quantityDuration, "min": quantityDuration, "s": quantityDuration,
	}
	table := unitTable()
	if len(table) != len(wantQuantity) {
		t.Errorf("unitTable() has %d entries, want %d", len(table), len(wantQuantity))
	}
	for code, unit := range table {
		want, ok := wantQuantity[unit.name]
		if !ok {
			t.Errorf("unit code %d: unexpected unit %q", code, unit.name)
			continue
		}
		if unit.quantity != want {
			t.Errorf("unit code %d (%s): quantity = %v, want %v", code, unit.name, unit.quantity, want)
		}
	}
}
