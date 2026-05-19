// CLAUDE GENERATED
// Package oscillator wraps an adapter behind an ID and a goroutine
// that reads envelopes off an input channel, dispatches them to the
// adapter, and forwards the resulting envelope (with Outcome
// populated) to a shared output channel as an Emission.
//
// The oscillator is deliberately dumb: it does not decide where to
// route the result. That's the router's job, applied by the runner
// after the oscillator emits.
package oscillator

import (
	"context"
	"log/slog"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// ID identifies an oscillator within a topology.
type ID string

// Emission pairs an envelope with the oscillator that produced it.
// The runner reads Emissions from a shared output channel; the From
// field tells the router which graph node to look up outgoing edges
// from.
type Emission struct {
	From     ID
	Envelope session.Envelope
}

// Oscillator is a specialist node — adapter + identity + goroutine.
type Oscillator struct {
	ID      ID
	Adapter adapter.Adapter
	Tracer  trace.Tracer
}

// New constructs an oscillator. Accepts either a *slog.Logger (wrapped
// in trace.Slog) or any trace.Tracer; a nil tracer is replaced with
// trace.Discard so the dispatch loop never has to nil-check.
func New(id ID, a adapter.Adapter, logger *slog.Logger) *Oscillator {
	var t trace.Tracer = trace.Discard{}
	if logger != nil {
		t = trace.Slog{Logger: logger}
	}
	return &Oscillator{ID: id, Adapter: a, Tracer: t}
}

// NewWithTracer constructs an oscillator with an arbitrary Tracer.
// Prefer this over New for non-slog backends (Langfuse, OTel) once
// they land. nil tracer is replaced with trace.Discard.
func NewWithTracer(id ID, a adapter.Adapter, t trace.Tracer) *Oscillator {
	if t == nil {
		t = trace.Discard{}
	}
	return &Oscillator{ID: id, Adapter: a, Tracer: t}
}

// Run is the oscillator's main loop. It blocks until in is closed or
// ctx is cancelled. Each inbound envelope is dispatched to the adapter
// and the result (with Outcome and Routing populated) is sent to out
// as an Emission.
//
// Run does not own in or out; the caller (the runner) creates and
// closes them.
func (o *Oscillator) Run(ctx context.Context, in <-chan session.Envelope, out chan<- Emission) {
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-in:
			if !ok {
				return
			}
			result := o.dispatch(ctx, env)
			select {
			case <-ctx.Done():
				return
			case out <- Emission{From: o.ID, Envelope: result}:
			}
		}
	}
}

func (o *Oscillator) dispatch(ctx context.Context, env session.Envelope) session.Envelope {
	start := time.Now()
	outcome, err := o.Adapter.Call(ctx, env)
	dur := time.Since(start)

	// Always stamp routing + duration so the chain can be inspected
	// even when the adapter errored.
	env.Routing.Model = o.Adapter.Name()
	env.Routing.Reason = "handled by " + string(o.ID)
	env.Trace.DurationMs = dur.Milliseconds()

	if err != nil {
		o.Tracer.Event(ctx, "oscillator.adapter_error",
			slog.String("oscillator", string(o.ID)),
			slog.String("adapter", o.Adapter.Name()),
			slog.String("err", err.Error()),
		)
		// Surface the error as an inhibited Outcome so the chain
		// terminates cleanly rather than disappearing.
		env.Outcome = &session.Outcome{
			ExitReason:     session.ExitInhibited,
			Verdict:        "adapter error: " + err.Error(),
			Confidence:     0,
			Contradictions: []string{"adapter call failed"},
		}
		return env
	}

	env.Outcome = &outcome
	// Copy usage into Trace so downstream layers (cost tracker, eval
	// harness) don't have to peek at Outcome. Adapters that don't report
	// usage leave these at zero.
	env.Trace.TokensInput = outcome.TokensInput
	env.Trace.TokensOutput = outcome.TokensOutput

	o.Tracer.Event(ctx, "oscillator.emitted",
		slog.String("oscillator", string(o.ID)),
		slog.String("session", string(env.ID)),
		slog.String("exit", string(outcome.ExitReason)),
		slog.Float64("confidence", outcome.Confidence),
		slog.Int64("duration_ms", env.Trace.DurationMs),
	)

	return env
}
