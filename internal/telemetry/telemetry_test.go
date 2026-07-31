package telemetry

// The enabled paths need a reachable OTLP collector and Pyroscope server, so
// only the disabled no-op paths are tested here.

import (
	"context"
	"testing"

	"github.com/yottabytesolutions/meterlogger/internal/config"
)

func TestInitTracing_Disabled(t *testing.T) {
	stop, err := InitTracing(context.Background(), config.OTELConfig{Enabled: false})
	if err != nil {
		t.Fatalf("InitTracing() error = %v", err)
	}
	if stopErr := stop(context.Background()); stopErr != nil {
		t.Errorf("stop() error = %v, want nil from the no-op", stopErr)
	}
}

func TestInitProfiling_Disabled(t *testing.T) {
	stop, err := InitProfiling(config.ProfilingConfig{Enabled: false})
	if err != nil {
		t.Fatalf("InitProfiling() error = %v", err)
	}
	if stopErr := stop(); stopErr != nil {
		t.Errorf("stop() error = %v, want nil from the no-op", stopErr)
	}
}
