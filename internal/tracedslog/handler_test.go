package tracedslog_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/yottabytesolutions/meterlogger/internal/tracedslog"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	base := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(tracedslog.New(base))
}

func TestHandler_NoSpan_NoTraceAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)
	log.InfoContext(context.Background(), "hello")
	out := buf.String()
	if strings.Contains(out, "traceID") {
		t.Errorf("expected no traceID without active span, got: %s", out)
	}
}

func TestHandler_ActiveSpan_InjectsTraceAttrs(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exp))
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	var buf bytes.Buffer
	log := newTestLogger(&buf)
	log.InfoContext(ctx, "with span")
	out := buf.String()

	if !strings.Contains(out, "traceID") {
		t.Errorf("expected traceID in log output, got: %s", out)
	}
	if !strings.Contains(out, "spanID") {
		t.Errorf("expected spanID in log output, got: %s", out)
	}
}

func TestHandler_Enabled_Delegates(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := tracedslog.New(base)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Debug to be disabled when base level is Warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("expected Error to be enabled when base level is Warn")
	}
}

func TestHandler_WithAttrs_Propagates(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf).With(slog.String("key", "value"))
	log.InfoContext(context.Background(), "msg")
	if !strings.Contains(buf.String(), "key=value") {
		t.Errorf("expected key=value in output, got: %s", buf.String())
	}
}

func TestHandler_WithGroup_Propagates(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf).WithGroup("grp")
	log.InfoContext(context.Background(), "msg", slog.String("k", "v"))
	if !strings.Contains(buf.String(), "grp.k=v") {
		t.Errorf("expected grp.k=v in output, got: %s", buf.String())
	}
}
