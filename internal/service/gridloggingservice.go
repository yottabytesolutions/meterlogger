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

// gasUnitM3 is the only unit accepted for a gas M-Bus subdevice reading.
const gasUnitM3 = "m3"

type GridLoggingService struct {
	source         domain.GridTelegramReader
	sink           domain.GridTelegramRepository
	gasSink        domain.GasRepository
	flushInterval  time.Duration
	logger         *slog.Logger
	metrics        *metrics.Metrics
	dataFlowLogged bool
	lastGasCapture map[int]time.Time
	nonGasLogged   map[int]bool
}

func NewGridLoggingService(
	source domain.GridTelegramReader,
	sink domain.GridTelegramRepository,
	flushInterval time.Duration,
	logger *slog.Logger,
) *GridLoggingService {
	return &GridLoggingService{
		source:         source,
		sink:           sink,
		flushInterval:  flushInterval,
		logger:         logger,
		metrics:        metrics.NewNoop(),
		lastGasCapture: make(map[int]time.Time),
		nonGasLogged:   make(map[int]bool),
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

// flushSinks flushes the grid repository and, when enabled, the gas
// repository. Flush errors are logged but never escalate.
func (s *GridLoggingService) flushSinks(ctx context.Context) {
	s.logger.DebugContext(ctx, "flushing grid meter data")
	if err := withStoreTimeout(ctx, s.sink.Flush); err != nil {
		s.logger.ErrorContext(ctx, "error flushing grid meter data", slog.Any("error", err))
	}
	if s.gasSink == nil {
		return
	}
	if err := withStoreTimeout(ctx, s.gasSink.Flush); err != nil {
		s.logger.ErrorContext(ctx, "error flushing gas meter data", slog.Any("error", err))
	}
}

// handleStore stores one grid telegram plus its gas subdevice readings and
// escalates on repeated failures. Returns true if the service should stop.
func (s *GridLoggingService) handleStore(
	ctx context.Context, meterData domain.GridTelegram, consecutiveErrors *int,
) bool {
	failed := 0
	if err := s.storeData(ctx, meterData); err != nil {
		failed++
		s.logger.ErrorContext(ctx, "error storing grid meter data", slog.Any("error", err))
	}
	failed += s.storeGasReadings(ctx, meterData)
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

// storeGasReadings stores the deduplicated gas captures carried by one
// telegram and returns the number of failed stores. CapturedAt is the
// deduplication key per channel; a failed store leaves the key unset so the
// next telegram retries. Non-gas subdevices are logged once per channel and
// then ignored.
func (s *GridLoggingService) storeGasReadings(ctx context.Context, meterData domain.GridTelegram) int {
	if s.gasSink == nil {
		return 0
	}
	failed := 0
	for _, dev := range meterData.MBusDevices {
		if dev.DeviceType != domain.DeviceTypeGas {
			if !s.nonGasLogged[dev.Channel] {
				s.nonGasLogged[dev.Channel] = true
				s.logger.DebugContext(ctx, "ignoring non-gas M-Bus device",
					slog.Int("channel", dev.Channel),
					slog.Int("deviceType", dev.DeviceType),
				)
			}
			continue
		}
		if dev.Unit != gasUnitM3 {
			s.logger.WarnContext(ctx, "gas reading with unexpected unit, skipping",
				slog.Int("channel", dev.Channel),
				slog.String("unit", dev.Unit),
			)
			continue
		}
		if last, seen := s.lastGasCapture[dev.Channel]; seen && last.Equal(dev.CapturedAt) {
			continue
		}
		reading := domain.GasReading{
			CapturedAt: dev.CapturedAt,
			ReceivedAt: meterData.Time,
			Channel:    dev.Channel,
			DeviceType: dev.DeviceType,
			SerialNo:   dev.SerialNo,
			ReadingM3:  dev.Value,
		}
		err := withStoreTimeout(ctx, func(c context.Context) error {
			return s.gasSink.StoreGasReading(c, reading)
		})
		if err != nil {
			failed++
			s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "gas").Inc()
			s.logger.ErrorContext(ctx, "error storing gas reading",
				slog.Any("error", err),
				slog.Int("channel", dev.Channel),
			)
			continue
		}
		s.metrics.WritesTotal.WithLabelValues("multisink", "gas").Inc()
		s.lastGasCapture[dev.Channel] = dev.CapturedAt
	}
	return failed
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
