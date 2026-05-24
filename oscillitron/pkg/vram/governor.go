package vram

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Governor is the process-wide VRAM coordinator. Anything that runs
// inference against a shared substrate (runner-orchestrated APs,
// grader calls, future benchmark drivers) acquires a lease from the
// same Governor so they cooperate on a single VRAM budget.
//
// What the Governor owns end-to-end:
//
//   - Probing: it auto-selects a platform probe via vram.Auto() —
//     operators do NOT plug probes in. ProbeOverride exists for tests.
//   - Estimating: it builds the per-call estimator internally from
//     the ModelSpec the operator supplied. No magic-number defaults.
//   - Accounting: Acquire blocks until granting the lease would fit
//     under both the ceiling and (Available × (1 − ReserveFraction));
//     Release returns the slot.
//
// What the Governor does NOT own:
//
//   - Graph execution. The runner walks the AP call tree; the
//     governor only sees Acquire/Release calls. The two concerns
//     don't entangle.
//
// Probe failure is non-fatal. When the platform probe returns an
// error, the Governor degrades to ceiling-only enforcement — it
// still bounds concurrency, just without VRAM accounting. The
// alternative (refuse to grant anything) would block the runtime
// indefinitely on transient probe failures.
//
// Waiters are served in FIFO order. Because all callers share one
// model/estimator, every waiting Acquire has the same per-call byte
// cost — if the head can't fit, none of the waiters can fit either,
// so strict FIFO is starvation-free in v0.
//
// A nil *Governor is safe to call. Acquire on nil returns a no-op
// lease and nil error so consumers can opt in without nil checks at
// every call site.
type Governor struct {
	probe       Probe
	estimator   SlidingWindowEstimator
	model       ModelSpec
	ceiling     int
	reserveFrac float64
	tracer      trace.Tracer

	mu       sync.Mutex
	inflight uint64
	leases   int
	waiters  *list.List // *waiter in FIFO order
}

// ModelSpec describes the inference target. Operator-supplied because
// model architecture is the one piece the governor can't auto-detect
// from the platform — the substrate (Hermes / llama.cpp / vLLM)
// doesn't expose layer counts or KV head dimensions through its API.
//
// All four numeric fields are required and validated. There are no
// defaults — guessing model architecture from a name would be a magic
// number in disguise.
type ModelSpec struct {
	// Name is the model identifier for trace records.
	Name string
	// Layers is the transformer layer count.
	Layers int
	// KVHiddenDim is num_kv_heads × head_dim. For models without
	// grouped-query attention this equals the hidden dimension; for
	// Llama-3 / Mistral / Gemma-style GQA it's smaller (commonly
	// 1/4 to 1/8 of hidden dim).
	KVHiddenDim int
	// KVDtypeBytes is the per-element size of the KV cache. fp16/bf16
	// = 2; fp8 = 1. Quantized KV (q4/q8) usually still stores at fp16
	// for compute; check your engine.
	KVDtypeBytes int
	// ContextSize is the model's hard context-window ceiling
	// (tokens). KV cache cannot grow past this; the estimator caps
	// per-call bytes accordingly.
	ContextSize int
	// PrefixTokens is the persona + (optional) semantic pool +
	// per-playbook prefix the substrate injects on every call.
	// Operator-supplied because the prefix is constructed in the
	// substrate before Oscillitron sees it. 0 leaves it out of the
	// estimate (conservative — estimator under-counts, governor
	// runs slightly tighter than it needs to).
	PrefixTokens int
	// ModelResidentBytes is a fixed per-session overhead the model
	// imposes outside the KV cache (loaded weights, intermediate
	// activations the engine keeps per session, etc.). Optional —
	// when 0 the estimator assumes the substrate de-duplicates the
	// weights across sessions. The library-managed (auto) governor
	// path sets a conservative 3 GB default via DefaultVRAMModel so
	// the small-model OOM scenario (N concurrent 3 GB-resident calls
	// blowing through unified memory) gets caught before launch.
	ModelResidentBytes uint64
}

