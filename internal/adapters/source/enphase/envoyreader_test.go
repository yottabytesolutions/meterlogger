package enphase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	testProductionPath = "/production.json"
	testInventoryPath  = "/inventory.json"

	testProductionTypeInverters = "inverters"
	testDeviceTypePCU           = "PCU"
	testEnvoyUser               = "user"
	testEnvoyPass               = "pass"
	testEnvoyURL                = "http://localhost"
	testEnvoySerial             = "serial"
	testInverterSerial          = "inv001"
	testInvertersPath           = "/api/v1/production/inverters"
	testEnvoySerialNo           = "serial123"
)

// testDeviceDataPayload is the raw /ivp/pdm/device_data body as the Envoy
// actually returns it: scalar keys (deviceCount, deviceDataLimit) mixed in at
// the top level, eim and nsrb devices alongside the pcu entries, and the
// milli-unit keys with Enphase's inconsistent casing (acVoltageINmV vs
// acCurrentInmA). This shape must not fail the whole decode.
const testDeviceDataPayload = `{
	"deviceCount": 14,
	"deviceDataLimit": 50,
	"704643328": {"devName": "eim", "sn": "eim1", "channels": []},
	"704643584": {"devName": "nsrb", "sn": "nsrb1", "channels": []},
	"553648384": {
		"devName": "pcu", "sn": "inv001", "active": true,
		"channels": [{
			"chanEid": 1,
			"wattHours": {"today": 429, "yesterday": 1950, "week": 12871},
			"watts": {"now": 150, "nowUsed": 0, "max": 200},
			"lastReading": {
				"dcVoltageINmV": 30000, "dcCurrentINmA": 8000,
				"acVoltageINmV": 235000, "acCurrentInmA": 1000,
				"acFrequencyINmHz": 50000, "channelTemp": 41,
				"leadingVArs": 91, "laggingVArs": 4, "rssi": 106, "issi": 62
			},
			"lifetime": {"joulesProduced": 7200}
		}]
	}
}`

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// makeTestToken creates a JWT token with MapClaims (not StandardClaims)
// so that ensureToken's type assertion fails and returns nil without network calls.
func makeTestToken() *jwt.Token {
	tokenStr := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0In0.signature" //nolint:gosec // G101: test token, not a real credential // gitleaks:allow
	token, _, _ := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
	return token
}

func TestNewEnvoyReader(t *testing.T) {
	reader := NewEnvoyReader(
		Config{EnvoyURL: testEnvoyURL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerial},
		testLogger(),
	)
	if reader == nil {
		t.Error("NewEnvoyReader returned nil")
	}
}

func TestUnmarshalInverterData_Valid(t *testing.T) {
	data := InverterData{
		{SerialNumber: "inv1", LastReportDate: 1234567890, DevType: 1, LastReportWatts: 250, MaxReportWatts: 300},
	}
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	result, err := unmarshalInverterData(body)
	if err != nil {
		t.Fatalf("unmarshalInverterData() error: %v", err)
	}
	if len(*result) != 1 {
		t.Errorf("len = %d, want 1", len(*result))
	}
	if (*result)[0].SerialNumber != "inv1" {
		t.Errorf("SerialNumber = %q, want inv1", (*result)[0].SerialNumber)
	}
}

func TestUnmarshalInverterData_Invalid(t *testing.T) {
	_, err := unmarshalInverterData([]byte(`{invalid}`))
	if err == nil {
		t.Error("unmarshalInverterData should return error for invalid JSON")
	}
}

func TestUnmarshalMeterReading_Valid(t *testing.T) {
	data := MeterReading{
		Production: []Production{
			{Type: testProductionTypeInverters, ActiveCount: 10, WNow: 250.0, WhLifetime: 5000.0},
		},
	}
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	result, err := unmarshalMeterReading(body)
	if err != nil {
		t.Fatalf("unmarshalMeterReading() error: %v", err)
	}
	if len(result.Production) != 1 {
		t.Errorf("Production count = %d, want 1", len(result.Production))
	}
	if result.Production[0].Type != testProductionTypeInverters {
		t.Errorf("Type = %q, want inverters", result.Production[0].Type)
	}
}

func TestUnmarshalMeterReading_Invalid(t *testing.T) {
	_, err := unmarshalMeterReading([]byte(`{invalid}`))
	if err == nil {
		t.Error("unmarshalMeterReading should return error for invalid JSON")
	}
}

