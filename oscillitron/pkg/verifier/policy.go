// Package verifier implements the verifier policy (locked 2026-05-20):
// a phase-ramp sampling rate that decides whether the runtime emits a
// critique AP after a return_result.
//
// Bootstrap (n_invocations <= bootstrap_threshold): sample_rate = 1.0.
// Steady-state: sample_rate = max(floor, 1 - happiness_wilson_lower_bound)
// where happiness is the judge-sampling agreement rate over a sliding
// window. Reading the Wilson *lower* bound (not the point estimate)
// protects freshly-bootstrapped playbooks from premature throttling on
// a lucky early run.
//
// HappinessScope ∈ {global, per_action} is configurable. Telemetry for
// both is recorded regardless of which drives the rate, so the policy
// can be A/B'd later without re-instrumenting.
//
// Parent override (envelope.NeedsVerification == true) forces critique
// on top of the sampling baseline. Suppression by parent is not allowed
// in v0 — parent can pin, not unpin.
//
// All four numeric defaults (10k bootstrap, 15% floor, 2k window, 95%
// CI) are starting points; revisit if telemetry shows wild rate swings
// or saturation. See scratch/design-notes.md "Verifier policy".
package verifier

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Scope chooses whether happiness is tracked globally or per action.
type Scope string

const (
	ScopeGlobal    Scope = "global"
	ScopePerAction Scope = "per_action"
)

// Config bundles the four numeric knobs plus the happiness scope.
type Config struct {
	BootstrapThreshold int     // default 10_000
	Floor              float64 // default 0.15
	SlidingWindow      int     // default 2_000
	ConfidenceLevel    float64 // default 0.95
	HappinessScope     Scope   // default ScopeGlobal
}

// DefaultConfig returns the v0 defaults locked 2026-05-20.
func DefaultConfig() Config {
	return Config{
		BootstrapThreshold: 10_000,
		Floor:              0.15,
		SlidingWindow:      2_000,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopeGlobal,
	}
}

// Policy holds runtime state for the sampling decision. Safe for
// concurrent use; v0 runner is sync but a future async runtime may
// share a single Policy across siblings.
type Policy struct {
	cfg Config

	mu     sync.Mutex
	global *scopeState
	// per-action state; populated lazily on first touch.
	states map[session.Playbook]*scopeState
}

type scopeState struct {
	invocations int
	window      *ringWindow
}

// New constructs a Policy from cfg. The cfg is used verbatim — callers
// who want v0 defaults should pass DefaultConfig() (possibly with
// per-field overrides). A zero SlidingWindow is coerced to 1 internally
// to keep the ring buffer sound; everything else passes through.
func New(cfg Config) *Policy {
	winCap := cfg.SlidingWindow
	if winCap <= 0 {
		winCap = 1
	}
	return &Policy{
		cfg:    cfg,
		global: &scopeState{window: newRingWindow(winCap)},
		states: map[session.Playbook]*scopeState{},
	}
}

// ShouldCritique decides whether the runtime should emit a critique on
// an AP whose Evaluate.Playbook is `action`. If parentOverride is true
// (envelope.NeedsVerification), the answer is unconditionally true.
// Otherwise the policy rolls against the current sample rate.
//
// Side effect: increments the invocation counter for the active scope
// and the global counter (the latter for parity telemetry). Call
// exactly once per policy-eligible AP.
func (p *Policy) ShouldCritique(action session.Playbook, parentOverride bool, r *rand.Rand) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Decide using *prior* invocation count (read before increment), so
	// BootstrapThreshold=N means the first N calls are bootstrap and the
	// (N+1)th is steady-state.
	rate := p.sampleRateLocked(action)

	// Telemetry always increments both counters.
	p.global.invocations++
	p.stateForLocked(action).invocations++

	if parentOverride {
		return true
	}
	if rate >= 1.0 {
		return true
	}
	if rate <= 0 {
		return false
	}
	return r.Float64() < rate
}

// SampleRate returns the current sampling rate for `action` without
// rolling. Useful for telemetry, dashboards, and tests.
func (p *Policy) SampleRate(action session.Playbook) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sampleRateLocked(action)
}

// RecordJudgeAgreement feeds one judge-sampling outcome into the
// happiness windows. Both the global window and the per-action window
// are updated regardless of HappinessScope so both telemetry streams
// stay populated.
func (p *Policy) RecordJudgeAgreement(action session.Playbook, agreed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.global.window.add(agreed)
	p.stateForLocked(action).window.add(agreed)
}

// Telemetry is a snapshot of policy state for inspection. Cheap; not
// on the inference hot path.
type Telemetry struct {
	Scope     Scope
	Global    ScopeTelemetry
	PerAction map[session.Playbook]ScopeTelemetry
}

// ScopeTelemetry captures the relevant counters and rates for one scope.
type ScopeTelemetry struct {
	Invocations          int
	WindowCount          int
	HappinessPoint       float64
	HappinessWilsonLower float64
	SampleRate           float64
}

