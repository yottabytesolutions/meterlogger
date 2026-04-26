package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	"github.com/yottabytesolutions/meterlogger/internal/domain"
	"github.com/yottabytesolutions/meterlogger/internal/metrics"
)

//nolint:gochecknoglobals // OTel tracer initialised at package level per OTel convention
var ducoTracer = otel.Tracer("duco-service")

const maxErrorCount = 20

type DucoLoggingService struct {
	source        domain.DucoReader
	sink          domain.DucoRepository
	interval      time.Duration
	flushInterval time.Duration
	logger        *slog.Logger
	nodes         []int
	metrics       *metrics.Metrics
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
	for {
		select {
		case <-flushTicker.C:
			if err := s.sink.Flush(ctx); err != nil {
				s.logger.ErrorContext(ctx, "error flushing data", slog.Any("error", err))
				processKiller()
				return
			}
		case <-ticker.C:
			if err := s.runReadAndStore(ctx); err != nil {
				if errorCounter >= maxErrorCount {
					s.logger.ErrorContext(ctx, "too many errors, stopping service")
					processKiller()
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

//nolint:gocognit // complexity is inherent to the multi-node ventilation service loop
func (s *DucoLoggingService) runReadAndStore(ctx context.Context) error {
	ctx, span := ducoTracer.Start(ctx, "ReadAndStore")
	defer span.End()

	boxStatus, err := s.source.ReadBoxStatus(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "error reading box status", slog.Any("error", err))
		span.RecordError(err)
		span.SetStatus(codes.Error, "read box status failed")
		if s.metrics != nil {
			s.metrics.ReadErrorsTotal.WithLabelValues("ventilation").Inc()
		}
		return err
	}

	if s.metrics != nil {
		s.metrics.ReadsTotal.WithLabelValues("ventilation").Inc()
		s.metrics.LastReadTime.WithLabelValues("ventilation").SetToCurrentTime()
	}

	if storeErr := s.sink.StoreBoxStatus(ctx, boxStatus); storeErr != nil {
		s.logger.ErrorContext(ctx, "error storing box status", slog.Any("error", storeErr))
		span.RecordError(storeErr)
		span.SetStatus(codes.Error, "store box status failed")
		if s.metrics != nil {
			s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "ventilation").Inc()
		}
		processKiller()
		return storeErr
	}
	if s.metrics != nil {
		s.metrics.WritesTotal.WithLabelValues("multisink", "ventilation").Inc()
	}

	for _, nodeID := range s.nodes {
		nodeData, nodeErr := s.source.ReadNodeStatus(ctx, nodeID)
		if nodeErr != nil {
			if !strings.HasSuffix(nodeErr.Error(), "UNKN") {
				s.logger.ErrorContext(
					ctx, "error reading node data",
					slog.Int("nodeID", nodeID), slog.Any("error", nodeErr),
				)
			}
			continue
		}
		if storeErr := s.sink.StoreNodeData(ctx, nodeData); storeErr != nil {
			s.logger.ErrorContext(
				ctx, "error storing node data",
				slog.Int("nodeID", nodeID), slog.Any("error", storeErr),
			)
			span.RecordError(storeErr)
			span.SetStatus(codes.Error, "store node data failed")
			if s.metrics != nil {
				s.metrics.WriteErrorsTotal.WithLabelValues("multisink", "ventilation").Inc()
			}
			processKiller()
			return storeErr
		}
		if s.metrics != nil {
			s.metrics.WritesTotal.WithLabelValues("multisink", "ventilation").Inc()
		}
	}
	return nil
}
