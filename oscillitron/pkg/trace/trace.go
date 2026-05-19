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

// Event implements Tracer.
func (s Slog) Event(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, level, name, attrs...)
}

// Discard drops every event. Useful in tests and benchmarks.
type Discard struct{}

// Event implements Tracer.
func (Discard) Event(context.Context, slog.Level, string, ...slog.Attr) {}

var (
	_ Tracer = Slog{}
	_ Tracer = Discard{}
)
