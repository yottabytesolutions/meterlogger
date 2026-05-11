package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/yottabytesolutions/meterlogger/internal/debuglog"
	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

//nolint:gochecknoglobals // OTel tracer initialised at package level per OTel convention
var heatTracer = otel.Tracer("heat-meter-service")

const maxConsecutiveHeatErrors = 5

type HeatMeterLoggingService struct {
	source         domain.HeatMeterReader
	sink           domain.HeatMeterRepository
	interval       time.Duration
	flushInterval  time.Duration
	logger         *slog.Logger
	metrics        *metrics.Metrics
	dataFlowLogged bool
}

func NewHeatMeterLoggingService(
	source domain.HeatMeterReader,
	sink domain.HeatMeterRepository,
	interval time.Duration,
	flushInterval time.Duration,
	logger *slog.Logger,
) *HeatMeterLoggingService {
	return &HeatMeterLoggingService{
		source:        source,
		sink:          sink,
		interval:      interval,
		flushInterval: flushInterval,
		logger:        logger,
	}
}

// WithMetrics attaches Prometheus metrics to the service.
func (s *HeatMeterLoggingService) WithMetrics(m *metrics.Metrics) *HeatMeterLoggingService {
	s.metrics = m
	return s
}

func (s *HeatMeterLoggingService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	flushTicker := time.NewTicker(s.flushInterval)
	defer flushTicker.Stop()
	consecutiveErrors := 0
	for {
		select {
		case <-ticker.C:
			s.logger.DebugContext(ctx, "heat meter tick: reading")
			if stop := s.handleTick(ctx, &consecutiveErrors); stop {
				return
			}
		case <-flushTicker.C:
			s.logger.DebugContext(ctx, "flushing heat meter data")
			if err := s.sink.Flush(ctx); err != nil {
				s.logger.ErrorContext(ctx, "error flushing meter data", slog.Any("error", err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// handleTick runs one read-and-store cycle. Returns true if the service should stop.
func (s *HeatMeterLoggingService) handleTick(ctx context.Context, consecutiveErrors *int) bool {
	err := s.runReadAndStore(ctx)
	if err == nil {
		*consecutiveErrors = 0
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	*consecutiveErrors++
	s.logger.ErrorContext(ctx, "error reading meter data",
		slog.Any("error", err),
		slog.Int("consecutiveErrors", *consecutiveErrors),
	)
	if s.metrics != nil {
		s.metrics.ReadErrorsTotal.WithLabelValues("heat").Inc()
	}
	if *consecutiveErrors >= maxConsecutiveHeatErrors {
		s.logger.ErrorContext(ctx, "heat meter: too many consecutive errors, terminating")
		processKiller()
		return true
	}
	return false
}

func (s *HeatMeterLoggingService) runReadAndStore(ctx context.Context) error {
	ctx, span := heatTracer.Start(ctx, "ReadAndStore")
	defer span.End()

	meterData, err := s.source.ReadHeatTelegram(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "read heat telegram failed")
		return err
	}

	if s.metrics != nil {
		s.metrics.ReadsTotal.WithLabelValues("heat").Inc()
		s.metrics.LastReadTime.WithLabelValues("heat").SetToCurrentTime()
	}

	s.logger.DebugContext(ctx, "heat telegram received, storing", debuglog.HeatAttrs(meterData))
	if storeErr := s.sink.StoreHeatTelegram(ctx, meterData); storeErr != nil {
		span.RecordError(storeErr)
		span.SetStatus(codes.Error, "store heat telegram failed")
		if s.metrics != nil {
			s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "heat").Inc()
		}
		return storeErr
	}
	if s.metrics != nil {
		s.metrics.WritesTotal.WithLabelValues("multisink", "heat").Inc()
	}
	if !s.dataFlowLogged {
		s.logger.InfoContext(ctx, "heat meter data flow started successfully", debuglog.HeatAttrs(meterData))
		s.dataFlowLogged = true
	}
	return nil
}
