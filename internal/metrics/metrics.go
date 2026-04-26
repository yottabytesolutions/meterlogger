// Package metrics provides Prometheus metrics for meterlogger.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for meterlogger.
type Metrics struct {
	ReadsTotal       *prometheus.CounterVec
	ReadErrorsTotal  *prometheus.CounterVec
	WriteErrorsTotal *prometheus.CounterVec
	WritesTotal      *prometheus.CounterVec
	LastReadTime     *prometheus.GaugeVec
	Registry         *prometheus.Registry
}

// New registers and returns a Metrics instance using a fresh Prometheus registry.
// Passing a non-nil registry uses that registry instead (useful in tests to share one).
func New() *Metrics {
	reg := prometheus.NewRegistry()
	f := promauto.With(reg)

	return &Metrics{
		Registry: reg,

		ReadsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "meterlogger",
				Name:      "reads_total",
				Help:      "Total number of successful reads per source.",
			}, []string{"source"},
		),

		ReadErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "meterlogger",
				Name:      "read_errors_total",
				Help:      "Total number of read errors per source.",
			}, []string{"source"},
		),

		WriteErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "meterlogger",
				Name:      "write_errors_total",
				Help:      "Total number of write errors per sink and source.",
			}, []string{"sink", "source"},
		),

		WritesTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "meterlogger",
				Name:      "writes_total",
				Help:      "Total number of successful writes per sink and source.",
			}, []string{"sink", "source"},
		),

		LastReadTime: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: "meterlogger",
				Name:      "last_read_timestamp_seconds",
				Help:      "Unix timestamp of the last successful read per source.",
			}, []string{"source"},
		),
	}
}
