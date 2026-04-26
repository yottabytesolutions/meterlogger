package metrics_test

import (
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

func TestNew_ReturnsNonNilMetrics(t *testing.T) {
	m := metrics.New()
	if m.ReadsTotal == nil {
		t.Error("ReadsTotal is nil")
	}
	if m.ReadErrorsTotal == nil {
		t.Error("ReadErrorsTotal is nil")
	}
	if m.WritesTotal == nil {
		t.Error("WritesTotal is nil")
	}
	if m.WriteErrorsTotal == nil {
		t.Error("WriteErrorsTotal is nil")
	}
	if m.LastReadTime == nil {
		t.Error("LastReadTime is nil")
	}
}

func TestNew_CountersAreUsable(_ *testing.T) {
	m := metrics.New()

	// Verify counters and gauges accept label values without panicking.
	sources := []string{"heat", "grid", "solar", "ventilation"}
	sinks := []string{"multisink"}

	for _, src := range sources {
		m.ReadsTotal.WithLabelValues(src).Inc()
		m.ReadErrorsTotal.WithLabelValues(src).Inc()
		m.LastReadTime.WithLabelValues(src).SetToCurrentTime()
		for _, sink := range sinks {
			m.WritesTotal.WithLabelValues(sink, src).Inc()
			m.WriteErrorsTotal.WithLabelValues(sink, src).Inc()
		}
	}
}
