// CLAUDE GENERATED
// Package trace defines a small structured-event interface that
// orchestrator and runtime components emit through. The default
// implementation wraps log/slog; library-plan §6 reserves a seam for
// a Langfuse backend (Phase 7) without churning every callsite.
package trace

import (
	"context"
	"log/slog"
)

// Tracer emits structured events for the runtime. The context is
// passed through so future backends (Langfuse, OpenTelemetry) can
// pull trace IDs without a signature change. Level distinguishes
// routine events (Info) from operational concerns (Error); future
// non-slog backends are free to remap or ignore it.
type Tracer interface {
	Event(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr)
}

// Info is sugar for Event at slog.LevelInfo.
func Info(t Tracer, ctx context.Context, name string, attrs ...slog.Attr) {
	t.Event(ctx, slog.LevelInfo, name, attrs...)
}

// Error is sugar for Event at slog.LevelError.
func Error(t Tracer, ctx context.Context, name string, attrs ...slog.Attr) {
	t.Event(ctx, slog.LevelError, name, attrs...)
}

// Slog wraps a *slog.Logger as a Tracer. Zero value uses
// slog.Default().
type Slog struct {
	Logger *slog.Logger
}

// Event implements Tracer. Correlation pairs from ctx
// (WithCorrelation) are prepended to attrs so every emit downstream
// of a stamped scope carries its identifiers automatically — caller
// at each layer doesn't have to know about the originating bench
// case ID.
func (s Slog) Event(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	pairs := CorrelationFrom(ctx)
	if len(pairs) == 0 {
		logger.LogAttrs(ctx, level, name, attrs...)
		return
	}
	merged := make([]slog.Attr, 0, len(pairs)+len(attrs))
	for _, p := range pairs {
		merged = append(merged, slog.String(p.Key, p.Value))
	}
	merged = append(merged, attrs...)
	logger.LogAttrs(ctx, level, name, merged...)
}

// Discard drops every event. Useful in tests and benchmarks.
type Discard struct{}

// Event implements Tracer.
func (Discard) Event(context.Context, slog.Level, string, ...slog.Attr) {}

var (
	_ Tracer = Slog{}
	_ Tracer = Discard{}
)
