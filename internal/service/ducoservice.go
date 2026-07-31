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
var ducoTracer = otel.Tracer("duco-service")

// maxErrorCount is the duco-specific threshold for consecutive read-and-store
// cycle failures. It is more tolerant than maxConsecutiveErrors because the
// DucoBox HTTP API is flaky on home networks.
const maxErrorCount = 20

type DucoLoggingService struct {
	source         domain.DucoReader
	sink           domain.DucoRepository
	interval       time.Duration
	flushInterval  time.Duration
	logger         *slog.Logger
	nodes          []int
	metrics        *metrics.Metrics
	dataFlowLogged bool
}

func NewDucoLoggingService(
	source domain.DucoReader,
	sink domain.DucoRepository,
	interval time.Duration,
	flushInterval time.Duration,
	nodes []int,
	logger *slog.Logger,
) *DucoLoggingService {
	return &DucoLoggingService{
		source:        source,
		sink:          sink,
		interval:      interval,
		flushInterval: flushInterval,
		logger:        logger,
		nodes:         nodes,
		metrics:       metrics.NewNoop(),
	}
}

// WithMetrics attaches Prometheus metrics to the service.
func (s *DucoLoggingService) WithMetrics(m *metrics.Metrics) *DucoLoggingService {
	s.metrics = m
	return s
}

func (s *DucoLoggingService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	flushTicker := time.NewTicker(s.flushInterval)
	defer flushTicker.Stop()
	errorCounter := 0
	flushErrors := 0
	for {
		select {
		case <-flushTicker.C:
			if stop := s.handleFlush(ctx, &flushErrors); stop {
				return
			}
		case <-ticker.C:
			if err := s.runReadAndStore(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				if errorCounter >= maxErrorCount {
					s.logger.ErrorContext(ctx, "too many errors, stopping service")
					processKiller()
					<-ctx.Done()
					return
				}
				errorCounter++
				continue
			}
			errorCounter = 0
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "DucoLoggingService stopping due to context cancellation")
			return
		}
	}
}

// handleFlush flushes the sink and escalates after maxConsecutiveErrors
// consecutive failures. Returns true if the service should stop.
func (s *DucoLoggingService) handleFlush(ctx context.Context, flushErrors *int) bool {
	if err := withStoreTimeout(ctx, s.sink.Flush); err != nil {
		*flushErrors++
		s.logger.ErrorContext(ctx, "error flushing data",
			slog.Any("error", err),
			slog.Int("consecutiveErrors", *flushErrors),
		)
		if *flushErrors >= maxConsecutiveErrors {
			s.logger.ErrorContext(ctx, "duco: too many consecutive flush errors, terminating")
			processKiller()
			<-ctx.Done()
			return true
		}
		return false
	}
	*flushErrors = 0
	return false
}

func (s *DucoLoggingService) runReadAndStore(ctx context.Context) error {
	ctx, span := ducoTracer.Start(ctx, "ReadAndStore")
	defer span.End()

	boxStatus, err := s.source.ReadBoxStatus(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "error reading box status", slog.Any("error", err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "read box status failed")
		s.metrics.ReadErrorsTotal.WithLabelValues("ventilation").Inc()
		return err
	}

	s.metrics.ReadsTotal.WithLabelValues("ventilation").Inc()
	s.metrics.LastReadTime.WithLabelValues("ventilation").SetToCurrentTime()

	storeErr := withStoreTimeout(ctx, func(c context.Context) error {
		return s.sink.StoreBoxStatus(c, boxStatus)
	})
	if storeErr != nil {
		s.logger.ErrorContext(ctx, "error storing box status", slog.Any("error", storeErr))
		span.RecordError(storeErr)
		span.SetStatus(codes.Error, "store box status failed")
		s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "ventilation").Inc()
		return storeErr
	}
	s.metrics.WritesTotal.WithLabelValues("multisink", "ventilation").Inc()
	if !s.dataFlowLogged {
		s.logger.InfoContext(ctx, "ventilation data flow started successfully")
		s.dataFlowLogged = true
	}

	for _, nodeID := range s.nodes {
		nodeData, nodeErr := s.source.ReadNodeStatus(ctx, nodeID)
		if nodeErr != nil {
			if !errors.Is(nodeErr, domain.ErrUnknownDevType) {
				s.logger.ErrorContext(
					ctx, "error reading node data",
					slog.Int("nodeID", nodeID), slog.Any("error", nodeErr),
				)
			}
			continue
		}
		storeNodeErr := withStoreTimeout(ctx, func(c context.Context) error {
			return s.sink.StoreNodeData(c, nodeData)
		})
		if storeNodeErr != nil {
			s.logger.ErrorContext(
				ctx, "error storing node data",
				slog.Int("nodeID", nodeID), slog.Any("error", storeNodeErr),
			)
			span.RecordError(storeNodeErr)
			span.SetStatus(codes.Error, "store node data failed")
			s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "ventilation").Inc()
			return storeNodeErr
		}
		s.metrics.WritesTotal.WithLabelValues("multisink", "ventilation").Inc()
	}
	return nil
}