// Telemetry snapshots the current state of the policy. Both global and
// per-action streams are populated regardless of which one drives the
// active rate.
func (p *Policy) Telemetry() Telemetry {
	p.mu.Lock()
	defer p.mu.Unlock()

	snap := func(st *scopeState) ScopeTelemetry {
		succ, tot := st.window.stats()
		point, lb := 0.0, 0.0
		if tot > 0 {
			point = float64(succ) / float64(tot)
			lb = wilsonLowerBound(succ, tot, p.cfg.ConfidenceLevel)
		}
		return ScopeTelemetry{
			Invocations:          st.invocations,
			WindowCount:          tot,
			HappinessPoint:       point,
			HappinessWilsonLower: lb,
		}
	}
	gt := snap(p.global)
	gt.SampleRate = p.rateFromState(p.global)
	perAction := map[session.Playbook]ScopeTelemetry{}
	for a, st := range p.states {
		at := snap(st)
		at.SampleRate = p.rateFromState(st)
		perAction[a] = at
	}
	return Telemetry{Scope: p.cfg.HappinessScope, Global: gt, PerAction: perAction}
}

// sampleRateLocked computes the sample rate for `action` using the
// active scope's counters.
func (p *Policy) sampleRateLocked(action session.Playbook) float64 {
	switch p.cfg.HappinessScope {
	case ScopePerAction:
		return p.rateFromState(p.stateForLocked(action))
	default:
		return p.rateFromState(p.global)
	}
}

func (p *Policy) rateFromState(st *scopeState) float64 {
	// Strict less-than: the first BootstrapThreshold invocations are
	// bootstrap; the (threshold+1)th call is the first steady-state one.
	// BootstrapThreshold=0 → no bootstrap phase, even at invocations=0.
	if st.invocations < p.cfg.BootstrapThreshold {
		return 1.0
	}
	succ, tot := st.window.stats()
	if tot == 0 {
		return 1.0
	}
	lb := wilsonLowerBound(succ, tot, p.cfg.ConfidenceLevel)
	rate := 1 - lb
	if rate < p.cfg.Floor {
		rate = p.cfg.Floor
	}
	if rate > 1 {
		rate = 1
	}
	return rate
}

func (p *Policy) stateForLocked(action session.Playbook) *scopeState {
	st, ok := p.states[action]
	if !ok {
		winCap := p.cfg.SlidingWindow
		if winCap <= 0 {
			winCap = 1
		}
		st = &scopeState{window: newRingWindow(winCap)}
		p.states[action] = st
	}
	return st
}

// ringWindow is a fixed-capacity ring buffer of boolean agreements. On
// overflow it overwrites the oldest entry; this is the sliding-window
// behavior the policy wants.
type ringWindow struct {
	cap   int
	buf   []bool
	head  int
	full  bool
	succs int
}

func newRingWindow(cap int) *ringWindow {
	if cap <= 0 {
		cap = 1
	}
	return &ringWindow{cap: cap, buf: make([]bool, cap)}
}

func (w *ringWindow) add(agreed bool) {
	if w.full {
		if w.buf[w.head] {
			w.succs--
		}
	}
	w.buf[w.head] = agreed
	if agreed {
		w.succs++
	}
	w.head++
	if w.head >= w.cap {
		w.head = 0
		w.full = true
	}
}

func (w *ringWindow) count() int {
	if w.full {
		return w.cap
	}
	return w.head
}

func (w *ringWindow) stats() (successes, total int) {
	return w.succs, w.count()
}

// wilsonLowerBound returns the lower bound of the Wilson score interval
// at the given confidence level. Returns 0 when total == 0.
//
// Wilson interval (Brown/Cai/DasGupta 2001):
//
//	(p + z²/2n ± z·sqrt((p(1-p) + z²/4n) / n)) / (1 + z²/n)
//
// where p = successes/total, n = total, z = N⁻¹((1+conf)/2).
func wilsonLowerBound(successes, total int, confidence float64) float64 {
	if total <= 0 {
		return 0
	}
	n := float64(total)
	p := float64(successes) / n
	z := normalQuantile((1 + confidence) / 2)
	z2 := z * z
	denom := 1 + z2/n
	center := p + z2/(2*n)
	margin := z * math.Sqrt((p*(1-p)+z2/(4*n))/n)
	lb := (center - margin) / denom
	if lb < 0 {
		lb = 0
	}
	if lb > 1 {
		lb = 1
	}
	return lb
}

// normalQuantile returns the inverse of the standard normal CDF for
// p ∈ (0, 1) using Acklam's approximation. The Go stdlib has no native
// inverse normal CDF; this approximation is well within the precision
// the policy needs (the rate is clamped to a floor and rolled against a
// float64 RNG anyway).
func normalQuantile(p float64) float64 {
	if p <= 0 || p >= 1 {
		if p <= 0 {
			return math.Inf(-1)
		}
		return math.Inf(+1)
	}
	a := [...]float64{
		-3.969683028665376e+01, 2.209460984245205e+02,
		-2.759285104469687e+02, 1.383577518672690e+02,
		-3.066479806614716e+01, 2.506628277459239e+00,
	}
	b := [...]float64{
		-5.447609879822406e+01, 1.615858368580409e+02,
		-1.556989798598866e+02, 6.680131188771972e+01,
		-1.328068155288572e+01,
	}
	c := [...]float64{
		-7.784894002430293e-03, -3.223964580411365e-01,
		-2.400758277161838e+00, -2.549732539343734e+00,
		4.374664141464968e+00, 2.938163982698783e+00,
	}
	d := [...]float64{
		7.784695709041462e-03, 3.224671290700398e-01,
		2.445134137142996e+00, 3.754408661907416e+00,
	}
	plow := 0.02425
	phigh := 1 - plow
	switch {
	case p < plow:
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= phigh:
		q := p - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
}

// Compile-time guard that rand.Rand satisfies the small set of methods
// the policy uses (kept here as documentation of the dependency).
var _ = rand.Float64
