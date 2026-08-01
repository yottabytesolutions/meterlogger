package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

//nolint:gochecknoglobals // OTel tracer initialised at package level per OTel convention
var gridTracer = otel.Tracer("grid-meter-service")

// Units accepted for M-Bus subdevice readings: volume meters report m3,
// thermal meters report GJ.
const (
	unitM3 = "m3"
	unitGJ = "GJ"
)

type GridLoggingService struct {
	source         domain.GridTelegramReader
	sink           domain.GridTelegramRepository
	gasSink        domain.GasRepository
	waterSink      domain.WaterRepository
	thermalSink    domain.ThermalRepository
	flushInterval  time.Duration
	logger         *slog.Logger
	metrics        *metrics.Metrics
	dataFlowLogged bool
	lastCapture    map[int]time.Time
	skipLogged     map[int]bool
}

func NewGridLoggingService(
	source domain.GridTelegramReader,
	sink domain.GridTelegramRepository,
	flushInterval time.Duration,
	logger *slog.Logger,
) *GridLoggingService {
	return &GridLoggingService{
		source:        source,
		sink:          sink,
		flushInterval: flushInterval,
		logger:        logger,
		metrics:       metrics.NewNoop(),
		lastCapture:   make(map[int]time.Time),
		skipLogged:    make(map[int]bool),
	}
}

// WithMetrics attaches Prometheus metrics to the service.
func (s *GridLoggingService) WithMetrics(m *metrics.Metrics) *GridLoggingService {
	s.metrics = m
	return s
}

// WithGas attaches a gas repository fed from the telegram's M-Bus subdevices.
// A nil repo leaves gas logging disabled.
func (s *GridLoggingService) WithGas(repo domain.GasRepository) *GridLoggingService {
	s.gasSink = repo
	return s
}

// WithWater attaches a water repository fed from the telegram's M-Bus
// subdevices. A nil repo leaves water logging disabled.
func (s *GridLoggingService) WithWater(repo domain.WaterRepository) *GridLoggingService {
	s.waterSink = repo
	return s
}

// WithThermal attaches a thermal (heat and cooling) repository fed from the
// telegram's M-Bus subdevices. A nil repo leaves thermal logging disabled.
func (s *GridLoggingService) WithThermal(repo domain.ThermalRepository) *GridLoggingService {
	s.thermalSink = repo
	return s
}

