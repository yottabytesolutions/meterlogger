// Package metrics provides Prometheus metrics for meterlogger.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace   = "meterlogger"
	labelSource = "source"
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
				Namespace: namespace,
				Name:      "reads_total",
				Help:      "Total number of successful reads per source.",
			}, []string{labelSource},
		),

		ReadErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "read_errors_total",
				Help:      "Total number of read errors per source.",
			}, []string{labelSource},
		),

		WriteErrorsTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "write_errors_total",
				Help:      "Total number of write errors per sink and source.",
			}, []string{"sink", labelSource},
		),

		WritesTotal: f.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "writes_total",
				Help:      "Total number of successful writes per sink and source.",
			}, []string{"sink", labelSource},
		),

		LastReadTime: f.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "last_read_timestamp_seconds",
				Help:      "Unix timestamp of the last successful read per source.",
			}, []string{labelSource},
		),
	}
}
