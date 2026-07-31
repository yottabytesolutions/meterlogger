package ducobox

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestParseDucoNodeStatus_Box(t *testing.T) {
	node := nodeBoxStatusDTO{
		baseNodeStatusDTO: baseNodeStatusDTO{Node: 1, DevType: devTypeBox},
		Trgt:              100,
		Actl:              80,
		Rh:                55.5,
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	result, err := ParseDucoNodeStatus(data)
	if err != nil {
		t.Fatalf("ParseDucoNodeStatus(BOX) error: %v", err)
	}

	parsed, ok := result.(domain.DucoNodeBoxStatus)
	if !ok {
		t.Fatalf("expected DucoNodeBoxStatus, got %T", result)
	}
	if parsed.DevType != devTypeBox {
		t.Errorf("DevType = %q, want BOX", parsed.DevType)
	}
	if parsed.Rh != 55.5 {
		t.Errorf("Rh = %v, want 55.5", parsed.Rh)
	}
}

func TestParseDucoNodeStatus_BoxLowercase(t *testing.T) {
	data := []byte(
		`{"node":1,"devtype":"box","subtype":0,"netw":"","addr":0,"sub":0,"prnt":0,"asso":0,` +
			`"location":"","state":"","cntdwn":0,"mode":"","ovrl":0,"snsr":0,"cerr":0,` +
			`"swversion":"","serialnb":"","show":0,"link":0,"trgt":0,"actl":0,"rh":0,"temp":0,"co2":0}`,
	)
	result, err := ParseDucoNodeStatus(data)
	if err != nil {
		t.Fatalf("ParseDucoNodeStatus(box lowercase) error: %v", err)
	}
	_, ok := result.(domain.DucoNodeBoxStatus)
	if !ok {
		t.Errorf("expected DucoNodeBoxStatus, got %T", result)
	}
}

func TestParseDucoNodeStatus_VLV(t *testing.T) {
	node := nodeBoxValveStatusDTO{
		baseNodeStatusDTO: baseNodeStatusDTO{Node: 2, DevType: devTypeValve},
		Trgt:              50,
		Actl:              45,
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	result, err := ParseDucoNodeStatus(data)
	if err != nil {
		t.Fatalf("ParseDucoNodeStatus(VLV) error: %v", err)
	}

	parsed, ok := result.(domain.DucoNodeBoxValveStatus)
	if !ok {
		t.Fatalf("expected DucoNodeBoxValveStatus, got %T", result)
	}
	if parsed.DevType != devTypeValve {
		t.Errorf("DevType = %q, want VLV", parsed.DevType)
	}
}

func TestParseDucoNodeStatus_UCCO2(t *testing.T) {
	node := rfSensorStatusDTO{
		baseNodeStatusDTO: baseNodeStatusDTO{Node: 3, DevType: devTypeUCCO2},
		Co2:               800.0,
		Rh:                0,
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	result, err := ParseDucoNodeStatus(data)
	if err != nil {
		t.Fatalf("ParseDucoNodeStatus(UCCO2) error: %v", err)
	}

	parsed, ok := result.(domain.DucoRFSensorStatus)
	if !ok {
		t.Fatalf("expected DucoRFSensorStatus, got %T", result)
	}
	if parsed.Co2 != 800.0 {
		t.Errorf("Co2 = %v, want 800.0", parsed.Co2)
	}
}

func TestParseDucoNodeStatus_UCRH(t *testing.T) {
	node := rfSensorStatusDTO{
		baseNodeStatusDTO: baseNodeStatusDTO{Node: 4, DevType: devTypeUCRH},
		Co2:               0,
		Rh:                65.0,
	}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	result, err := ParseDucoNodeStatus(data)
	if err != nil {
		t.Fatalf("ParseDucoNodeStatus(UCRH) error: %v", err)
	}

	parsed, ok := result.(domain.DucoRFSensorStatus)
	if !ok {
		t.Fatalf("expected DucoRFSensorStatus, got %T", result)
	}
	if parsed.Rh != 65.0 {
		t.Errorf("Rh = %v, want 65.0", parsed.Rh)
	}
}

func TestParseDucoNodeStatus_UCRH_SwapsValues(t *testing.T) {
	// Co2 has value but Rh is 0 → should be swapped
	data := []byte(
		`{"node":5,"devtype":"UCRH","subtype":0,"netw":"","addr":0,"sub":0,"prnt":0,"asso":0,` +
			`"location":"","state":"","cntdwn":0,"mode":"","ovrl":0,"snsr":0,"cerr":0,"swversion":"",` +
			`"serialnb":"","show":0,"link":0,"temp":0,"co2":75.5,"rh":0,"rssi_n2m":0,"hop_via":0,"rssi_n2h":0}`,
	)
	result, err := ParseDucoNodeStatus(data)
	if err != nil {
		t.Fatalf("ParseDucoNodeStatus(UCRH swap) error: %v", err)
	}

	parsed, ok := result.(domain.DucoRFSensorStatus)
	if !ok {
		t.Fatalf("expected DucoRFSensorStatus, got %T", result)
	}
	if parsed.Rh != 75.5 {
		t.Errorf("after swap: Rh = %v, want 75.5", parsed.Rh)
	}
	if parsed.Co2 != 0 {
		t.Errorf("after swap: Co2 = %v, want 0", parsed.Co2)
	}
}

func TestParseDucoNodeStatus_UnknownType(t *testing.T) {
	data := []byte(
		`{"node":1,"devtype":"UNKN","subtype":0,"netw":"","addr":0,"sub":0,"prnt":0,"asso":0,` +
			`"location":"","state":"","cntdwn":0,"mode":"","ovrl":0,"snsr":0,"cerr":0,"swversion":"",` +
			`"serialnb":"","show":0,"link":0}`,
	)
	_, err := ParseDucoNodeStatus(data)
	if !errors.Is(err, domain.ErrUnknownDevType) {
		t.Errorf("ParseDucoNodeStatus(UNKN) error = %v, want domain.ErrUnknownDevType", err)
	}
}

func TestParseDucoNodeStatus_InvalidJSON(t *testing.T) {
	_, err := ParseDucoNodeStatus([]byte(`{invalid json`))
	if err == nil {
		t.Error("ParseDucoNodeStatus should return error for invalid JSON")
	}
}

// --- DucoReader HTTP tests ---

func TestNewDucoReader(t *testing.T) {
	reader := NewDucoReader("http://localhost", testLogger())
	if reader == nil {
		t.Error("NewDucoReader returned nil")
	}
}

func TestDucoReader_ReadBoxStatus_Success(t *testing.T) {
	boxStatusDTO := ducoBoxStatusDTO{
		General: generalDTO{RFHomeID: "home1"},
	}
	body, _ := json.Marshal(boxStatusDTO)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			},
		),
	)
	defer server.Close()

	reader := NewDucoReader(server.URL, testLogger())
	result, err := reader.ReadBoxStatus(t.Context())
	if err != nil {
		t.Fatalf("ReadBoxStatus() error: %v", err)
	}
	if result.General.RFHomeID != "home1" {
		t.Errorf("RFHomeID = %q, want home1", result.General.RFHomeID)
	}
}

