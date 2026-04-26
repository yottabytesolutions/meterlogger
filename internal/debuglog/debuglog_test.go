package debuglog_test

import (
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

func TestHeatAttrs(t *testing.T) {
	tel := domain.HeatTelegram{
		MeterID:     "302",
		SerialNo:    "12345678",
		Timestamp:   time.Now(),
		ActualPower: 5000,
		Joules:      2_000_000,
		Tforward:    70.5,
		Treturn:     40.0,
		ActualFlow:  0.5,
	}

	attr := debuglog.HeatAttrs(tel)
	if attr.Key != "heat" {
		t.Errorf("expected key %q, got %q", "heat", attr.Key)
	}
}

func TestGridAttrs(t *testing.T) {
	tel := domain.GridTelegram{
		Serienummer:      "E0031234567890",
		Time:             time.Now(),
		TotalPowerUsage:  1000,
		TotalPowerOutput: 0,
		UsageCounter1:    100.5,
		UsageCounter2:    200.5,
		OutputCounter1:   5.0,
		OutputCounter2:   0.0,
	}

	attr := debuglog.GridAttrs(tel)
	if attr.Key != "grid" {
		t.Errorf("expected key %q, got %q", "grid", attr.Key)
	}
}
