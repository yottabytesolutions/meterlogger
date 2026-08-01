package gridmeter

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// fluviusTelegram is a real Belgian Fluvius eMUCS v1.7.1 telegram with a
// valid CRC (3AD7): version on 0-0:96.1.4, peak demand fields, gas on the
// Belgian 0-1:24.2.3 code and a water meter (device type 007) on 0-2:24.2.1.
//
//nolint:gochecknoglobals // test data shared across test functions
var fluviusTelegram = "/FLU5\\253769484_A\r\n" +
	"\r\n" +
	"0-0:96.1.4(50217)\r\n" +
	"0-0:96.1.1(3153414733313031303231363035)\r\n" +
	"0-0:1.0.0(200512135409S)\r\n" +
	"1-0:1.8.1(000000.034*kWh)\r\n" +
	"1-0:1.8.2(000015.758*kWh)\r\n" +
	"1-0:2.8.1(000000.000*kWh)\r\n" +
	"1-0:2.8.2(000000.011*kWh)\r\n" +
	"1-0:1.4.0(02.351*kW)\r\n" +
	"1-0:1.6.0(200509134558S)(02.589*kW)\r\n" +
	"0-0:98.1.0(3)(1-0:1.6.0)(1-0:1.6.0)(200501000000S)(200423192538S)(03.695*kW)" +
	"(200401000000S)(200305122139S)(05.980*kW)(200301000000S)(200210035421W)(04.318*kW)\r\n" +
	"0-0:96.14.0(0001)\r\n" +
	"1-0:1.7.0(00.000*kW)\r\n" +
	"1-0:2.7.0(00.000*kW)\r\n" +
	"1-0:21.7.0(00.000*kW)\r\n" +
	"1-0:41.7.0(00.000*kW)\r\n" +
	"1-0:61.7.0(00.000*kW)\r\n" +
	"1-0:22.7.0(00.000*kW)\r\n" +
	"1-0:42.7.0(00.000*kW)\r\n" +
	"1-0:62.7.0(00.000*kW)\r\n" +
	"1-0:32.7.0(234.7*V)\r\n" +
	"1-0:52.7.0(234.7*V)\r\n" +
	"1-0:72.7.0(234.7*V)\r\n" +
	"1-0:31.7.0(000.00*A)\r\n" +
	"1-0:51.7.0(000.00*A)\r\n" +
	"1-0:71.7.0(000.00*A)\r\n" +
	"0-0:96.3.10(1)\r\n" +
	"0-0:17.0.0(999.9*kW)\r\n" +
	"1-0:31.4.0(999*A)\r\n" +
	"0-0:96.13.0()\r\n" +
	"0-1:24.1.0(003)\r\n" +
	"0-1:96.1.1(37464C4F32313139303333373333)\r\n" +
	"0-1:24.4.0(1)\r\n" +
	"0-1:24.2.3(200512134558S)(00112.384*m3)\r\n" +
	"0-2:24.1.0(007)\r\n" +
	"0-2:96.1.1(3853414731323334353637383930)\r\n" +
	"0-2:24.2.1(200512134558S)(00872.234*m3)\r\n" +
	"!3AD7\r\n"

