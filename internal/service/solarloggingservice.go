package service

import (
	"context"
	"errors"
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
	source        domain.EnvoySolarReader
	sink          domain.EnvoySolarRepository
	interval      time.Duration
	flushInterval time.Duration
	logger        *slog.Logger
	metrics       *metrics.Metrics
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
	for {
		select {
		case <-flushTicker.C:
			err := s.sink.Flush(ctx)
			if err != nil {
				s.logger.ErrorContext(ctx, "error flushing envoy data", slog.Any("error", err))
			}
		case <-ticker.C:
			err := s.runReadAndStore(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				s.logger.ErrorContext(ctx, "error processing envoy data", slog.Any("error", err))
				if s.metrics != nil {
					s.metrics.ReadErrorsTotal.WithLabelValues("solar").Inc()
				}
			}
		case <-ctx.Done():
			return
		}
	}
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

	if s.metrics != nil {
		s.metrics.ReadsTotal.WithLabelValues("solar").Inc()
		s.metrics.LastReadTime.WithLabelValues("solar").SetToCurrentTime()
	}

	if storeErr := s.sink.StoreEnvoySolarData(ctx, meterData); storeErr != nil {
		span.RecordError(storeErr)
		span.SetStatus(codes.Error, "store solar data failed")
		if s.metrics != nil {
			s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "solar").Inc()
		}
		return storeErr
	}
	if s.metrics != nil {
		s.metrics.WritesTotal.WithLabelValues("multisink", "solar").Inc()
	}
	return nil
}
