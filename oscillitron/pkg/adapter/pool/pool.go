// CLAUDE GENERATED
// Package pool provides an Adapter that fans concurrent Calls across
// a fixed-size set of backing Adapters. Each backing Adapter still
// owns its own per-region lock (see hermes.Adapter.callMu) — the
// pool's job is choosing WHICH backing Adapter to dispatch to.
//
// Semantic trade-off (READ THIS BEFORE USING):
//
// Parent ../../CLAUDE.md locks "one persistent ACP session per
// oscillator" so that Hermes' skill/memory updates accrue in a
// single deterministic order per "brain region." A pool of N
// Adapters under one logical region means N parallel learning
// threads on (probably) the same underlying model — playbook
// updates fan across replicas and may diverge over time.
//
// Use this Adapter when:
//   - The backing model is large/parallel and the workload is
//     throughput-bound (LLM serving, not coding agent).
//   - The specialist is effectively stateless for the call —
//     prompts are self-contained, no growing skill set you want
//     deterministically ordered.
//   - You're running a comparison harness where each call is an
//     independent benchmark unit.
//
// Do NOT use this Adapter when:
//   - The specialist is expected to grow skills over time and the
//     order of APs matters (the default Phase 2 stance).
//   - Replicas would diverge in ways that downstream consumers
//     (curation, eval) can't reconcile.
//
// A future, smarter pool can serialize stateful learning while
// allowing parallel stateless reads — that's a Phase 3 design.
package pool

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// ParallelDefault is the parallel-mode default for newly constructed
// Adapters. When false, Call always dispatches to backends[0] regardless
// of strategy, restoring the per-region "single learning thread"
// invariant. Togglable per-Adapter via SetParallel for runtime control.
const ParallelDefault = true

// Strategy picks a backing Adapter index given the pool size.
// Stateless: implementations should be safe for concurrent calls.
type Strategy interface {
	Pick(poolSize int) int
}

// RoundRobin is the default strategy. Lock-free via atomic counter;
// monotonic so successive picks visit each backing adapter in turn.
type RoundRobin struct {
	next atomic.Uint64
}

// Pick implements Strategy.
func (r *RoundRobin) Pick(n int) int {
	if n <= 0 {
		return 0
	}
	// Subtract one because Add returns the post-increment value.
	return int((r.next.Add(1) - 1) % uint64(n))
}

// Adapter is the pool. Holds N backing Adapters and dispatches each
// Call to one of them per Strategy. Name() returns the configured
// pool name (typically the specialist's "brain region" name); the
// backing Adapters' own Names are not surfaced (they're implementation
// detail of the pool).
//
// Parallel mode (atomic, runtime-togglable) controls dispatch:
//   - true  → strategy picks the backend, fan-out across replicas
//   - false → all Calls route to backends[0], restoring the per-region
//     "single learning thread" invariant. Other backends sit
//     dormant. Use this as a kill switch when parallel
//     divergence (skill drift, replay non-determinism, a
//     flaky backend) becomes a problem.
type Adapter struct {
	name     string
	backends []adapter.Adapter
	strategy Strategy
	parallel atomic.Bool
}

// New constructs a pool with the given backing Adapters and strategy.
// If strategy is nil, RoundRobin is used. backends must be non-empty.
func New(name string, strategy Strategy, backends ...adapter.Adapter) (*Adapter, error) {
	if name == "" {
		return nil, errors.New("pool: name is required")
	}
	if len(backends) == 0 {
		return nil, errors.New("pool: at least one backing adapter is required")
	}
	for i, b := range backends {
		if b == nil {
			return nil, errors.New("pool: nil backing adapter at index " + itoa(i))
		}
	}
	if strategy == nil {
		strategy = &RoundRobin{}
	}
	a := &Adapter{
		name:     name,
		backends: append([]adapter.Adapter(nil), backends...),
		strategy: strategy,
	}
	a.parallel.Store(ParallelDefault)
	return a, nil
}

// SetParallel toggles parallel mode at runtime. Safe for concurrent
// use; in-flight Calls observe whichever value was visible when their
// dispatch decision ran (no guarantee about partial-window switching).
func (a *Adapter) SetParallel(on bool) { a.parallel.Store(on) }

// Parallel reports the current parallel-mode setting.
func (a *Adapter) Parallel() bool { return a.parallel.Load() }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return a.name }

// Size returns the number of backing Adapters.
func (a *Adapter) Size() int { return len(a.backends) }

// Call implements adapter.Adapter. Picks a backing Adapter via the
// strategy (parallel=true) or pins to backends[0] (parallel=false)
// and forwards the envelope. Errors and Outcomes are returned
// unchanged — pool composition is invisible to callers.
func (a *Adapter) Call(ctx context.Context, env session.Envelope) (session.Outcome, error) {
	idx := 0
	if a.parallel.Load() {
		idx = a.strategy.Pick(len(a.backends))
		if idx < 0 || idx >= len(a.backends) {
			idx = 0
		}
	}
	return a.backends[idx].Call(ctx, env)
}

// Compile-time check.
var _ adapter.Adapter = (*Adapter)(nil)

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = digits[i%10]
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