// luxembourgTelegram is a real Luxembourgish Smarty telegram with a valid
// CRC (80F6): totals only (1.8.0/2.8.0), equipment id on 0-0:42.0.0.
//
//nolint:gochecknoglobals // test data shared across test functions
var luxembourgTelegram = "/Lux5\\253663629_D\r\n" +
	"\r\n" +
	"1-3:0.2.8(42)\r\n" +
	"0-0:1.0.0(260403175431S)\r\n" +
	"0-0:42.0.0(53414731303330373030303132373031)\r\n" +
	"1-0:1.8.0(000273.764*kWh)\r\n" +
	"1-0:2.8.0(112743.030*kWh)\r\n" +
	"1-0:3.8.0(000462.590*kvarh)\r\n" +
	"1-0:4.8.0(007109.230*kvarh)\r\n" +
	"1-0:1.7.0(00.000*kW)\r\n" +
	"1-0:2.7.0(00.897*kW)\r\n" +
	"1-0:3.7.0(00.000*kvar)\r\n" +
	"1-0:4.7.0(00.154*kvar)\r\n" +
	"0-0:17.0.0(027.6*kVA)\r\n" +
	"1-0:9.7.0(00.000*kVA)\r\n" +
	"1-0:10.7.0(00.913*kVA)\r\n" +
	"1-1:31.4.0(040*A)(-040*A)\r\n" +
	"0-0:96.3.10(1)\r\n" +
	"0-1:96.3.10(0)\r\n" +
	"0-2:96.3.10(0)\r\n" +
	"0-0:96.7.21(01067)\r\n" +
	"1-0:32.32.0(00014)\r\n" +
	"1-0:52.32.0(00015)\r\n" +
	"1-0:72.32.0(00012)\r\n" +
	"1-0:32.36.0(00000)\r\n" +
	"1-0:52.36.0(00000)\r\n" +
	"1-0:72.36.0(00000)\r\n" +
	"0-0:96.13.0()\r\n" +
	"0-0:96.13.2()\r\n" +
	"0-0:96.13.3()\r\n" +
	"0-0:96.13.4()\r\n" +
	"0-0:96.13.5()\r\n" +
	"1-0:32.7.0(231.0*V)\r\n" +
	"1-0:52.7.0(229.0*V)\r\n" +
	"1-0:72.7.0(230.0*V)\r\n" +
	"1-0:31.7.0(001*A)\r\n" +
	"1-0:51.7.0(001*A)\r\n" +
	"1-0:71.7.0(001*A)\r\n" +
	"1-0:21.7.0(00.000*kW)\r\n" +
	"1-0:41.7.0(00.000*kW)\r\n" +
	"1-0:61.7.0(00.000*kW)\r\n" +
	"1-0:22.7.0(00.319*kW)\r\n" +
	"1-0:42.7.0(00.300*kW)\r\n" +
	"1-0:62.7.0(00.277*kW)\r\n" +
	"1-0:23.7.0(00.000*kvar)\r\n" +
	"1-0:43.7.0(00.000*kvar)\r\n" +
	"1-0:63.7.0(00.000*kvar)\r\n" +
	"1-0:24.7.0(00.046*kvar)\r\n" +
	"1-0:44.7.0(00.065*kvar)\r\n" +
	"1-0:64.7.0(00.041*kvar)\r\n" +
	"0-1:24.1.0(003)\r\n" +
	"0-1:96.1.0(464C4F313832333730313230333530)\r\n" +
	"0-1:24.2.1(260403174542S)(14239.771*m3)\r\n" +
	"0-1:24.4.0(1)\r\n" +
	"!80F6\r\n"

func TestReadGridTelegrams_FluviusBelgium(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(fluviusTelegram)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	tel := got[0]

	if tel.MeterMerkType != "50217" {
		t.Errorf("MeterMerkType = %q, want 50217 (from 0-0:96.1.4)", tel.MeterMerkType)
	}
	if tel.Serienummer != "3153414733313031303231363035" {
		t.Errorf("Serienummer = %q, want the 0-0:96.1.1 value", tel.Serienummer)
	}
	if tel.UsageCounter2 != 15.758 {
		t.Errorf("UsageCounter2 = %v, want 15.758", tel.UsageCounter2)
	}
	if tel.AvgDemand != 2351 {
		t.Errorf("AvgDemand = %d, want 2351", tel.AvgDemand)
	}
	if tel.MaxDemandMonth != 2589 {
		t.Errorf("MaxDemandMonth = %d, want 2589", tel.MaxDemandMonth)
	}
	wantPeakAt := time.Date(2020, 5, 9, 13, 45, 58, 0, time.FixedZone("CEST", 2*60*60))
	if !tel.MaxDemandMonthAt.Equal(wantPeakAt) {
		t.Errorf("MaxDemandMonthAt = %v, want %v", tel.MaxDemandMonthAt, wantPeakAt)
	}
	if tel.CurrentP1 != 0 || tel.CurrentP2 != 0 || tel.CurrentP3 != 0 {
		t.Errorf("currents = %d %d %d, want zeros from 000.00*A", tel.CurrentP1, tel.CurrentP2, tel.CurrentP3)
	}

	assertFluviusMBusDevices(t, tel.MBusDevices)
}

// assertFluviusMBusDevices checks the Belgian gas (24.2.3) and water
// subdevices; both are parsed, the service layer decides what to store.
func assertFluviusMBusDevices(t *testing.T, devices []domain.MBusDeviceReading) {
	t.Helper()
	if len(devices) != 2 {
		t.Fatalf("got %d M-Bus devices, want 2 (gas and water)", len(devices))
	}
	gas := devices[0]
	if gas.Channel != 1 || gas.DeviceType != domain.DeviceTypeGas {
		t.Errorf("device 0 = channel %d type %d, want channel 1 gas", gas.Channel, gas.DeviceType)
	}
	if gas.Value != 112.384 || gas.Unit != "m3" {
		t.Errorf("gas reading = %v %s, want 112.384 m3", gas.Value, gas.Unit)
	}
	if gas.SerialNo != "37464C4F32313139303333373333" {
		t.Errorf("gas serial = %q, want the 0-1:96.1.1 value", gas.SerialNo)
	}
	water := devices[1]
	if water.Channel != 2 || water.DeviceType != 7 {
		t.Errorf("device 1 = channel %d type %d, want channel 2 water (7)", water.Channel, water.DeviceType)
	}
	if water.Value != 872.234 {
		t.Errorf("water reading = %v, want 872.234", water.Value)
	}
}