func TestDucoReader_ReadBoxStatus_HTTPError(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		),
	)
	defer server.Close()

	reader := NewDucoReader(server.URL, testLogger())
	_, err := reader.ReadBoxStatus(t.Context())
	if err == nil {
		t.Error("ReadBoxStatus() should return error for HTTP 500")
	}
}

func TestDucoReader_ReadBoxStatus_InvalidURL(t *testing.T) {
	reader := NewDucoReader("http://invalid-host-that-does-not-exist.local", testLogger())
	_, err := reader.ReadBoxStatus(t.Context())
	if err == nil {
		t.Error("ReadBoxStatus() should return error for invalid host")
	}
}

func TestDucoReader_ReadBoxStatus_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{invalid json`))
			},
		),
	)
	defer server.Close()

	reader := NewDucoReader(server.URL, testLogger())
	_, err := reader.ReadBoxStatus(t.Context())
	if err == nil {
		t.Error("ReadBoxStatus() should return error for invalid JSON")
	}
}

func TestDucoReader_ReadNodeStatus_Success(t *testing.T) {
	node := nodeBoxStatusDTO{
		baseNodeStatusDTO: baseNodeStatusDTO{Node: 1, DevType: devTypeBox},
		Trgt:              100,
	}
	body, _ := json.Marshal(node)

	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			},
		),
	)
	defer server.Close()

	reader := NewDucoReader(server.URL, testLogger())
	result, err := reader.ReadNodeStatus(t.Context(), 1)
	if err != nil {
		t.Fatalf("ReadNodeStatus() error: %v", err)
	}
	if result == nil {
		t.Error("ReadNodeStatus() returned nil")
	}
}

func TestDucoReader_ReadNodeStatus_HTTPError(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		),
	)
	defer server.Close()

	reader := NewDucoReader(server.URL, testLogger())
	_, err := reader.ReadNodeStatus(t.Context(), 1)
	if err == nil {
		t.Error("ReadNodeStatus() should return error for HTTP 404")
	}
}

func TestDucoReader_ReadNodeStatus_InvalidURL(t *testing.T) {
	reader := NewDucoReader("http://invalid-host-that-does-not-exist.local", testLogger())
	_, err := reader.ReadNodeStatus(t.Context(), 1)
	if err == nil {
		t.Error("ReadNodeStatus() should return error for invalid host")
	}
}

func TestDucoReader_ReadNodeStatus_UnknownDevType(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = fmt.Fprint(
					w,
					`{"node":1,"devtype":"UNKN","subtype":0,"netw":"","addr":0,"sub":0,"prnt":0,"asso":0,`+
						`"location":"","state":"","cntdwn":0,"mode":"","ovrl":0,"snsr":0,"cerr":0,"swversion":"","serialnb":"","show":0,"link":0}`,
				)
			},
		),
	)
	defer server.Close()

	reader := NewDucoReader(server.URL, testLogger())
	_, err := reader.ReadNodeStatus(t.Context(), 1)
	if !errors.Is(err, domain.ErrUnknownDevType) {
		t.Errorf("ReadNodeStatus() error = %v, want domain.ErrUnknownDevType", err)
	}
}