func TestUnmarshalInventoryData_Valid(t *testing.T) {
	data := InventoryData{
		{
			Type: testDeviceTypePCU,
			Devices: []Device{
				{SerialNum: "dev1", Producing: true},
			},
		},
	}
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	result, err := unmarshalInventoryData(body)
	if err != nil {
		t.Fatalf("unmarshalInventoryData() error: %v", err)
	}
	if len(*result) != 1 {
		t.Errorf("len = %d, want 1", len(*result))
	}
}

func TestUnmarshalInventoryData_Invalid(t *testing.T) {
	_, err := unmarshalInventoryData([]byte(`{invalid}`))
	if err == nil {
		t.Error("unmarshalInventoryData should return error for invalid JSON")
	}
}

func TestUnmarshalDeviceData_MixedScalarsAndDevices(t *testing.T) {
	// The real endpoint mixes scalar keys with device objects and includes
	// non-pcu devices. The scalars must be skipped, not fail the decode.
	body := []byte(`{
		"deviceCount": 14,
		"deviceDataLimit": 50,
		"704643328": {"devName": "eim", "sn": "eim1"},
		"553648384": {"devName": "pcu", "sn": "inv001",
			"channels": [{"lastReading": {"dcVoltageINmV": 30000}}]}
	}`)
	data, err := unmarshalDeviceData(body)
	if err != nil {
		t.Fatalf("unmarshalDeviceData() error: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("device entries = %d, want 2 (eim + pcu, scalars skipped)", len(data))
	}
	lookup := deviceDataBySerial(data)
	ch, ok := lookup["inv001"]
	if !ok {
		t.Fatal("pcu inv001 not found in lookup")
	}
	if ch.LastReading.DCVoltageMV != 30000 {
		t.Errorf("DCVoltageMV = %d, want 30000", ch.LastReading.DCVoltageMV)
	}
}

func TestUnmarshalDeviceData_Invalid(t *testing.T) {
	_, err := unmarshalDeviceData([]byte(`{invalid}`))
	if err == nil {
		t.Error("unmarshalDeviceData should return error for invalid JSON")
	}
}

func TestReadEnvoySolarData_Success(t *testing.T) {
	// Set up mock HTTP responses
	meterData := MeterReading{
		Production: []Production{
			{
				Type: testProductionTypeInverters, ActiveCount: 2, WNow: 300.0,
				WhLifetime: 10000.0, ReadingTime: int(time.Now().Unix()),
			},
		},
	}
	inventoryData := InventoryData{
		{
			Type: testDeviceTypePCU,
			Devices: []Device{
				{
					SerialNum: testInverterSerial, Producing: true, Operating: true,
					Communicating: true, Phase: "A", Chaneid: 1,
				},
				{
					SerialNum: "inv002", Producing: true, Operating: true,
					Communicating: true, Phase: "B", Chaneid: 2,
				},
			},
		},
	}
	now := int(time.Now().Unix())
	inverterData := InverterData{
		{SerialNumber: testInverterSerial, LastReportDate: now, LastReportWatts: 150, MaxReportWatts: 200},
		{SerialNumber: "inv002", LastReportDate: now, LastReportWatts: 150, MaxReportWatts: 200},
	}
	deviceBody := []byte(testDeviceDataPayload)

	meterBody, _ := json.Marshal(meterData)
	inventoryBody, _ := json.Marshal(inventoryData)
	inverterBody, _ := json.Marshal(inverterData)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case testProductionPath:
					_, _ = w.Write(meterBody)
				case testInventoryPath:
					_, _ = w.Write(inventoryBody)
				case testInvertersPath:
					_, _ = w.Write(inverterBody)
				case "/ivp/pdm/device_data":
					_, _ = w.Write(deviceBody)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		),
	)
	defer server.Close()

	// Replace httpClient with test client
	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL, User: testEnvoyUser, Password: testEnvoyPass, Serial: testEnvoySerialNo},
		logger: testLogger(),
		token:  makeTestToken(),
	}

	result, err := reader.ReadEnvoySolarData(context.Background())
	if err != nil {
		t.Fatalf("ReadEnvoySolarData() error: %v", err)
	}

	if result.PanelCount != 2 {
		t.Errorf("PanelCount = %d, want 2", result.PanelCount)
	}
	if result.Watt != 300.0 {
		t.Errorf("Watt = %v, want 300.0", result.Watt)
	}
	if result.EnvoySerial != testEnvoySerialNo {
		t.Errorf("EnvoySerial = %q, want serial123", result.EnvoySerial)
	}
	if len(result.Inverters) != 2 {
		t.Errorf("Inverters count = %d, want 2", len(result.Inverters))
	}

	assertDeviceDataMerged(t, result.Inverters)
}