func (s *GridLoggingService) Start(ctx context.Context) {
	flushTicker := time.NewTicker(s.flushInterval)
	defer flushTicker.Stop()

	var wg sync.WaitGroup
	wg.Go(func() {
		err := s.source.ReadGridTelegrams(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			s.logger.ErrorContext(ctx, "error reading grid meter data", slog.Any("error", err))
			processKiller()
		}
	})
	defer wg.Wait()

	telegrams := s.source.Telegrams()
	consecutiveErrors := 0
	for {
		select {
		case meterData, ok := <-telegrams:
			if !ok {
				// Reader exited and closed its channel. Its error path has
				// already escalated if needed; wait for shutdown.
				s.logger.InfoContext(ctx, "grid telegram channel closed, waiting for shutdown")
				<-ctx.Done()
				return
			}
			s.metrics.ReadsTotal.WithLabelValues("grid").Inc()
			s.metrics.LastReadTime.WithLabelValues("grid").SetToCurrentTime()
			if stop := s.handleStore(ctx, meterData, &consecutiveErrors); stop {
				return
			}
		case <-flushTicker.C:
			s.flushSinks(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// flushSinks flushes the grid repository and, when enabled, the gas, water
// and thermal repositories. Flush errors are logged but never escalate.
func (s *GridLoggingService) flushSinks(ctx context.Context) {
	s.logger.DebugContext(ctx, "flushing grid meter data")
	if err := withStoreTimeout(ctx, s.sink.Flush); err != nil {
		s.logger.ErrorContext(ctx, "error flushing grid meter data", slog.Any("error", err))
	}
	s.flushSubdeviceSink(ctx, "gas", s.gasSink)
	s.flushSubdeviceSink(ctx, "water", s.waterSink)
	s.flushSubdeviceSink(ctx, "thermal", s.thermalSink)
}

// flusher is the Flush subset shared by the subdevice repositories.
type flusher interface {
	Flush(ctx context.Context) error
}

// flushSubdeviceSink flushes one optional subdevice repository.
func (s *GridLoggingService) flushSubdeviceSink(ctx context.Context, kind string, sink flusher) {
	if sink == nil {
		return
	}
	if err := withStoreTimeout(ctx, sink.Flush); err != nil {
		s.logger.ErrorContext(ctx, "error flushing "+kind+" meter data", slog.Any("error", err))
	}
}

// handleStore stores one grid telegram plus its M-Bus subdevice readings and
// escalates on repeated failures. Returns true if the service should stop.
func (s *GridLoggingService) handleStore(
	ctx context.Context, meterData domain.GridTelegram, consecutiveErrors *int,
) bool {
	failed := 0
	if err := s.storeData(ctx, meterData); err != nil {
		failed++
		s.logger.ErrorContext(ctx, "error storing grid meter data", slog.Any("error", err))
	}
	failed += s.storeMBusReadings(ctx, meterData)
	if failed == 0 {
		*consecutiveErrors = 0
		return false
	}
	*consecutiveErrors += failed
	s.logger.ErrorContext(ctx, "grid meter store failures",
		slog.Int("failed", failed),
		slog.Int("consecutiveErrors", *consecutiveErrors),
	)
	if *consecutiveErrors >= maxConsecutiveErrors {
		s.logger.ErrorContext(ctx, "grid meter: too many consecutive errors, terminating")
		processKiller()
		<-ctx.Done()
		return true
	}
	return false
}

// storeMBusReadings stores the deduplicated M-Bus subdevice captures carried
// by one telegram and returns the number of failed stores. Routing is by
// device type: gas, water and thermal go to their repositories; everything
// else is skipped with one log line per channel.
func (s *GridLoggingService) storeMBusReadings(ctx context.Context, meterData domain.GridTelegram) int {
	failed := 0
	for _, dev := range meterData.MBusDevices {
		switch dev.DeviceType {
		case domain.DeviceTypeGas:
			if s.gasSink == nil {
				s.logSkippedOnce(ctx, dev, "gas storage not enabled")
				continue
			}
			failed += s.storeSubdeviceReading(ctx, "gas", unitM3, dev, func(c context.Context) error {
				return s.gasSink.StoreGasReading(c, gasReading(dev, meterData.Time))
			})
		case domain.DeviceTypeWaterWarm, domain.DeviceTypeWater:
			if s.waterSink == nil {
				s.logSkippedOnce(ctx, dev, "water storage not enabled")
				continue
			}
			failed += s.storeSubdeviceReading(ctx, "water", unitM3, dev, func(c context.Context) error {
				return s.waterSink.StoreWaterReading(c, waterReading(dev, meterData.Time))
			})
		case domain.DeviceTypeHeat, domain.DeviceTypeCoolingOutlet,
			domain.DeviceTypeCoolingInlet, domain.DeviceTypeHeatCool:
			if s.thermalSink == nil {
				s.logSkippedOnce(ctx, dev, "thermal storage not enabled")
				continue
			}
			failed += s.storeSubdeviceReading(ctx, "thermal", unitGJ, dev, func(c context.Context) error {
				return s.thermalSink.StoreThermalReading(c, thermalReading(dev, meterData.Time))
			})
		case domain.DeviceTypeSlaveEMeter:
			s.logSkippedOnce(ctx, dev, "slave e-meter is not stored; read the slave meter from its own P1 port")
		default:
			s.logSkippedOnce(ctx, dev, "unsupported M-Bus device type")
		}
	}
	return failed
}

// logSkippedOnce logs one skipped M-Bus subdevice per channel and stays
// silent on repeats.
func (s *GridLoggingService) logSkippedOnce(ctx context.Context, dev domain.MBusDeviceReading, reason string) {
	if s.skipLogged[dev.Channel] {
		return
	}
	s.skipLogged[dev.Channel] = true
	s.logger.DebugContext(ctx, "ignoring M-Bus device: "+reason,
		slog.Int("channel", dev.Channel),
		slog.Int("deviceType", dev.DeviceType),
	)
}

// storeSubdeviceReading stores one M-Bus subdevice capture with the shared
// per-channel dedup. CapturedAt is the deduplication key; a failed store
// leaves the key unset so the next telegram retries. Returns the number of
// failed stores (0 or 1).
func (s *GridLoggingService) storeSubdeviceReading(
	ctx context.Context, kind, wantUnit string, dev domain.MBusDeviceReading,
	store func(context.Context) error,
) int {
	if dev.Unit != wantUnit {
		s.logger.WarnContext(ctx, kind+" reading with unexpected unit, skipping",
			slog.Int("channel", dev.Channel),
			slog.String("unit", dev.Unit),
		)
		return 0
	}
	if last, seen := s.lastCapture[dev.Channel]; seen && last.Equal(dev.CapturedAt) {
		return 0
	}
	if err := withStoreTimeout(ctx, store); err != nil {
		s.metrics.WriteErrorsTotal.WithLabelValues("multisink", kind).Inc()
		s.logger.ErrorContext(ctx, "error storing "+kind+" reading",
			slog.Any("error", err),
			slog.Int("channel", dev.Channel),
		)
		return 1
	}
	s.metrics.WritesTotal.WithLabelValues("multisink", kind).Inc()
	s.lastCapture[dev.Channel] = dev.CapturedAt
	return 0
}

func gasReading(dev domain.MBusDeviceReading, receivedAt time.Time) domain.GasReading {
	return domain.GasReading{
		CapturedAt: dev.CapturedAt,
		ReceivedAt: receivedAt,
		Channel:    dev.Channel,
		DeviceType: dev.DeviceType,
		SerialNo:   dev.SerialNo,
		ReadingM3:  dev.Value,
	}
}

func waterReading(dev domain.MBusDeviceReading, receivedAt time.Time) domain.WaterReading {
	return domain.WaterReading{
		CapturedAt: dev.CapturedAt,
		ReceivedAt: receivedAt,
		Channel:    dev.Channel,
		DeviceType: dev.DeviceType,
		SerialNo:   dev.SerialNo,
		ReadingM3:  dev.Value,
	}
}

func thermalReading(dev domain.MBusDeviceReading, receivedAt time.Time) domain.ThermalReading {
	return domain.ThermalReading{
		CapturedAt: dev.CapturedAt,
		ReceivedAt: receivedAt,
		Channel:    dev.Channel,
		DeviceType: dev.DeviceType,
		SerialNo:   dev.SerialNo,
		ReadingGJ:  dev.Value,
	}
}

func (s *GridLoggingService) storeData(ctx context.Context, meterData domain.GridTelegram) error {
	ctx, span := gridTracer.Start(ctx, "StoreData")
	defer span.End()

	s.logger.DebugContext(ctx, "grid telegram received, storing", debuglog.GridAttrs(meterData))
	err := withStoreTimeout(ctx, func(c context.Context) error {
		return s.sink.StoreGridTelegram(c, meterData)
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "store grid telegram failed")
		s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "grid").Inc()
		return err
	}
	s.metrics.WritesTotal.WithLabelValues("multisink", "grid").Inc()
	if !s.dataFlowLogged {
		s.logger.InfoContext(ctx, "grid meter data flow started successfully", debuglog.GridAttrs(meterData))
		s.dataFlowLogged = true
	}
	return nil
}
