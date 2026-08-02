package service

import (
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

// inverterDeduplicator drops per-inverter rows whose report time has not
// advanced since the last store. Microinverters report over powerline roughly
// every five minutes, staggered, so a poll interval shorter than that would
// otherwise write many identical rows per panel (same report time, same
// values) between reports. The deduplicator keeps exactly one row per panel
// per new report, regardless of poll rate.
//
// The gateway aggregate is never deduplicated: it is a genuine per-poll time
// series (current watts, lifetime production) and every sample is meaningful.
//
// State is in memory, keyed by inverter serial number, and resets when the
// process restarts. After a restart the first poll writes the current report
// for each panel once, even if that report is unchanged from before the
// restart. That is a bounded, at-most-once-per-panel-per-restart duplicate at
// the panel's true report time, not a correctness problem for downstream
// last-value or time-bucketed queries.
type inverterDeduplicator struct {
	lastReport map[string]time.Time
}

func newInverterDeduplicator() *inverterDeduplicator {
	return &inverterDeduplicator{lastReport: make(map[string]time.Time)}
}

// filter returns data with Inverters reduced to those whose ReportTime is
// newer than the last one stored for that serial. The returned value shares
// the input's aggregate fields; only the Inverters slice is replaced, so the
// caller's slice backing array is left untouched. An inverter with a zero
// ReportTime is treated as a report at the zero instant: written once on first
// sight, then suppressed until a real report arrives.
func (d *inverterDeduplicator) filter(data domain.EnvoySolarData) domain.EnvoySolarData {
	fresh := make([]domain.InverterDetails, 0, len(data.Inverters))
	for _, inv := range data.Inverters {
		last, seen := d.lastReport[inv.SerialNumber]
		if seen && !inv.ReportTime.After(last) {
			continue
		}
		d.lastReport[inv.SerialNumber] = inv.ReportTime
		fresh = append(fresh, inv)
	}
	data.Inverters = fresh
	return data
}
