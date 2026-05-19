// CLAUDE GENERATED
// Package runner ties oscillators, topology, router, and inhibitor
// into a single chain-driving loop. Fire one AP at the topology
// entry; the runner shepherds it through the graph until a terminal
// state, an inhibitor abort, or context cancellation.
//
// This is the v0.2-architecture orchestrator (per library-plan §3.2
// after the wrap-Hermes lock). The earlier library-plan §3.2
// "orchestrator package" was a fan-out/fan-in batch runner for a
// single Oscillation; that batch runner is still planned but is a
// different thing from this chain-spike loop.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Config bundles the runner's dependencies.
type Config struct {
	Topology    *topology.Topology
	Oscillators map[oscillator.ID]*oscillator.Oscillator
	Router      router.Router
	Inhibitor   inhibitor.Inhibitor
	Initial     session.Envelope
	// Logger is a convenience: if non-nil it's wrapped in trace.Slog
	// and used as the runner's Tracer. Tracer takes precedence if both
	// are set.
	Logger *slog.Logger
	Tracer trace.Tracer
	// Tracker, if non-nil, records every emission's token usage and
	// stamps Trace.CostUSD / Trace.CostVsFrontierBaselineUSD on the
	// envelope before it's appended to the chain. Models without
	// registered pricing record at ActualUSD=0 (still computes the
	// frontier counterfactual).
	Tracker *cost.Tracker
	// BufferSize is the channel buffer per oscillator. Defaults to 8.
	// Externalized via OSCILLITRON_RUNNER_BUFFER_SIZE / --runner-buffer-size.
	BufferSize int
	// ChainTimeout, if non-zero, wraps the caller's ctx with a deadline.
	// Externalized via OSCILLITRON_RUNNER_CHAIN_TIMEOUT (a Go duration
	// string like "30s", "5m") / --runner-chain-timeout. Zero leaves
	// the caller's ctx unwrapped.
	ChainTimeout time.Duration
}

// Reason describes why the runner stopped.
type Reason string

const (
	ReasonTerminalOutcome Reason = "terminal_outcome" // envelope.IsTerminal()
	ReasonRouterTerminal  Reason = "router_terminal"  // router returned Terminal=true
	ReasonInhibitorAbort  Reason = "inhibitor_abort"  // inhibitor said Abort
	ReasonContextDone     Reason = "context_done"     // ctx.Done() fired
)

// Result is what the runner returns after the chain settles.
type Result struct {
	Chain  []session.Envelope
	Reason Reason
	Detail string
}