// Validate returns an error if any required ModelSpec field is unset
// or non-positive. Used by NewGovernor to fail fast at config time
// rather than silently producing garbage byte estimates at runtime.
func (m ModelSpec) Validate() error {
	if m.Layers <= 0 {
		return errors.New("ModelSpec.Layers must be > 0")
	}
	if m.KVHiddenDim <= 0 {
		return errors.New("ModelSpec.KVHiddenDim must be > 0")
	}
	if m.KVDtypeBytes <= 0 {
		return errors.New("ModelSpec.KVDtypeBytes must be > 0")
	}
	if m.ContextSize <= 0 {
		return errors.New("ModelSpec.ContextSize must be > 0")
	}
	if m.PrefixTokens < 0 {
		return errors.New("ModelSpec.PrefixTokens must be >= 0")
	}
	return nil
}

// GovernorConfig configures a new Governor.
//
// The only required field is Model. Ceiling and ReserveFraction have
// derived defaults (NumCPU and 5% respectively), not magic numbers.
type GovernorConfig struct {
	// Model is the inference target. Required.
	Model ModelSpec
	// Ceiling caps concurrent leases regardless of VRAM headroom.
	// 0 means runtime.NumCPU() — derived from real hardware rather
	// than a made-up constant. Set to 1 for strict serial.
	Ceiling int
	// ReserveFraction is the share of probed available memory held
	// back as a safety margin. 0 means 0.05 (5%). Range (0, 1).
	// A fraction, not an absolute byte count — scales with the host.
	ReserveFraction float64
	// Tracer emits structured events. Defaults to trace.Discard{}.
	Tracer trace.Tracer
	// ProbeOverride substitutes for the auto-detected platform probe.
	// Nil (the production default) → vram.Auto(). Tests and CI use
	// this to inject controlled probes.
	ProbeOverride Probe
}

// NewGovernor constructs a Governor. Returns an error if the
// ModelSpec fails Validate — the governor refuses to start with
// architecture it can't trust to produce real byte estimates.
func NewGovernor(cfg GovernorConfig) (*Governor, error) {
	if err := cfg.Model.Validate(); err != nil {
		return nil, fmt.Errorf("vram.NewGovernor: %w", err)
	}

	probe := cfg.ProbeOverride
	if probe == nil {
		probe = Auto()
	}

	ceiling := cfg.Ceiling
	if ceiling <= 0 {
		ceiling = runtime.NumCPU()
	}

	reserve := cfg.ReserveFraction
	if reserve <= 0 {
		reserve = 0.05
	}
	if reserve >= 1 {
		return nil, fmt.Errorf("vram.NewGovernor: ReserveFraction must be < 1, got %g", reserve)
	}

	tracer := cfg.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}

	estimator := EstimatorForModel(cfg.Model.Layers, cfg.Model.KVHiddenDim, cfg.Model.KVDtypeBytes)
	if estimator.BytesPerToken == 0 {
		// Defense in depth — Validate already caught this, but if
		// EstimatorForModel ever fails to compute we'd rather fail
		// here than produce a no-op governor.
		return nil, errors.New("vram.NewGovernor: estimator produced zero BytesPerToken")
	}
	// Propagate the spec's per-session resident overhead into the
	// estimator so per-call estimates include weights / activations the
	// engine doesn't deduplicate across sessions. This is what catches
	// the N×3 GB OOM under the auto path (DefaultVRAMModel sets it).
	estimator.ModelResidentBytes = cfg.Model.ModelResidentBytes

	return &Governor{
		probe:       probe,
		estimator:   estimator,
		model:       cfg.Model,
		ceiling:     ceiling,
		reserveFrac: reserve,
		tracer:      tracer,
		waiters:     list.New(),
	}, nil
}

// Lease is one acquired slot. Release returns the slot's bytes and
// concurrency credit to the governor. Double-release is a no-op.
// The zero value's Release is a no-op (handy for the nil-governor
// path; see Governor.Acquire).
type Lease struct {
	g     *Governor
	bytes uint64

	mu       sync.Mutex
	released bool
}

// Release returns this lease's bytes and concurrency credit to the
// governor. Safe to call from any goroutine; safe to call multiple
// times (second+ calls are no-ops).
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.released || l.g == nil {
		l.mu.Unlock()
		return
	}
	l.released = true
	g := l.g
	bytes := l.bytes
	l.mu.Unlock()
	g.release(bytes)
}

