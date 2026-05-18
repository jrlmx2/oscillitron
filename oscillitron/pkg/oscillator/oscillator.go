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
	Logger  *slog.Logger // optional; nil-safe
}

// New constructs an oscillator. Logger may be nil.
func New(id ID, a adapter.Adapter, logger *slog.Logger) *Oscillator {
	return &Oscillator{ID: id, Adapter: a, Logger: logger}
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
		if o.Logger != nil {
			o.Logger.Error("oscillator adapter error",
				"oscillator", o.ID,
				"adapter", o.Adapter.Name(),
				"err", err,
			)
		}
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

	if o.Logger != nil {
		o.Logger.Info("oscillator emitted",
			"oscillator", o.ID,
			"session", env.ID,
			"exit", outcome.ExitReason,
			"confidence", outcome.Confidence,
			"duration_ms", env.Trace.DurationMs,
		)
	}

	return env
}