// Run executes the chain. It returns when the chain reaches a
// terminal state, the inhibitor aborts, or the context is cancelled.
//
// Validation: every node in cfg.Topology must have a corresponding
// entry in cfg.Oscillators, and cfg.Topology.Entry() must be present
// in both. Mismatches are returned as errors before any goroutine
// starts.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := validate(cfg); err != nil {
		return Result{}, err
	}

	bufSize := cfg.BufferSize
	if bufSize <= 0 {
		bufSize = 8
	}

	tracer := cfg.Tracer
	if tracer == nil {
		if cfg.Logger != nil {
			tracer = trace.Slog{Logger: cfg.Logger}
		} else {
			tracer = trace.Discard{}
		}
	}

	// Advisory: warn when ChainTimeout is shorter than any adapter's
	// declared MinCallTimeout. The per-adapter floor is still the hard
	// guard at Call entry; this is just so the user knows their chain
	// timeout setting will trigger inhibition on every call against
	// this backend.
	if cfg.ChainTimeout > 0 {
		for id, osc := range cfg.Oscillators {
			adv, ok := osc.Adapter.(adapter.MinTimeoutAdvisory)
			if !ok {
				continue
			}
			min := adv.MinCallTimeout()
			if min > 0 && cfg.ChainTimeout < min {
				tracer.Event(ctx, "runner.chain_timeout_below_adapter_floor",
					slog.String("oscillator", string(id)),
					slog.String("adapter", osc.Adapter.Name()),
					slog.Duration("chain_timeout", cfg.ChainTimeout),
					slog.Duration("adapter_min_call_timeout", min),
				)
			}
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if cfg.ChainTimeout > 0 {
		var timeoutCancel context.CancelFunc
		runCtx, timeoutCancel = context.WithTimeout(runCtx, cfg.ChainTimeout)
		defer timeoutCancel()
	}

	// Wire up: one input channel per oscillator, one shared output channel.
	inputs := make(map[oscillator.ID]chan session.Envelope, len(cfg.Oscillators))
	for id := range cfg.Oscillators {
		inputs[id] = make(chan session.Envelope, bufSize)
	}
	shared := make(chan oscillator.Emission, bufSize)

	// Spawn oscillator goroutines under a WaitGroup so we can clean
	// up on the way out.
	var wg sync.WaitGroup
	for id, osc := range cfg.Oscillators {
		wg.Add(1)
		go func(id oscillator.ID, osc *oscillator.Oscillator) {
			defer wg.Done()
			osc.Run(runCtx, inputs[id], shared)
		}(id, osc)
	}

	// Fire the initial AP at the entry oscillator.
	entry := cfg.Topology.Entry()
	if err := send(runCtx, inputs[entry], cfg.Initial); err != nil {
		cancel()
		closeInputs(inputs)
		wg.Wait()
		return Result{}, fmt.Errorf("runner: failed to fire initial AP: %w", err)
	}

	chain := make([]session.Envelope, 0, 16)
	result := Result{}

drainLoop:
	for {
		select {
		case <-runCtx.Done():
			result = Result{Chain: chain, Reason: ReasonContextDone, Detail: runCtx.Err().Error()}
			break drainLoop
		case em := <-shared:
			env := em.Envelope
			if cfg.Tracker != nil && env.Outcome != nil {
				entry := cfg.Tracker.Record(
					env.Routing.Model,
					env.Outcome.TokensInput,
					env.Outcome.TokensOutput,
				)
				env.Trace.CostUSD = entry.ActualUSD
				env.Trace.CostVsFrontierBaselineUSD = entry.FrontierUSD
			}
			chain = append(chain, env)
			// From here on the loop reads em.Envelope for routing/inhibition;
			// keep that view in sync with the cost-stamped env.
			em.Envelope = env

			if v := cfg.Inhibitor.Check(chain); v.Decision != inhibitor.Continue {
				// Restart isn't wired yet (no checkpointing); per
				// CLAUDE.md inhibitor.go, v0 callers may treat Restart
				// as Abort. Annotate the reason so the log makes the
				// downgrade visible.
				detail := v.Reason
				if v.Decision == inhibitor.Restart {
					detail = "restart-as-abort (no checkpointing yet): " + v.Reason
				}
				result = Result{Chain: chain, Reason: ReasonInhibitorAbort, Detail: detail}
				tracer.Event(runCtx, "runner.inhibitor_abort",
					slog.Int("decision", int(v.Decision)),
					slog.String("reason", v.Reason),
					slog.Int("chain_len", len(chain)),
				)
				break drainLoop
			}

			if em.Envelope.IsTerminal() {
				result = Result{Chain: chain, Reason: ReasonTerminalOutcome,
					Detail: fmt.Sprintf("exit_reason=%s from %s", em.Envelope.Outcome.ExitReason, em.From)}
				break drainLoop
			}

			dec, err := cfg.Router.Route(em.From, em.Envelope, cfg.Topology)
			if err != nil {
				result = Result{Chain: chain, Reason: ReasonRouterTerminal,
					Detail: fmt.Sprintf("router error: %v", err)}
				break drainLoop
			}
			if dec.Terminal {
				result = Result{Chain: chain, Reason: ReasonRouterTerminal, Detail: dec.Reason}
				tracer.Event(runCtx, "runner.router_terminal",
					slog.String("reason", dec.Reason),
					slog.Int("chain_len", len(chain)),
				)
				break drainLoop
			}
			// Defensive: a router that returns Terminal=false with no
			// destinations would deadlock the runner. Treat as terminal.
			if len(dec.Destinations) == 0 {
				result = Result{Chain: chain, Reason: ReasonRouterTerminal,
					Detail: "router returned no destinations and Terminal=false"}
				break drainLoop
			}

			for _, dest := range dec.Destinations {
				ch, ok := inputs[dest.To]
				if !ok {
					// Destination not wired — treat as terminal to avoid
					// silently dropping the AP.
					result = Result{Chain: chain, Reason: ReasonRouterTerminal,
						Detail: fmt.Sprintf("destination %q not in oscillator map", dest.To)}
					tracer.Event(runCtx, "runner.unwired_destination",
						slog.String("dest", string(dest.To)),
					)
					break drainLoop
				}
				next := nextEnvelope(em.Envelope)
				tracer.Event(runCtx, "runner.routing",
					slog.String("from", string(em.From)),
					slog.String("to", string(dest.To)),
					slog.String("parent_session", string(em.Envelope.ID)),
					slog.String("next_session", string(next.ID)),
					slog.String("router_reason", dec.Reason),
				)
				if err := send(runCtx, ch, next); err != nil {
					result = Result{Chain: chain, Reason: ReasonContextDone, Detail: err.Error()}
					break drainLoop
				}
			}
		}
	}

	cancel()
	closeInputs(inputs)
	wg.Wait()
	return result, nil
}

// validate checks the configuration before any goroutine starts.
func validate(cfg Config) error {
	if cfg.Topology == nil {
		return errors.New("runner: Config.Topology is nil")
	}
	if cfg.Router == nil {
		return errors.New("runner: Config.Router is nil")
	}
	if cfg.Inhibitor == nil {
		return errors.New("runner: Config.Inhibitor is nil")
	}
	if len(cfg.Oscillators) == 0 {
		return errors.New("runner: Config.Oscillators is empty")
	}
	entry := cfg.Topology.Entry()
	if _, ok := cfg.Oscillators[entry]; !ok {
		return fmt.Errorf("runner: entry oscillator %q has no implementation in Oscillators map", entry)
	}
	for _, id := range cfg.Topology.Nodes() {
		if _, ok := cfg.Oscillators[id]; !ok {
			return fmt.Errorf("runner: topology node %q has no implementation in Oscillators map", id)
		}
	}
	return nil
}

func send(ctx context.Context, ch chan<- session.Envelope, env session.Envelope) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- env:
		return nil
	}
}