// Bytes returns the byte estimate this lease was granted against.
// 0 in ceiling-only mode (probe is unavailable or failing).
func (l *Lease) Bytes() uint64 {
	if l == nil {
		return 0
	}
	return l.bytes
}

// Acquire blocks until granting a lease would fit under both the
// ceiling and the VRAM budget. Cancelling ctx aborts the wait and
// returns ctx.Err().
//
// On a nil *Governor, Acquire returns a no-op lease and nil. This
// lets consumer code pass an optional governor without a nil check
// at every call site (the runner, the grader, future bench drivers).
func (g *Governor) Acquire(ctx context.Context) (*Lease, error) {
	if g == nil {
		return &Lease{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	estBytes, available := g.measure(ctx)

	g.mu.Lock()
	// Fast path: budget available right now.
	if g.canGrantLocked(estBytes, available) {
		g.inflight += estBytes
		g.leases++
		inflight, leases := g.inflight, g.leases
		g.mu.Unlock()
		trace.Info(g.tracer, ctx, "governor.acquire",
			slog.String("model", g.model.Name),
			slog.Uint64("bytes", estBytes),
			slog.Uint64("inflight", inflight),
			slog.Int("leases", leases),
		)
		return &Lease{g: g, bytes: estBytes}, nil
	}

	// Slow path: enqueue and wait. The waiter holds its position in
	// the FIFO via w.element; release pops the head and closes its
	// ready channel.
	w := &waiter{ready: make(chan struct{})}
	w.element = g.waiters.PushBack(w)
	queued := g.waiters.Len()
	g.mu.Unlock()

	trace.Info(g.tracer, ctx, "governor.acquire_waiting",
		slog.String("model", g.model.Name),
		slog.Uint64("requested_bytes", estBytes),
		slog.Int("queued_waiters", queued),
	)

	for {
		select {
		case <-ctx.Done():
			g.mu.Lock()
			wasWoken := w.element == nil
			if w.element != nil {
				g.waiters.Remove(w.element)
				w.element = nil
			}
			if wasWoken {
				// We were popped-and-woken by a release but cancelled
				// before claiming. The slot the release freed is
				// otherwise lost — wake the next waiter so the freed
				// budget reaches someone.
				g.wakeHeadLocked()
			}
			g.mu.Unlock()
			return nil, ctx.Err()

		case <-w.ready:
			// Woken by release. Re-measure (headroom may have changed
			// while we waited) and re-check the budget.
			estBytes, available = g.measure(ctx)
			g.mu.Lock()
			if g.canGrantLocked(estBytes, available) {
				g.inflight += estBytes
				g.leases++
				inflight, leases := g.inflight, g.leases
				g.mu.Unlock()
				trace.Info(g.tracer, ctx, "governor.acquire",
					slog.String("model", g.model.Name),
					slog.Uint64("bytes", estBytes),
					slog.Uint64("inflight", inflight),
					slog.Int("leases", leases),
				)
				return &Lease{g: g, bytes: estBytes}, nil
			}
			// Budget still doesn't fit. Re-arm and re-push at the
			// front so we stay at the head of the queue.
			w.ready = make(chan struct{})
			w.element = g.waiters.PushFront(w)
			g.mu.Unlock()
		}
	}
}

// Snapshot returns a point-in-time view of the governor's state.
// Useful for telemetry and tests. Samples the probe (one call).
func (g *Governor) Snapshot(ctx context.Context) Snapshot {
	if g == nil {
		return Snapshot{}
	}
	estBytes, available := g.measure(ctx)
	g.mu.Lock()
	defer g.mu.Unlock()
	return Snapshot{
		Model:           g.model,
		PerCallBytes:    estBytes,
		AvailableBytes:  available,
		InflightBytes:   g.inflight,
		ActiveLeases:    g.leases,
		QueuedWaiters:   g.waiters.Len(),
		Ceiling:         g.ceiling,
		ReserveFraction: g.reserveFrac,
	}
}

// Snapshot is a point-in-time governor view.
type Snapshot struct {
	Model           ModelSpec
	PerCallBytes    uint64
	AvailableBytes  uint64
	InflightBytes   uint64
	ActiveLeases    int
	QueuedWaiters   int
	Ceiling         int
	ReserveFraction float64
}

// QueuedWaiters returns the number of currently-waiting Acquire calls.
func (g *Governor) QueuedWaiters() int {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.waiters.Len()
}

// Ceiling returns the configured concurrency ceiling. Cheap (no
// probe sample). 0 for a nil governor.
func (g *Governor) Ceiling() int {
	if g == nil {
		return 0
	}
	return g.ceiling
}

// Model returns the configured ModelSpec.
func (g *Governor) Model() ModelSpec {
	if g == nil {
		return ModelSpec{}
	}
	return g.model
}

// --- internals ---

type waiter struct {
	ready   chan struct{}
	element *list.Element
}

// measure samples the probe and computes the per-call estimate.
// Returns (perCallBytes, available). On probe failure, returns
// (perCall, 0) so the budget check skips VRAM and the ceiling
// becomes the only constraint.
func (g *Governor) measure(ctx context.Context) (perCall uint64, available uint64) {
	perCall = g.estimator.Estimate(SessionEstimate{
		Model:        g.model.Name,
		PrefixTokens: g.model.PrefixTokens,
		ContextSize:  g.model.ContextSize,
	})
	report, err := g.probe.Probe(ctx)
	if err != nil {
		trace.Error(g.tracer, ctx, "governor.probe_failed",
			slog.String("err", err.Error()),
		)
		return perCall, 0
	}
	return perCall, report.AvailableBytes
}

// canGrantLocked is the budget check. Caller must hold g.mu.
//
//   - Ceiling: lease count must stay ≤ ceiling.
//   - VRAM:    inflight + est must stay ≤ available × (1 − reserveFrac).
//     When available == 0 (probe failed), the VRAM check is skipped.
//
// Special case: the very first lease is always granted (subject to
// ceiling). Refusing it would deadlock the runtime on under-
// provisioned hardware where a single call is bigger than the budget.
func (g *Governor) canGrantLocked(est, available uint64) bool {
	if g.leases >= g.ceiling {
		return false
	}
	if available == 0 || est == 0 {
		return true
	}
	if g.leases == 0 {
		return true
	}
	budget := uint64(float64(available) * (1 - g.reserveFrac))
	return g.inflight+est <= budget
}

// release frees a lease's slot. Wakes the head waiter (if any).
func (g *Governor) release(bytes uint64) {
	g.mu.Lock()
	if bytes > g.inflight {
		g.inflight = 0
	} else {
		g.inflight -= bytes
	}
	if g.leases > 0 {
		g.leases--
	}
	g.wakeHeadLocked()
	inflight, leases := g.inflight, g.leases
	g.mu.Unlock()
	trace.Info(g.tracer, context.Background(), "governor.release",
		slog.Uint64("bytes", bytes),
		slog.Uint64("inflight", inflight),
		slog.Int("leases", leases),
	)
}

// wakeHeadLocked pops the head waiter (if any) and signals it.
// Caller must hold mu.
func (g *Governor) wakeHeadLocked() {
	front := g.waiters.Front()
	if front == nil {
		return
	}
	w := front.Value.(*waiter)
	g.waiters.Remove(front)
	w.element = nil
	close(w.ready)
}

// String is a debug-friendly rendering of the ModelSpec.
func (m ModelSpec) String() string {
	name := m.Name
	if name == "" {
		name = "unnamed"
	}
	if m.ModelResidentBytes > 0 {
		return fmt.Sprintf("%s(layers=%d,kv=%d,dtype=%d,ctx=%d,prefix=%d,resident=%dB)",
			name, m.Layers, m.KVHiddenDim, m.KVDtypeBytes, m.ContextSize, m.PrefixTokens, m.ModelResidentBytes)
	}
	return fmt.Sprintf("%s(layers=%d,kv=%d,dtype=%d,ctx=%d,prefix=%d)",
		name, m.Layers, m.KVHiddenDim, m.KVDtypeBytes, m.ContextSize, m.PrefixTokens)
}
