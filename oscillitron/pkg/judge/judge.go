// Package judge implements the audit layer that feeds the verifier
// policy's happiness signal (per the lock 2026-05-19).
//
// The judge is the frontier-model second opinion. After a local
// critique produces a verdict on a sibling's return_result, the runtime
// samples — 100% on un-grounded outputs, 10% on grounded ones — and
// asks the frontier judge for an independent verdict on the same
// (input, result, local_verdict) tuple. Agreement / disagreement is
// fed into verifier.Policy.RecordJudgeAgreement; that drives the phase
// ramp's sample rate.
//
// This is the audit tripwire against sycophancy drift (parent CLAUDE.md
// "Self-improvement loop"): the frontier costs more per call, but it
// runs on a small sample of outputs, so the aggregate cost stays
// bounded. The 10% / 100% split exists because grounded checks (compile,
// exec, retrieval-match) already provide one tier of ground truth — the
// frontier audit is the second tier, more expensive but needed mainly
// where no grounded check exists.
//
// What lives in this package:
//
//   - Judge interface — the frontier-model second-opinion surface.
//   - Stub impl — configurable verdict for tests and the demo.
//   - Sampler — owns the 100% / 10% policy and the per-target decision.
//
// What doesn't (yet) live here:
//
//   - A real frontier-backed Judge implementation. The interface is
//     stable; a Claude-API-backed impl lands in a follow-up PR.
package judge

import (
	"context"
	"math/rand/v2"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Request is the input to one judge call. The judge sees the same
// signal the local critique saw, plus the local verdict, so it can
// independently agree or disagree.
type Request struct {
	// Target is the AP being audited (the AP whose return_result the
	// local critique looked at).
	Target session.Envelope
	// LocalVerdict is what the local critique decided. The judge does
	// not see this until after deciding its own verdict in a real
	// implementation; the stub may ignore it.
	LocalVerdict session.Verdict
	// LocalIssues are the issues the local critique flagged. Carried
	// for trace fidelity; a real judge implementation may use them or
	// ignore them depending on its policy.
	LocalIssues []session.Issue
}

// Response is the judge's independent verdict.
type Response struct {
	// Verdict is the judge's pass/fail/issues call.
	Verdict session.Verdict
	// Issues is the judge's annotation. Optional.
	Issues []session.Issue
	// TokensUsed is the cost-bookkeeping handle for the call.
	TokensUsed int
}

// Judge is the audit interface. A real implementation calls a frontier
// model with the target input + result and asks for an independent
// verdict. Implementations should be safe for concurrent use; the
// runner is sync today, but a future async runtime may share one Judge
// across siblings.
type Judge interface {
	// Name identifies the judge in logs and the cost tracker.
	Name() string
	// Judge produces a verdict on the request. Errors surface to the
	// runner; the runner treats a failed judge call as "no audit signal
	// for this AP" (does not feed agreement to the policy) rather than
	// failing the run.
	Judge(ctx context.Context, req Request) (Response, error)
}

// SamplePolicy enumerates the two sampling tiers locked 2026-05-19.
type SamplePolicy struct {
	// UngroundedRate is the sample rate for outputs with no grounded
	// check (Signals.GroundedPass == nil). Lock: 1.0.
	UngroundedRate float64
	// GroundedRate is the sample rate for outputs with a grounded
	// check (Signals.GroundedPass != nil). Lock: 0.1.
	GroundedRate float64
}

// DefaultSamplePolicy returns the v0 locked rates.
func DefaultSamplePolicy() SamplePolicy {
	return SamplePolicy{UngroundedRate: 1.0, GroundedRate: 0.1}
}

// Sampler owns the per-target sampling decision. Stateless; the rand
// source is supplied per call so the runner can share its own seeded
// rand and keep determinism in tests.
type Sampler struct {
	Policy SamplePolicy
}

// NewSampler constructs a Sampler from a SamplePolicy.
func NewSampler(p SamplePolicy) *Sampler {
	return &Sampler{Policy: p}
}

// ShouldJudge reports whether the given target should be audited by the
// frontier judge. Reads Signals.GroundedPass on the target's
// return_result payload to decide which tier applies — if the target
// has no return_result payload (verifier_signal or emit_subtree), there
// is nothing to judge and ShouldJudge returns false.
func (s *Sampler) ShouldJudge(target session.Envelope, r *rand.Rand) bool {
	if target.Execute == nil || target.Execute.Category != session.CategoryReturnResult ||
		target.Execute.ReturnResult == nil {
		return false
	}
	rate := s.Policy.UngroundedRate
	if target.Execute.ReturnResult.Signals.GroundedPass != nil {
		rate = s.Policy.GroundedRate
	}
	if rate >= 1.0 {
		return true
	}
	if rate <= 0 {
		return false
	}
	return r.Float64() < rate
}

// IsGrounded reports whether the target counts as "grounded" for the
// sampler's purposes — i.e., it carries a co-located grounded-check
// signal. Useful for tests and trace annotation.
func IsGrounded(target session.Envelope) bool {
	if target.Execute == nil || target.Execute.ReturnResult == nil {
		return false
	}
	return target.Execute.ReturnResult.Signals.GroundedPass != nil
}