func closeInputs(inputs map[oscillator.ID]chan session.Envelope) {
	for _, ch := range inputs {
		close(ch)
	}
}

// idCounter gives the runner a unique session ID per hop. Monotonic;
// good enough for v0. Real sessions will get UUIDs.
var idCounter atomic.Int64

func nextEnvelope(upstream session.Envelope) session.Envelope {
	parent := upstream.ID
	// Invariant: oscillator.dispatch always populates Outcome before
	// emitting (even on adapter error — see ExitInhibited path). So
	// upstream.Outcome is non-nil here. Guard defensively anyway.
	var (
		signals []string
		verdict string
	)
	if out := upstream.Outcome; out != nil {
		signals = append([]string(nil), out.Signals...)
		verdict = out.Verdict
	}

	n := idCounter.Add(1)
	return session.Envelope{
		ID:             session.ID(fmt.Sprintf("sess-%d-%d", time.Now().UnixNano(), n)),
		Type:           session.TypeAnalyze,
		Objective:      fmt.Sprintf("continue from %s", upstream.ID),
		Classification: upstream.Classification,
		Notes: session.Notes{
			PriorSignals: signals,
		},
		Input: session.Input{
			Type:    "outcome_handoff",
			Content: verdict,
		},
		Trace: session.Trace{
			ParentSession: &parent,
		},
	}
}
