// Package tracedslog provides a slog.Handler wrapper that injects OTel trace
// and span IDs into every log record when a span is active.
package tracedslog

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// Handler wraps a base slog.Handler and adds "traceID" and "spanID" attributes
// when the context carries an active OTel span.
type Handler struct {
	base slog.Handler
}

// New returns a Handler that delegates to base.
func New(base slog.Handler) *Handler {
	return &Handler{base: base}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("traceID", sc.TraceID().String()),
			slog.String("spanID", sc.SpanID().String()),
		)
	}
	return h.base.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{base: h.base.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{base: h.base.WithGroup(name)}
}