// TestParseTelegram_DemandHistoryGarbage feeds the 13-month peak history line
// with the garbage timestamp Fluvius meters emit for empty months
// (632525252525W). The line must be ignored without failing the telegram.
func TestParseTelegram_DemandHistoryGarbage(t *testing.T) {
	msg := "/FLU5\\253769484_A\r\n\r\n" +
		"0-0:96.1.4(50217)\r\n" +
		"0-0:1.0.0(231102121548W)\r\n" +
		"1-0:1.8.1(000301.548*kWh)\r\n" +
		"1-0:1.8.2(000270.014*kWh)\r\n" +
		"1-0:2.8.1(000000.005*kWh)\r\n" +
		"1-0:2.8.2(000000.000*kWh)\r\n" +
		"1-0:1.4.0(00.052*kW)\r\n" +
		"1-0:1.6.0(231102114500W)(03.064*kW)\r\n" +
		"0-0:98.1.0(4)(1-0:1.6.0)(1-0:1.6.0)(230801000000S)(632525252525W)(00.000*kW)" +
		"(230901000000S)(230831181500S)(01.862*kW)\r\n" +
		"1-0:1.7.0(00.338*kW)\r\n" +
		"1-0:2.7.0(00.000*kW)\r\n" +
		"1-0:31.7.0(000.27*A)\r\n" +
		"1-0:51.7.0(000.88*A)\r\n" +
		"1-0:71.7.0(000.52*A)\r\n" +
		"!"
	tel, err := parseTelegram(msg)
	if err != nil {
		t.Fatalf("parseTelegram() error: %v", err)
	}
	if tel.AvgDemand != 52 {
		t.Errorf("AvgDemand = %d, want 52", tel.AvgDemand)
	}
	if tel.MaxDemandMonth != 3064 {
		t.Errorf("MaxDemandMonth = %d, want 3064", tel.MaxDemandMonth)
	}
	if tel.CurrentP1 != 0 || tel.CurrentP2 != 1 || tel.CurrentP3 != 1 {
		t.Errorf("currents = %d %d %d, want 0 1 1 (rounded)", tel.CurrentP1, tel.CurrentP2, tel.CurrentP3)
	}
}

func TestReadGridTelegrams_LuxembourgPlaintext(t *testing.T) {
	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(luxembourgTelegram)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	tel := got[0]
	if tel.Serienummer != "53414731303330373030303132373031" {
		t.Errorf("Serienummer = %q, want the 0-0:42.0.0 value", tel.Serienummer)
	}
	if tel.UsageCounter1 != 273.764 {
		t.Errorf("UsageCounter1 = %v, want 273.764 (from 1-0:1.8.0)", tel.UsageCounter1)
	}
	if tel.OutputCounter1 != 112743.030 {
		t.Errorf("OutputCounter1 = %v, want 112743.030 (from 1-0:2.8.0)", tel.OutputCounter1)
	}
	if tel.UsageCounter2 != 0 || tel.OutputCounter2 != 0 {
		t.Errorf("tariff-2 counters = %v %v, want zeros", tel.UsageCounter2, tel.OutputCounter2)
	}
	if tel.AvgDemand != 0 || tel.MaxDemandMonth != 0 || !tel.MaxDemandMonthAt.IsZero() {
		t.Errorf("demand fields should be zero when absent, got %d %d %v",
			tel.AvgDemand, tel.MaxDemandMonth, tel.MaxDemandMonthAt)
	}
}

func TestParseMonthlyPeak(t *testing.T) {
	location, err := loadGridMeterLocation()
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	tests := []struct {
		name     string
		raw      string
		wantW    int
		wantZero bool
	}{
		{name: "valid", raw: "200509134558S|02.589*kW", wantW: 2589},
		{name: "absent", raw: "", wantZero: true},
		{name: "no separator", raw: "02.589*kW", wantZero: true},
		{name: "garbage timestamp", raw: "632525252525W|00.000*kW", wantZero: true},
		{name: "garbage value", raw: "200509134558S|junk*kW", wantZero: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotAt := parseMonthlyPeak(tt.raw, location)
			if tt.wantZero {
				if gotW != 0 || !gotAt.IsZero() {
					t.Errorf("parseMonthlyPeak(%q) = %d, %v; want zeros", tt.raw, gotW, gotAt)
				}
				return
			}
			if gotW != tt.wantW {
				t.Errorf("parseMonthlyPeak(%q) = %d W, want %d", tt.raw, gotW, tt.wantW)
			}
			if gotAt.IsZero() {
				t.Errorf("parseMonthlyPeak(%q) returned zero time", tt.raw)
			}
		})
	}
}
