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
)

// ID identifies a specialist instance. Distinct from BrainFunction
// (multiple instances may bind the same brain function later).
type ID string

// Oscillator is a brain-function-typed wrapper around an adapter.
type Oscillator struct {
	ID            ID
	BrainFunction session.BrainFunction
	Adapter       adapter.Adapter
	Logger        *slog.Logger // optional; nil-safe
}

// New constructs an oscillator. Logger may be nil.
func New(id ID, bf session.BrainFunction, a adapter.Adapter, logger *slog.Logger) *Oscillator {
	return &Oscillator{ID: id, BrainFunction: bf, Adapter: a, Logger: logger}
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
		if o.Logger != nil {
			o.Logger.Error("oscillator adapter error",
				"oscillator", o.ID,
				"brain_function", o.BrainFunction,
				"adapter", o.Adapter.Name(),
				"err", err)
		}
		env.Output = &session.Output{
			ExitReason:     session.ExitInhibited,
			Content:        "adapter error: " + err.Error(),
			Confidence:     0,
			Contradictions: []string{"adapter call failed"},
		}
		return env
	}
	env.Output = &output
	if o.Logger != nil {
		o.Logger.Info("oscillator invoked",
			"oscillator", o.ID,
			"brain_function", o.BrainFunction,
			"session", env.ID,
			"exit", output.ExitReason,
			"confidence", output.Confidence,
			"subaps", len(output.SubAPs),
			"duration_ms", env.Trace.DurationMs)
	}
	return env
}
