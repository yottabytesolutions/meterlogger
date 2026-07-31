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

type GridLoggingService struct {
	source         domain.GridTelegramReader
	sink           domain.GridTelegramRepository
	flushInterval  time.Duration
	logger         *slog.Logger
	metrics        *metrics.Metrics
	dataFlowLogged bool
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
	}
}

// WithMetrics attaches Prometheus metrics to the service.
func (s *GridLoggingService) WithMetrics(m *metrics.Metrics) *GridLoggingService {
	s.metrics = m
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
			s.logger.DebugContext(ctx, "flushing grid meter data")
			err := withStoreTimeout(ctx, s.sink.Flush)
			if err != nil {
				s.logger.ErrorContext(ctx, "error flushing grid meter data", slog.Any("error", err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// handleStore stores one grid telegram and escalates on repeated failures.
// Returns true if the service should stop.
func (s *GridLoggingService) handleStore(
	ctx context.Context, meterData domain.GridTelegram, consecutiveErrors *int,
) bool {
	err := s.storeData(ctx, meterData)
	if err == nil {
		*consecutiveErrors = 0
		return false
	}
	*consecutiveErrors++
	s.logger.ErrorContext(ctx, "error storing grid meter data",
		slog.Any("error", err),
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
