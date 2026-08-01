package gridmeter

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// mustLocation returns the cached Europe/Amsterdam location.
func mustLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := loadGridMeterLocation()
	if err != nil {
		t.Fatalf("loadGridMeterLocation() error: %v", err)
	}
	return location
}

// equalReadings compares two MBusDeviceReading slices, using time.Equal for
// the capture timestamps.
func equalReadings(got, want []domain.MBusDeviceReading) bool {
	return slices.EqualFunc(got, want, func(g, w domain.MBusDeviceReading) bool {
		return g.Channel == w.Channel && g.DeviceType == w.DeviceType && g.SerialNo == w.SerialNo &&
			g.Value == w.Value && g.Unit == w.Unit && g.CapturedAt.Equal(w.CapturedAt)
	})
}

type mbusParseCase struct {
	name    string
	message string
	want    []domain.MBusDeviceReading
}

func runMBusParseCases(t *testing.T, tests []mbusParseCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMBusDevices(context.Background(), tt.message, testLogger())
			if !equalReadings(got, tt.want) {
				t.Errorf("parseMBusDevices() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseMBusDevices(t *testing.T) {
	loc := mustLocation(t)
	tests := []mbusParseCase{
		{
			name: "dsmr5 gas channel 1",
			message: "0-1:24.1.0(003)\r\n" +
				"0-1:96.1.0(4730303339303031393336393930363139)\r\n" +
				"0-1:24.2.1(101209112500W)(12785.123*m3)\r\n",
			want: []domain.MBusDeviceReading{{
				Channel:    1,
				DeviceType: 3,
				SerialNo:   "4730303339303031393336393930363139",
				CapturedAt: time.Date(2010, 12, 9, 11, 25, 0, 0, loc),
				Value:      12785.123,
				Unit:       "m3",
			}},
		},
		{
			name: "dsmr4 two-digit device type padding",
			message: "0-1:24.1.0(03)\r\n" +
				"0-1:96.1.0(3232323241424344313233343536373839)\r\n" +
				"0-1:24.2.1(161129200000W)(00981.443*m3)\r\n",
			want: []domain.MBusDeviceReading{{
				Channel:    1,
				DeviceType: 3,
				SerialNo:   "3232323241424344313233343536373839",
				CapturedAt: time.Date(2016, 11, 29, 20, 0, 0, 0, loc),
				Value:      981.443,
				Unit:       "m3",
			}},
		},
		{
			name: "single-digit device type and summer time",
			message: "0-1:24.1.0(3)\r\n" +
				"0-1:24.2.1(190615143000S)(00123.456*m3)\r\n",
			want: []domain.MBusDeviceReading{{
				Channel:    1,
				DeviceType: 3,
				CapturedAt: time.Date(2019, 6, 15, 14, 30, 0, 0, loc),
				Value:      123.456,
				Unit:       "m3",
			}},
		},
		{
			name: "legacy dsmr22 reading on next line",
			message: "0-1:24.1.0(3)\r\n" +
				"0-1:96.1.0(303038333930)\r\n" +
				"0-1:24.3.0(161107190000)(00)(60)(1)(0-1:24.2.1)(m3)\r\n" +
				"(00001.001)\r\n",
			want: []domain.MBusDeviceReading{{
				Channel:    1,
				DeviceType: 3,
				SerialNo:   "303038333930",
				CapturedAt: time.Date(2016, 11, 7, 19, 0, 0, 0, loc),
				Value:      1.001,
				Unit:       "m3",
			}},
		},
		{
			name: "multiple channels with non-gas device typed correctly",
			message: "0-1:24.1.0(003)\r\n" +
				"0-1:24.2.1(210301060000W)(04321.100*m3)\r\n" +
				"0-2:24.1.0(004)\r\n" +
				"0-2:24.2.1(210301060000W)(00055.250*GJ)\r\n",
			want: []domain.MBusDeviceReading{
				{
					Channel:    1,
					DeviceType: 3,
					CapturedAt: time.Date(2021, 3, 1, 6, 0, 0, 0, loc),
					Value:      4321.1,
					Unit:       "m3",
				},
				{
					Channel:    2,
					DeviceType: 4,
					CapturedAt: time.Date(2021, 3, 1, 6, 0, 0, 0, loc),
					Value:      55.25,
					Unit:       "GJ",
				},
			},
		},
		{
			name: "malformed device type does not drop the reading",
			message: "0-1:24.1.0(xyz)\r\n" +
				"0-1:24.2.1(101209112500W)(12785.123*m3)\r\n",
			want: []domain.MBusDeviceReading{{
				Channel:    1,
				DeviceType: 0,
				CapturedAt: time.Date(2010, 12, 9, 11, 25, 0, 0, loc),
				Value:      12785.123,
				Unit:       "m3",
			}},
		},
	}
	runMBusParseCases(t, tests)
}

func TestParseMBusDevices_MalformedSkipped(t *testing.T) {
	tests := []mbusParseCase{
		{
			name: "channel without reading yields no entry",
			message: "0-1:24.1.0(003)\r\n" +
				"0-1:96.1.0(4730303339303031393336393930363139)\r\n",
			want: nil,
		},
		{
			name:    "malformed timestamp skipped",
			message: "0-1:24.2.1(NOTATIMESTAW)(1.0*m3)\r\n",
			want:    nil,
		},
		{
			name:    "invalid dst suffix skipped",
			message: "0-1:24.2.1(101209112500X)(1.0*m3)\r\n",
			want:    nil,
		},
		{
			name:    "value without unit skipped",
			message: "0-1:24.2.1(101209112500W)(12785.123)\r\n",
			want:    nil,
		},
		{
			name:    "legacy without reading line yields no entry",
			message: "0-1:24.3.0(161107190000)(00)(60)(1)(0-1:24.2.1)(m3)\r\n",
			want:    nil,
		},
		{
			name:    "legacy with non-numeric reading line skipped",
			message: "0-1:24.3.0(161107190000)(00)(60)(1)(0-1:24.2.1)(m3)\r\n(oops)\r\n",
			want:    nil,
		},
		{
			name:    "channel out of range ignored",
			message: "0-5:24.2.1(101209112500W)(12785.123*m3)\r\n",
			want:    nil,
		},
		{
			name:    "electricity lines ignored",
			message: "0-0:1.0.0(191130210919W)\r\n1-0:1.8.1(000239.922*kWh)\r\n",
			want:    nil,
		},
	}
	runMBusParseCases(t, tests)
}

// The W/S suffix must disambiguate the repeated hour at the DST fall-back.
// On 2019-10-27 the Dutch clock showed 02:30 twice: first in CEST (UTC+2),
// then in CET (UTC+1).
func TestParseMBusTimestamp_DSTFallback(t *testing.T) {
	loc := mustLocation(t)
	tests := []struct {
		name      string
		timestamp string
		wantUTC   time.Time
	}{
		{
			name:      "summer occurrence",
			timestamp: "191027023000S",
			wantUTC:   time.Date(2019, 10, 27, 0, 30, 0, 0, time.UTC),
		},
		{
			name:      "winter occurrence",
			timestamp: "191027023000W",
			wantUTC:   time.Date(2019, 10, 27, 1, 30, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMBusTimestamp(tt.timestamp, loc)
			if err != nil {
				t.Fatalf("parseMBusTimestamp(%q) error: %v", tt.timestamp, err)
			}
			if !got.Equal(tt.wantUTC) {
				t.Errorf("parseMBusTimestamp(%q) = %v, want %v", tt.timestamp, got.UTC(), tt.wantUTC)
			}
		})
	}
}

func TestParseMBusTimestamp_InvalidLength(t *testing.T) {
	loc := mustLocation(t)
	if _, err := parseMBusTimestamp("12345", loc); err == nil {
		t.Error("parseMBusTimestamp should reject a short timestamp")
	}
}

// A full read cycle must deliver the M-Bus devices on the parsed telegram.
func TestReadGridTelegrams_MBusDevices(t *testing.T) {
	loc := mustLocation(t)
	body := "/ISk5\\2MT382-1000\r\n" +
		"\r\n" +
		"0-0:1.0.0(191130210919W)\r\n" +
		"1-0:1.8.1(000239.922*kWh)\r\n" +
		"1-0:1.8.2(000239.621*kWh)\r\n" +
		"1-0:2.8.1(000003.448*kWh)\r\n" +
		"1-0:2.8.2(000000.000*kWh)\r\n" +
		"1-0:1.7.0(00.577*kW)\r\n" +
		"1-0:2.7.0(00.000*kW)\r\n" +
		"0-1:24.1.0(003)\r\n" +
		"0-1:96.1.0(4730303339303031393336393930363139)\r\n" +
		"0-1:24.2.1(191130210500W)(12785.123*m3)\r\n" +
		"!"
	telegramText := body + crcToHex(calculateCrc16([]byte(body))) + "\r\n"

	reader := NewGridReader("/dev/null", testLogger())
	reader.portReader = strings.NewReader(telegramText)

	got, err := runReader(t, reader)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadGridTelegrams() error = %v, want wrapped io.EOF", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadGridTelegrams() produced %d telegrams, want 1", len(got))
	}
	want := []domain.MBusDeviceReading{{
		Channel:    1,
		DeviceType: 3,
		SerialNo:   "4730303339303031393336393930363139",
		CapturedAt: time.Date(2019, 11, 30, 21, 5, 0, 0, loc),
		Value:      12785.123,
		Unit:       "m3",
	}}
	if !equalReadings(got[0].MBusDevices, want) {
		t.Errorf("MBusDevices = %+v, want %+v", got[0].MBusDevices, want)
	}
}
