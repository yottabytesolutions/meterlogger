package multical401

import (
	"math"
	"strings"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// captureLine is a real "/#1" response captured from a Multical 401 in a
// Dutch district heating install, without the CR terminator.
const captureLine = "0507109 1296626 0032673 0006811 0003366 0003445 0000031 0000079 0000093 0000000"

// nlDefaults mirrors the documented configuration defaults.
func nlDefaults() Config {
	return Config{EnergyUnit: "GJ", EnergyDecimals: 3, VolumeDecimals: 3, PowerDecimals: 1, FlowDecimals: 1}
}

func TestParseDataLine_RealCapture(t *testing.T) {
	got, err := parseDataLine([]byte(captureLine))
	if err != nil {
		t.Fatalf("parseDataLine() error = %v", err)
	}
	want := dataFields{
		energy: 507109, volume: 1296626, hours: 32673,
		t1: 6811, t2: 3366, tdiff: 3445,
		power: 31, flow: 79, peakPower: 93, infoCode: 0,
	}
	if got != want {
		t.Errorf("parseDataLine() = %+v, want %+v", got, want)
	}
}

func TestParseDataLine_Invalid(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"nine fields", "0000001 0000002 0000003 0000004 0000005 0000006 0000007 0000008 0000009"},
		{"eleven fields", captureLine + " 0000000"},
		{"field too short", strings.Replace(captureLine, "0507109", "507109", 1)},
		{"field too long", strings.Replace(captureLine, "0507109", "00507109", 1)},
		{"non-digit character", strings.Replace(captureLine, "0507109", "05071O9", 1)},
		{"negative sign", strings.Replace(captureLine, "0507109", "-507109", 1)},
		{"double space separator", strings.Replace(captureLine, " 1296626", "  296626", 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseDataLine([]byte(tt.line)); err == nil {
				t.Errorf("parseDataLine(%q) succeeded, want error", tt.line)
			}
		})
	}
}

func TestParseDataLine_NonzeroInfoCode(t *testing.T) {
	line := strings.Replace(captureLine, " 0000000", " 0000016", 1)
	got, err := parseDataLine([]byte(line))
	if err != nil {
		t.Fatalf("parseDataLine() error = %v", err)
	}
	if got.infoCode != 16 {
		t.Errorf("infoCode = %d, want 16", got.infoCode)
	}
}

func TestBuildTelegram_RealCaptureWithNLDefaults(t *testing.T) {
	fields, err := parseDataLine([]byte(captureLine))
	if err != nil {
		t.Fatalf("parseDataLine() error = %v", err)
	}
	got, err := buildTelegram(fields, nlDefaults())
	if err != nil {
		t.Fatalf("buildTelegram() error = %v", err)
	}

	// 507.109 GJ, 1296.626 m3, 32673 h, 68.11/33.66/34.45 C, 3.1 kW,
	// 7.9 l/h, 9.3 kW peak.
	if got.Joules != 507109000000 {
		t.Errorf("Joules = %d, want 507109000000", got.Joules)
	}
	if math.Abs(got.VolumeCm3-1296.626) > 1e-9 {
		t.Errorf("VolumeCm3 = %v, want 1296.626", got.VolumeCm3)
	}
	if got.SecondsCounter != 32673*3600 {
		t.Errorf("SecondsCounter = %d, want %d", got.SecondsCounter, 32673*3600)
	}
	if math.Abs(got.Tforward-68.11) > 1e-9 {
		t.Errorf("Tforward = %v, want 68.11", got.Tforward)
	}
	if math.Abs(got.Treturn-33.66) > 1e-9 {
		t.Errorf("Treturn = %v, want 33.66", got.Treturn)
	}
	if math.Abs(got.Tdiff-34.45) > 1e-9 {
		t.Errorf("Tdiff = %v, want 34.45", got.Tdiff)
	}
	if got.ActualPower != 3100 {
		t.Errorf("ActualPower = %d, want 3100", got.ActualPower)
	}
	if got.MaxPower != 9300 {
		t.Errorf("MaxPower = %d, want 9300", got.MaxPower)
	}
	if math.Abs(got.ActualFlow-0.0079) > 1e-9 {
		t.Errorf("ActualFlow = %v, want 0.0079", got.ActualFlow)
	}
	if got.MaxFlow != 0 {
		t.Errorf("MaxFlow = %v, want 0 (not available on this interface)", got.MaxFlow)
	}
}