// assertDeviceDataMerged checks that the device_data electrical fields were
// merged into inv001 with milli-units converted to base units and joules to
// watt-hours.
func assertDeviceDataMerged(t *testing.T, inverters []domain.InverterDetails) {
	t.Helper()
	idx := -1
	for i := range inverters {
		if inverters[i].SerialNumber == testInverterSerial {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("%s not found in result", testInverterSerial)
	}
	inv := inverters[idx]
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"DCVoltage", inv.DCVoltage, 30.0},
		{"DCCurrent", inv.DCCurrent, 8.0},
		{"ACVoltage", inv.ACVoltage, 235.0},
		{"ACCurrent", inv.ACCurrent, 1.0},
		{"ACFrequency", inv.ACFrequency, 50.0},
		{"WhLifetime", inv.WhLifetime, 2.0},
		{"TemperatureC", float64(inv.TemperatureC), 41},
		{"WhToday", float64(inv.WhToday), 429},
		{"RSSI", float64(inv.RSSI), 106},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

// TestReadEnvoySolarData_DeviceDataErrorDegrades verifies that a device_data
// failure does not fail the whole read: the aggregate and per-panel watts still
// come back, with zero electrical fields.
func TestReadEnvoySolarData_DeviceDataErrorDegrades(t *testing.T) {
	meterData := MeterReading{
		Production: []Production{
			{Type: testProductionTypeInverters, ActiveCount: 1, WNow: 100, ReadingTime: int(time.Now().Unix())},
		},
	}
	inventoryData := InventoryData{}
	inverterData := InverterData{
		{SerialNumber: testInverterSerial, LastReportDate: int(time.Now().Unix()), LastReportWatts: 100},
	}
	meterBody, _ := json.Marshal(meterData)
	inventoryBody, _ := json.Marshal(inventoryData)
	inverterBody, _ := json.Marshal(inverterData)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testProductionPath:
					_, _ = w.Write(meterBody)
				case testInventoryPath:
					_, _ = w.Write(inventoryBody)
				case testInvertersPath:
					_, _ = w.Write(inverterBody)
				default: // device_data returns 500
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL, Serial: testEnvoySerialNo},
		logger: testLogger(),
		token:  makeTestToken(),
	}

	result, err := reader.ReadEnvoySolarData(context.Background())
	if err != nil {
		t.Fatalf("ReadEnvoySolarData() should degrade, got error: %v", err)
	}
	if len(result.Inverters) != 1 {
		t.Fatalf("Inverters count = %d, want 1", len(result.Inverters))
	}
	if result.Inverters[0].LastReportedWatts != 100 {
		t.Errorf("LastReportedWatts = %d, want 100", result.Inverters[0].LastReportedWatts)
	}
	if result.Inverters[0].DCVoltage != 0 {
		t.Errorf("DCVoltage = %v, want 0 (device_data failed)", result.Inverters[0].DCVoltage)
	}
}

func TestReadEnvoySolarData_MissingInvertersProduction(t *testing.T) {
	// Meter data with no "inverters" type production
	meterData := MeterReading{
		Production: []Production{
			{Type: "eim", ActiveCount: 0, WNow: 0},
		},
	}
	inventoryData := InventoryData{}
	inverterData := InverterData{}

	meterBody, _ := json.Marshal(meterData)
	inventoryBody, _ := json.Marshal(inventoryData)
	inverterBody, _ := json.Marshal(inverterData)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testProductionPath:
					_, _ = w.Write(meterBody)
				case testInventoryPath:
					_, _ = w.Write(inventoryBody)
				case testInvertersPath:
					_, _ = w.Write(inverterBody)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL},
		logger: testLogger(),
		token:  makeTestToken(),
	}

	_, err := reader.ReadEnvoySolarData(context.Background())
	if err == nil {
		t.Error("ReadEnvoySolarData() should return error when inverters production not found")
	}
}

func TestReadEnvoySolarData_MeterDataError(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL},
		logger: testLogger(),
		token:  makeTestToken(),
	}

	_, err := reader.ReadEnvoySolarData(context.Background())
	if err == nil {
		t.Error("ReadEnvoySolarData() should return error on HTTP failure")
	}
}

