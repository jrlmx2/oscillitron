// CLAUDE GENERATED
// Package oscillator wraps an adapter as a brain-function-typed
// specialist. Long-lived per brain function; the adapter owns
// per-invocation session lifecycle internally (e.g., spinning up a
// fresh Hermes process seeded from the brain function's persistent
// memory store).
//
// Under the call-tree model the oscillator is invoked synchronously
// by the runner — there is no Run loop, no goroutine, no input/output
// channels. The runner walks the call tree by calling Invoke and
// recursing on Output.SubAPs.
package oscillator

import (
	"context"
	"log/slog"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// ID identifies a specialist instance. Distinct from BrainFunction
// (multiple instances may bind the same brain function later).
type ID string

// Oscillator is a brain-function-typed wrapper around an adapter.
type Oscillator struct {
	ID            ID
	BrainFunction session.BrainFunction
	Adapter       adapter.Adapter
	Tracer        trace.Tracer // optional; nil-safe
}

// New constructs an oscillator. Tracer may be nil.
func New(id ID, bf session.BrainFunction, a adapter.Adapter, tr trace.Tracer) *Oscillator {
	return &Oscillator{ID: id, BrainFunction: bf, Adapter: a, Tracer: tr}
}

// Invoke runs the AP synchronously and returns the envelope with
// Output populated. On adapter error, populates an ExitInhibited
// Output so the tree-walker surfaces the failure cleanly rather than
// dropping the AP.
func (o *Oscillator) Invoke(ctx context.Context, env session.Envelope) session.Envelope {
	start := time.Now()
	output, err := o.Adapter.Call(ctx, env)
	env.Trace.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		o.emitError(ctx, "oscillator_adapter_error",
			slog.String("oscillator", string(o.ID)),
			slog.String("brain_function", string(o.BrainFunction)),
			slog.String("adapter", o.Adapter.Name()),
			slog.String("err", err.Error()),
		)
		env.Output = &session.Output{
			ExitReason:     session.ExitInhibited,
			Content:        "adapter error: " + err.Error(),
			Confidence:     0,
			Contradictions: []string{"adapter call failed"},
		}
		return env
	}
	env.Output = &output
	o.emitInfo(ctx, "oscillator_invoked",
		slog.String("oscillator", string(o.ID)),
		slog.String("brain_function", string(o.BrainFunction)),
		slog.String("session", string(env.ID)),
		slog.String("exit", string(output.ExitReason)),
		slog.Float64("confidence", output.Confidence),
		slog.Int("subaps", len(output.SubAPs)),
		slog.Int64("duration_ms", env.Trace.DurationMs),
	)
	return env
}

func (o *Oscillator) emitInfo(ctx context.Context, name string, attrs ...slog.Attr) {
	if o.Tracer == nil {
		return
	}
	trace.Info(o.Tracer, ctx, name, attrs...)
}

func (o *Oscillator) emitError(ctx context.Context, name string, attrs ...slog.Attr) {
	if o.Tracer == nil {
		return
	}
	trace.Error(o.Tracer, ctx, name, attrs...)
}