func TestBuildTelegram_ScalingVariants(t *testing.T) {
	tests := []struct {
		name   string
		fields dataFields
		cfg    Config
		check  func(t *testing.T, got domain.HeatTelegram)
	}{
		{
			name:   "energy GJ",
			fields: dataFields{energy: 507109},
			cfg:    Config{EnergyUnit: "GJ", EnergyDecimals: 3},
			check: func(t *testing.T, got domain.HeatTelegram) {
				t.Helper()
				if got.Joules != 507109000000 {
					t.Errorf("Joules = %d, want 507109000000", got.Joules)
				}
			},
		},
		{
			name:   "energy empty unit means GJ",
			fields: dataFields{energy: 1000},
			cfg:    Config{EnergyDecimals: 3},
			check: func(t *testing.T, got domain.HeatTelegram) {
				t.Helper()
				if got.Joules != 1000000000 {
					t.Errorf("Joules = %d, want 1000000000", got.Joules)
				}
			},
		},
		{
			name:   "energy kWh with rounding",
			fields: dataFields{energy: 1},
			cfg:    Config{EnergyUnit: "kWh", EnergyDecimals: 3},
			check: func(t *testing.T, got domain.HeatTelegram) {
				t.Helper()
				// 0.001 kWh = 3600 J exactly.
				if got.Joules != 3600 {
					t.Errorf("Joules = %d, want 3600", got.Joules)
				}
			},
		},
		{
			name:   "energy MWh",
			fields: dataFields{energy: 12345},
			cfg:    Config{EnergyUnit: "MWh", EnergyDecimals: 2},
			check: func(t *testing.T, got domain.HeatTelegram) {
				t.Helper()
				// 123.45 MWh = 444420000000 J.
				if got.Joules != 444420000000 {
					t.Errorf("Joules = %d, want 444420000000", got.Joules)
				}
			},
		},
		{
			name:   "power rounding to whole watts",
			fields: dataFields{power: 333},
			cfg:    Config{PowerDecimals: 4},
			check: func(t *testing.T, got domain.HeatTelegram) {
				t.Helper()
				// 0.0333 kW = 33.3 W, rounds to 33.
				if got.ActualPower != 33 {
					t.Errorf("ActualPower = %d, want 33", got.ActualPower)
				}
			},
		},
		{
			name:   "zero decimals leave raw values",
			fields: dataFields{volume: 42, flow: 500},
			cfg:    Config{},
			check: func(t *testing.T, got domain.HeatTelegram) {
				t.Helper()
				if got.VolumeCm3 != 42 {
					t.Errorf("VolumeCm3 = %v, want 42", got.VolumeCm3)
				}
				if math.Abs(got.ActualFlow-0.5) > 1e-9 {
					t.Errorf("ActualFlow = %v, want 0.5", got.ActualFlow)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildTelegram(tt.fields, tt.cfg)
			if err != nil {
				t.Fatalf("buildTelegram() error = %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestBuildTelegram_UnknownEnergyUnit(t *testing.T) {
	_, err := buildTelegram(dataFields{}, Config{EnergyUnit: "cal"})
	if err == nil || !strings.Contains(err.Error(), "unknown energy unit") {
		t.Fatalf("buildTelegram() error = %v, want unknown energy unit", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"defaults", nlDefaults(), false},
		{"zero value", Config{}, false},
		{"bad unit", Config{EnergyUnit: "cal"}, true},
		{"negative decimals", Config{VolumeDecimals: -1}, true},
		{"decimals too large", Config{FlowDecimals: 5}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseSerialLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    string
		wantErr bool
	}{
		{"customer number with trailing fields", "12345678901 0000000 0000001", "12345678901", false},
		{"single field", "12345678901", "12345678901", false},
		{"empty line", "", "", true},
		{"leading space", " 12345678901", "", true},
		{"non-digit", "1234S678901", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSerialLine([]byte(tt.line))
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSerialLine(%q) error = %v, wantErr %v", tt.line, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseSerialLine(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}
