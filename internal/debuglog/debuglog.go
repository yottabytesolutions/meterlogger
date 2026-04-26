// Package debuglog provides slog.Attr helpers for domain types.
//
// Use these helpers in slog.Logger.Debug calls to get consistent structured
// output across all sources and sinks:
//
//	logger.Debug("telegram received", debuglog.HeatAttrs(t))
//	logger.Debug("telegram queued",   debuglog.GridAttrs(t))
//
// Fields are logged at human-readable scale (W not mW, MJ not J, etc.).
// Adding a new domain type is one function here plus a one-liner at each call site.
package debuglog

import (
	"log/slog"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const (
	mwToW = int64(1000)
	jToMJ = int64(1_000_000)
)

// HeatAttrs returns a slog.Group with key fields of a HeatTelegram.
// ActualPower is converted from mW → W; Joules from J → MJ.
func HeatAttrs(t domain.HeatTelegram) slog.Attr {
	return slog.Group(
		"heat",
		slog.String("meterID", t.MeterID),
		slog.String("serial", t.SerialNo),
		slog.Time("ts", t.Timestamp),
		slog.Int64("power_w", t.ActualPower/mwToW),
		slog.Int64("energy_mj", t.Joules/jToMJ),
		slog.Float64("t_fwd_c", t.Tforward),
		slog.Float64("t_ret_c", t.Treturn),
		slog.Float64("flow_m3h", t.ActualFlow),
	)
}

// GridAttrs returns a slog.Group with key fields of a GridTelegram.
// TotalPowerUsage/Output are in W (already scaled in the reader).
func GridAttrs(t domain.GridTelegram) slog.Attr {
	return slog.Group(
		"grid",
		slog.String("serial", t.Serienummer),
		slog.Time("ts", t.Time),
		slog.Int("usage_w", t.TotalPowerUsage),
		slog.Int("output_w", t.TotalPowerOutput),
		slog.Float64("usage1_kwh", t.UsageCounter1),
		slog.Float64("usage2_kwh", t.UsageCounter2),
		slog.Float64("output1_kwh", t.OutputCounter1),
		slog.Float64("output2_kwh", t.OutputCounter2),
	)
}
