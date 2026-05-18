// CLAUDE GENERATED
// Package trace defines a small structured-event interface that
// orchestrator and runtime components emit through. The default
// implementation wraps log/slog; library-plan §6 reserves a seam for
// a Langfuse backend (Phase 7) without churning every callsite.
//
// Scaffold-only for now: existing callers (oscillator, runner,
// cmd/oscillitron) still call slog directly. New code should reach
// for trace.Tracer instead so the migration can happen package by
// package.
package trace

import (
	"context"
	"log/slog"
)

// Tracer emits structured events for the runtime. The context is
// passed through so future backends (Langfuse, OpenTelemetry) can
// pull trace IDs without a signature change.
type Tracer interface {
	Event(ctx context.Context, name string, attrs ...slog.Attr)
}

// Slog wraps a *slog.Logger as a Tracer. Zero value uses
// slog.Default().
type Slog struct {
	Logger *slog.Logger
}

// Event implements Tracer.
func (s Slog) Event(ctx context.Context, name string, attrs ...slog.Attr) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.LogAttrs(ctx, slog.LevelInfo, name, attrs...)
}

// Discard drops every event. Useful in tests and benchmarks.
type Discard struct{}

// Event implements Tracer.
func (Discard) Event(context.Context, string, ...slog.Attr) {}

var (
	_ Tracer = Slog{}
	_ Tracer = Discard{}
)
