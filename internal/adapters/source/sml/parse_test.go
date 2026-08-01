package sml

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// fullPayload synthesizes the value-list entries of a fully unlocked meter,
// mixing present and absent status/valTime fields, an SML_Time valTime,
// both phase-power register sets, and an undecodable entry that must be
// skipped.
func fullPayload(t *testing.T) []byte {
	t.Helper()
	entries := []string{
		"77070100000009FF" + "01" + "01" + "01" + "01" + "0B0A0149534B0004123456",               // server ID
		"77078181C78203FF" + "01" + "01" + "01" + "01" + "0449534B",                             // manufacturer "ISK"
		"77070100010801FF" + "6500010180" + "01" + "621E" + "5200" + "590000000000BC614E",       // 12345678 Wh
		"77070100010802FF" + "01" + "726201650000000A" + "621E" + "5200" + "5900000000000003E8", // 1000 Wh
		"77070100010800FF" + "01" + "01" + "621E" + "5200" + "5900000000000000C7",               // total, tariffs win
		"77070100020801FF" + "01" + "01" + "621E" + "5200" + "6301F4",                           // 500 Wh export
		"77070100100700FF" + "01" + "01" + "621B" + "52FF" + "530FA0",                           // 4000 * 0.1 = 400 W
		"77070100150700FF" + "01" + "01" + "621B" + "5200" + "5264",                             // L1 +100 W
		"77070100290700FF" + "01" + "01" + "621B" + "5200" + "53FF9C",                           // L2 -100 W
		"770701004C0700FF" + "01" + "01" + "621B" + "5200" + "5232",                             // L3 +50 W, vendor set
		"77070100200700FF" + "01" + "01" + "6223" + "52FF" + "6308FC",                           // 230.0 V
		"770701001F0700FF" + "01" + "01" + "6221" + "52FE" + "630102",                           // 2.58 A
		"77070100600100FF" + "FF", // undecodable tail, skipped
	}
	var payload []byte
	for _, e := range entries {
		payload = append(payload, mustHex(t, e)...)
	}
	return payload
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestParsePayloadFull(t *testing.T) {
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	got, err := parsePayload(fullPayload(t), at)
	if err != nil {
		t.Fatalf("parsePayload: %v", err)
	}
	want := domain.GridTelegram{
		Time:            at,
		MeterMerkType:   "ISK",
		Serienummer:     "0a0149534b0004123456",
		UsageCounter1:   12345.678,
		UsageCounter2:   1.0,
		OutputCounter1:  0.5,
		TotalPowerUsage: 400,
		PowerUsageP1:    100,
		PowerOutputP2:   100,
		PowerUsageP3:    50,
		VoltageP1:       230.0,
		CurrentP1:       3, // 2.58 A rounded
	}
	if !almostEqual(got.UsageCounter1, want.UsageCounter1) ||
		!almostEqual(got.UsageCounter2, want.UsageCounter2) ||
		!almostEqual(got.OutputCounter1, want.OutputCounter1) ||
		!almostEqual(got.OutputCounter2, 0) {
		t.Errorf("counters = %v %v %v %v, want %v %v %v 0",
			got.UsageCounter1, got.UsageCounter2, got.OutputCounter1, got.OutputCounter2,
			want.UsageCounter1, want.UsageCounter2, want.OutputCounter1)
	}
	got.UsageCounter1, got.UsageCounter2, got.OutputCounter1 = want.UsageCounter1, want.UsageCounter2,
		want.OutputCounter1
	if !almostEqual(got.VoltageP1, want.VoltageP1) {
		t.Errorf("VoltageP1 = %v, want %v", got.VoltageP1, want.VoltageP1)
	}
	got.VoltageP1 = want.VoltageP1
	if !reflect.DeepEqual(got, want) {
		t.Errorf("telegram mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

// TestParsePayloadFactoryState covers a meter in factory state, which sends
// only the import total until the owner enters the meter PIN and enables
// the extended info mode. The total lands in counter 1.
func TestParsePayloadFactoryState(t *testing.T) {
	payload := mustHex(t, "77070100010800FF"+"01"+"01"+"621E"+"5200"+"5900000000000F4240") // 1000000 Wh
	at := time.Now()
	got, err := parsePayload(payload, at)
	if err != nil {
		t.Fatalf("parsePayload: %v", err)
	}
	if !almostEqual(got.UsageCounter1, 1000.0) {
		t.Errorf("UsageCounter1 = %v, want 1000", got.UsageCounter1)
	}
	if got.UsageCounter2 != 0 || got.OutputCounter1 != 0 || got.OutputCounter2 != 0 {
		t.Errorf("expected zero counters for absent registers, got %+v", got)
	}
	if got.TotalPowerUsage != 0 || got.TotalPowerOutput != 0 {
		t.Errorf("expected zero power for absent 16.7.0, got %+v", got)
	}
}

func TestParsePayloadExportTotalOnly(t *testing.T) {
	payload := append(
		mustHex(t, "77070100010800FF"+"01"+"01"+"621E"+"5200"+"5900000000000F4240"),
		mustHex(t, "77070100020800FF"+"01"+"01"+"621E"+"5200"+"5900000000000C3500")...) // 800000 Wh
	got, err := parsePayload(payload, time.Now())
	if err != nil {
		t.Fatalf("parsePayload: %v", err)
	}
	if !almostEqual(got.OutputCounter1, 800.0) || got.OutputCounter2 != 0 {
		t.Errorf("OutputCounter1 = %v (want 800), OutputCounter2 = %v (want 0)",
			got.OutputCounter1, got.OutputCounter2)
	}
}

// TestParsePayloadNegativeTotalPower covers a feeding-in meter: a negative
// 1-0:16.7.0 fills the output field, not the usage field.
func TestParsePayloadNegativeTotalPower(t *testing.T) {
	payload := append(
		mustHex(t, "77070100010800FF"+"01"+"01"+"621E"+"5200"+"5900000000000F4240"),
		mustHex(t, "77070100100700FF"+"01"+"01"+"621B"+"5200"+"53F060")...) // -4000 W
	got, err := parsePayload(payload, time.Now())
	if err != nil {
		t.Fatalf("parsePayload: %v", err)
	}
	if got.TotalPowerUsage != 0 || got.TotalPowerOutput != 4000 {
		t.Errorf("power usage/output = %d/%d, want 0/4000", got.TotalPowerUsage, got.TotalPowerOutput)
	}
}

func TestParsePayloadMissingImportTotal(t *testing.T) {
	payload := mustHex(t, "77070100100700FF"+"01"+"01"+"621B"+"5200"+"530FA0")
	if _, err := parsePayload(payload, time.Now()); !errors.Is(err, errMissingImportTotal) {
		t.Errorf("expected errMissingImportTotal, got %v", err)
	}
}

// TestHardcodedFrame validates the worked example frame byte for byte: a
// minimal transport frame around one 1-0:1.8.0 entry with value 29953191,
// scaler -1, so 2995319.1 Wh = 2995.3191 kWh. The CRC bytes FA 56 are the
// byte-swapped X-25 value precomputed for this frame.
func TestHardcodedFrame(t *testing.T) {
	frame := mustHex(t,
		"1B1B1B1B01010101"+
			"77070100010800FF650001018001621E52FF590000000001C90CA700"+
			"1B1B1B1B1A01FA56")
	payload, variant, err := validateFrame(frame)
	if err != nil {
		t.Fatalf("validateFrame: %v", err)
	}
	if variant != crcVariantX25 {
		t.Errorf("variant = %q, want x25", variant)
	}
	got, err := parsePayload(payload, time.Now())
	if err != nil {
		t.Fatalf("parsePayload: %v", err)
	}
	if !almostEqual(got.UsageCounter1, 2995.3191) {
		t.Errorf("UsageCounter1 = %v, want 2995.3191", got.UsageCounter1)
	}
	if got.UsageCounter2 != 0 {
		t.Errorf("UsageCounter2 = %v, want 0", got.UsageCounter2)
	}
}

// TestHardcodedFrameKermit is the same frame carrying the Kermit CRC 78 35,
// as a Holley DTZ541 would send it.
func TestHardcodedFrameKermit(t *testing.T) {
	frame := mustHex(t,
		"1B1B1B1B01010101"+
			"77070100010800FF650001018001621E52FF590000000001C90CA700"+
			"1B1B1B1B1A017835")
	_, variant, err := validateFrame(frame)
	if err != nil {
		t.Fatalf("validateFrame: %v", err)
	}
	if variant != crcVariantKermit {
		t.Errorf("variant = %q, want kermit", variant)
	}
}
