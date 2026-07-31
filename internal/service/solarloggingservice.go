package service

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

//nolint:gochecknoglobals // OTel tracer initialised at package level per OTel convention
var solarTracer = otel.Tracer("solar-meter-service")

type SolarLoggingService struct {
	source         domain.EnvoySolarReader
	sink           domain.EnvoySolarRepository
	interval       time.Duration
	flushInterval  time.Duration
	logger         *slog.Logger
	metrics        *metrics.Metrics
	dataFlowLogged bool
}

func NewSolarLoggingService(
	source domain.EnvoySolarReader,
	sink domain.EnvoySolarRepository,
	interval time.Duration,
	flushInterval time.Duration,
	logger *slog.Logger,
) *SolarLoggingService {
	return &SolarLoggingService{
		source:        source,
		sink:          sink,
		interval:      interval,
		flushInterval: flushInterval,
		logger:        logger,
		metrics:       metrics.NewNoop(),
	}
}

// WithMetrics attaches Prometheus metrics to the service.
func (s *SolarLoggingService) WithMetrics(m *metrics.Metrics) *SolarLoggingService {
	s.metrics = m
	return s
}

func (s *SolarLoggingService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	flushTicker := time.NewTicker(s.flushInterval)
	defer flushTicker.Stop()
	consecutiveErrors := 0
	for {
		select {
		case <-flushTicker.C:
			err := withStoreTimeout(ctx, s.sink.Flush)
			if err != nil {
				s.logger.ErrorContext(ctx, "error flushing envoy data", slog.Any("error", err))
			}
		case <-ticker.C:
			if stop := s.handleTick(ctx, &consecutiveErrors); stop {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// handleTick runs one read-and-store cycle. Returns true if the service should stop.
func (s *SolarLoggingService) handleTick(ctx context.Context, consecutiveErrors *int) bool {
	err := s.runReadAndStore(ctx)
	if err == nil {
		*consecutiveErrors = 0
		return false
	}
	// Only a cancelled parent context means shutdown. A DeadlineExceeded from
	// a store timeout must count towards the error threshold.
	if ctx.Err() != nil {
		return true
	}
	*consecutiveErrors++
	s.logger.ErrorContext(ctx, "error processing envoy data",
		slog.Any("error", err),
		slog.Int("consecutiveErrors", *consecutiveErrors),
	)
	s.metrics.ReadErrorsTotal.WithLabelValues("solar").Inc()
	if *consecutiveErrors >= maxConsecutiveErrors {
		s.logger.ErrorContext(ctx, "solar: too many consecutive errors, terminating")
		processKiller()
		<-ctx.Done()
		return true
	}
	return false
}

func (s *SolarLoggingService) runReadAndStore(ctx context.Context) error {
	ctx, span := solarTracer.Start(ctx, "ReadAndStore")
	defer span.End()

	meterData, err := s.source.ReadEnvoySolarData(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "read solar data failed")
		return err
	}

	s.metrics.ReadsTotal.WithLabelValues("solar").Inc()
	s.metrics.LastReadTime.WithLabelValues("solar").SetToCurrentTime()

	storeErr := withStoreTimeout(ctx, func(c context.Context) error {
		return s.sink.StoreEnvoySolarData(c, meterData)
	})
	if storeErr != nil {
		span.RecordError(storeErr)
		span.SetStatus(codes.Error, "store solar data failed")
		s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "solar").Inc()
		return storeErr
	}
	s.metrics.WritesTotal.WithLabelValues("multisink", "solar").Inc()
	if !s.dataFlowLogged {
		s.logger.InfoContext(ctx, "solar data flow started successfully")
		s.dataFlowLogged = true
	}
	return nil
}
