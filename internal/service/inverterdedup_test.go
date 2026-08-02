package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
)

const dedupTestEnvoy = "env1"

func inv(serial string, report time.Time) domain.InverterDetails {
	return domain.InverterDetails{SerialNumber: serial, ReportTime: report}
}

func serials(inverters []domain.InverterDetails) []string {
	out := make([]string, len(inverters))
	for i, v := range inverters {
		out[i] = v.SerialNumber
	}
	return out
}

func TestInverterDeduplicator_Filter(t *testing.T) {
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	t5 := base.Add(5 * time.Minute)

	// Each step feeds one reading through the same deduplicator and asserts
	// which inverter serials survive. Steps run in order against shared state.
	steps := []struct {
		name string
		in   []domain.InverterDetails
		want []string
	}{
		{
			name: "first sight writes all",
			in:   []domain.InverterDetails{inv("a", base), inv("b", base)},
			want: []string{"a", "b"},
		},
		{
			name: "unchanged report times are dropped",
			in:   []domain.InverterDetails{inv("a", base), inv("b", base)},
			want: []string{},
		},
		{
			name: "only the advanced panel passes",
			in:   []domain.InverterDetails{inv("a", t5), inv("b", base)},
			want: []string{"a"},
		},
		{
			name: "a newly seen panel passes",
			in:   []domain.InverterDetails{inv("a", t5), inv("c", base)},
			want: []string{"c"},
		},
		{
			name: "a report time going backwards is dropped",
			in:   []domain.InverterDetails{inv("a", base)},
			want: []string{},
		},
	}

	d := newInverterDeduplicator()
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			got := serials(d.filter(domain.EnvoySolarData{Inverters: step.in}).Inverters)
			if !slices.Equal(got, step.want) {
				t.Errorf("filter() serials = %v, want %v", got, step.want)
			}
		})
	}
}

func TestInverterDeduplicator_ZeroReportTime(t *testing.T) {
	zero := time.Time{}
	d := newInverterDeduplicator()

	one := []domain.InverterDetails{inv("z", zero)}
	first := serials(d.filter(domain.EnvoySolarData{Inverters: one}).Inverters)
	if !slices.Equal(first, []string{"z"}) {
		t.Errorf("first zero-time filter = %v, want [z]", first)
	}
	second := serials(d.filter(domain.EnvoySolarData{Inverters: one}).Inverters)
	if !slices.Equal(second, []string{}) {
		t.Errorf("second zero-time filter = %v, want []", second)
	}
}

// TestInverterDeduplicator_PreservesAggregateAndInput checks that the gateway
// aggregate is untouched and the caller's input slice is not mutated.
func TestInverterDeduplicator_PreservesAggregateAndInput(t *testing.T) {
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	d := newInverterDeduplicator()
	d.filter(domain.EnvoySolarData{Inverters: []domain.InverterDetails{inv("a", base)}})

	input := domain.EnvoySolarData{
		Watt: 2500, ProductionWh: 1000, PanelCount: 2, EnvoySerial: dedupTestEnvoy,
		Inverters: []domain.InverterDetails{inv("a", base), inv("b", base)},
	}
	out := d.filter(input)

	if out.Watt != 2500 || out.ProductionWh != 1000 || out.PanelCount != 2 || out.EnvoySerial != dedupTestEnvoy {
		t.Errorf("aggregate fields changed: %+v", out)
	}
	if len(input.Inverters) != 2 {
		t.Errorf("input slice was mutated, len = %d, want 2", len(input.Inverters))
	}
	if got := serials(out.Inverters); !slices.Equal(got, []string{"b"}) {
		t.Errorf("filtered serials = %v, want [b]", got)
	}
}

// TestSolarLoggingService_DeduplicatesInverters checks the wiring: across two
// polls of an unchanged reading, the aggregate is stored twice but each
// inverter only once.
func TestSolarLoggingService_DeduplicatesInverters(t *testing.T) {
	report := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	data := domain.EnvoySolarData{
		Watt: 300, EnvoySerial: dedupTestEnvoy,
		Inverters: []domain.InverterDetails{inv("a", report), inv("b", report)},
	}
	reader := &mockSolarReader{data: data}
	repo := &mockSolarRepo{}
	svc := NewSolarLoggingService(reader, repo, time.Hour, time.Hour, testLogger())

	for range 2 {
		if err := svc.runReadAndStore(context.Background()); err != nil {
			t.Fatalf("runReadAndStore: %v", err)
		}
	}

	if len(repo.stored) != 2 {
		t.Fatalf("stored snapshots = %d, want 2", len(repo.stored))
	}
	if got := len(repo.stored[0].Inverters); got != 2 {
		t.Errorf("first poll inverter rows = %d, want 2", got)
	}
	if got := len(repo.stored[1].Inverters); got != 0 {
		t.Errorf("second poll inverter rows = %d, want 0 (deduplicated)", got)
	}
}
