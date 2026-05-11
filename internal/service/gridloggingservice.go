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
	resultChannel  chan domain.GridTelegram
	logger         *slog.Logger
	metrics        *metrics.Metrics
	dataFlowLogged bool
}

func NewGridLoggingService(
	source domain.GridTelegramReader,
	sink domain.GridTelegramRepository,
	flushInterval time.Duration,
	resultChannel chan domain.GridTelegram,
	logger *slog.Logger,
) *GridLoggingService {
	return &GridLoggingService{
		source:        source,
		sink:          sink,
		flushInterval: flushInterval,
		resultChannel: resultChannel,
		logger:        logger,
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
			s.logger.Error("error reading grid meter data", slog.Any("error", err))
			processKiller()
		}
	})
	defer wg.Wait()

	for {
		select {
		case meterData := <-s.resultChannel:
			if s.metrics != nil {
				s.metrics.ReadsTotal.WithLabelValues("grid").Inc()
				s.metrics.LastReadTime.WithLabelValues("grid").SetToCurrentTime()
			}
			_ = s.storeData(ctx, meterData)
		case <-flushTicker.C:
			s.logger.DebugContext(ctx, "flushing grid meter data")
			err := s.sink.Flush(ctx)
			if err != nil {
				s.logger.ErrorContext(ctx, "error flushing grid meter data", slog.Any("error", err))
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *GridLoggingService) storeData(ctx context.Context, meterData domain.GridTelegram) error {
	ctx, span := gridTracer.Start(ctx, "StoreData")
	defer span.End()

	s.logger.DebugContext(ctx, "grid telegram received, storing", debuglog.GridAttrs(meterData))
	if err := s.sink.StoreGridTelegram(ctx, meterData); err != nil {
		s.logger.ErrorContext(ctx, "error storing grid meter data", slog.Any("error", err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "store grid telegram failed")
		if s.metrics != nil {
			s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "grid").Inc()
		}
		return err
	}
	if s.metrics != nil {
		s.metrics.WritesTotal.WithLabelValues("multisink", "grid").Inc()
	}
	if !s.dataFlowLogged {
		s.logger.InfoContext(ctx, "grid meter data flow started successfully", debuglog.GridAttrs(meterData))
		s.dataFlowLogged = true
	}
	return nil
}
