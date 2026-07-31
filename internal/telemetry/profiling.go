package telemetry

import (
	"fmt"
	"runtime"

	"github.com/grafana/pyroscope-go"

	"github.com/yottabytesolutions/meterlogger/internal/config"
)

// InitProfiling starts Pyroscope continuous profiling and returns a stop
// function. If cfg.Enabled is false it returns a no-op stop function.
func InitProfiling(cfg config.ProfilingConfig) (func() error, error) {
	if !cfg.Enabled {
		return func() error { return nil }, nil
	}

	// Enable block and mutex profiling so Pyroscope can collect them.
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	p, err := pyroscope.Start(
		pyroscope.Config{
			ApplicationName:   cfg.ServiceName,
			ServerAddress:     cfg.ServerAddr,
			BasicAuthUser:     cfg.BasicAuthUser,
			BasicAuthPassword: cfg.BasicAuthPassword,
			ProfileTypes: []pyroscope.ProfileType{
				pyroscope.ProfileCPU,
				pyroscope.ProfileAllocObjects,
				pyroscope.ProfileAllocSpace,
				pyroscope.ProfileInuseObjects,
				pyroscope.ProfileInuseSpace,
				pyroscope.ProfileGoroutines,
				pyroscope.ProfileMutexCount,
				pyroscope.ProfileMutexDuration,
				pyroscope.ProfileBlockCount,
				pyroscope.ProfileBlockDuration,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start Pyroscope profiler: %w", err)
	}

	return p.Stop, nil
}