func TestReadEnvoySolarData_InventoryError(t *testing.T) {
	meterData := MeterReading{
		Production: []Production{
			{Type: testProductionTypeInverters, ActiveCount: 1, WNow: 100},
		},
	}
	meterBody, _ := json.Marshal(meterData)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testProductionPath:
					_, _ = w.Write(meterBody)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL},
		logger: testLogger(),
		token:  makeTestToken(),
	}

	_, err := reader.ReadEnvoySolarData(context.Background())
	if err == nil {
		t.Error("ReadEnvoySolarData() should return error when inventory call fails")
	}
}

func TestReadEnvoySolarData_InverterDataError(t *testing.T) {
	meterData := MeterReading{
		Production: []Production{
			{Type: testProductionTypeInverters, ActiveCount: 1, WNow: 100},
		},
	}
	inventoryData := InventoryData{}
	meterBody, _ := json.Marshal(meterData)
	inventoryBody, _ := json.Marshal(inventoryData)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case testProductionPath:
					_, _ = w.Write(meterBody)
				case testInventoryPath:
					_, _ = w.Write(inventoryBody)
				default:
					w.WriteHeader(http.StatusInternalServerError)
				}
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: server.URL},
		logger: testLogger(),
		token:  makeTestToken(),
	}

	_, err := reader.ReadEnvoySolarData(context.Background())
	if err == nil {
		t.Error("ReadEnvoySolarData() should return error when inverter data call fails")
	}
}

func TestQueryEnvoy_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(w, "not json")
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	// unmarshalInverterData will fail on "not json"
	_, err := unmarshalInverterData([]byte("not json"))
	if err == nil {
		t.Error("should fail on invalid JSON")
	}
}

func TestEnsureToken_WithExistingToken(t *testing.T) {
	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: testEnvoyURL},
		logger: testLogger(),
		token:  makeTestToken(),
	}
	err := reader.ensureToken(context.Background())
	if err != nil {
		t.Errorf("ensureToken() with existing MapClaims token should return nil: %v", err)
	}
}

func TestQueryEnvoy_UnexpectedStatusCode(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = fmt.Fprint(w, `{"error": "forbidden"}`)
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	_, err := queryEnvoy(context.Background(), server.URL+"/test", "fake-token", testLogger())
	if err == nil {
		t.Error("queryEnvoy() should return error for non-200 status")
	}
}

func TestQueryEnvoy_Success(t *testing.T) {
	expectedBody := []byte(`{"key": "value"}`)
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				// Verify token header
				authHeader := r.Header.Get("Authorization")
				if authHeader != "Bearer fake-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(expectedBody)
			},
		),
	)
	defer server.Close()

	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	body, err := queryEnvoy(context.Background(), server.URL+"/test", "fake-token", testLogger())
	if err != nil {
		t.Fatalf("queryEnvoy() error: %v", err)
	}
	if string(body) != string(expectedBody) {
		t.Errorf("queryEnvoy() body = %q, want %q", body, expectedBody)
	}
}

func TestQueryEnvoy_InvalidURL(t *testing.T) {
	// A URL with a control character that http.NewRequest rejects
	_, err := queryEnvoy(context.Background(), "http://host\x00/path", "token", testLogger())
	if err == nil {
		t.Error("queryEnvoy() should return error for invalid URL")
	}
}

func TestQueryEnvoy_ConnectionRefused(t *testing.T) {
	// Create and immediately close a server to get a port that refuses connections
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	addr := server.URL
	server.Close()

	// Restore real httpClient for this test (server.Client() would not work on closed server)
	_, err := queryEnvoy(context.Background(), addr+"/test", "token", testLogger())
	if err == nil {
		t.Error("queryEnvoy() should return error for refused connection")
	}
}

func TestEnsureToken_WithValidStandardClaims(t *testing.T) {
	// Create a JWT with StandardClaims and a far-future expiry
	// Base64url of {"alg":"HS256","typ":"JWT"}
	header := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
	// Base64url of {"sub":"test","exp":9999999999}
	payload := "eyJzdWIiOiJ0ZXN0IiwiZXhwIjo5OTk5OTk5OTk5fQ"
	tokenStr := header + "." + payload + ".signature"

	token, _, _ := new(jwt.Parser).ParseUnverified(tokenStr, &jwt.RegisteredClaims{})

	reader := &EnvoyReader{
		cfg:    Config{EnvoyURL: testEnvoyURL},
		logger: testLogger(),
		token:  token,
	}
	err := reader.ensureToken(context.Background())
	if err != nil {
		t.Errorf("ensureToken() with valid StandardClaims should not error: %v", err)
	}
}
