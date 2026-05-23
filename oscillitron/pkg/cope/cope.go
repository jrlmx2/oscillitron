// CLAUDE GENERATED
// Package cope is the v3.4 "act" layer — the rule-table dispatcher
// that reads inadequacy signals (effective confidence + stakes) and
// picks a coping action.
//
// v3.0–v3.3 built the NOTICE side: stakes annotation, prompt/response
// signal detectors, calibrated confidence. cope is what *uses* those
// signals to actually change behavior. Per scratch/v3-design.md §4.4
// + §7.4, the rule table answers "given effective confidence X and
// stakes Y, what should the system do?":
//
//	conf ≥ 0.85, any stakes      → Ship (confident enough)
//	conf 0.5–0.85, low/medium    → ShipWithCaveat (qualify but use it)
//	conf 0.5–0.85, high          → ShipWithCaveat (Vote already ran)
//	conf < 0.5,    low/medium    → ShipWithCaveat (low-stakes = ship)
//	conf < 0.5,    high          → Escalate (high-stakes + uncertain
//	                                 = pay for the frontier call)
//
// Pure decision function — no I/O, no side effects. Adapters /
// orchestrators consume the Action and execute it. v3.4's
// canonical consumer is `pkg/benchmark/orchestrator.Coping`, which
// wraps an inner orchestrator with a frontier-escalation path.
//
// Signal-driven actions (Re-run on format violation, Refuse on
// refusal language, downgrade-by-prompt-overload) are listed in
// the design doc §4.4 but **deferred** from v3.4: they require
// passing the per-call notice Assessment from adapter to dispatcher,
// which is a plumbing layer the orchestrator surface doesn't expose
// today. The confidence/stakes table covers the load-bearing
// decisions; behavioral signals already feed `EffectiveConfidence`
// via v3.2's adjustments, so refusal-language already manifests as
// a confidence downgrade that this table will route correctly.
package cope

import "github.com/jrlmx2/oscillitron/pkg/stakes"

// Action is the coping decision for one call.
type Action string

const (
	// Ship — confidence is high enough; use the answer as-is.
	Ship Action = "ship"

	// ShipWithCaveat — answer is usable but flag uncertainty to
	// the user. Implementations typically append a hedge to the
	// user-facing output or set a UI flag.
	ShipWithCaveat Action = "ship_with_caveat"

	// Escalate — answer isn't trustworthy AND stakes are high
	// enough to justify spending on a frontier call. Implementations
	// re-run the case against a frontier substrate and use that
	// answer instead.
	Escalate Action = "escalate"

	// Refuse — answer should not ship. Defer to the user.
	// Implementations return an empty/flagged answer with an
	// explanation. v3.4 wires this only via the "no frontier
	// configured + escalate path" fallback; signal-driven refusal
	// (the model said "I can't reliably answer") is v3.4.x scope.
	Refuse Action = "refuse"
)

// RuleTable is the decision logic. Tunable thresholds are exported
// so operators can move the bands without recompiling. Defaults
// match the v3.3 calibration bands.
type RuleTable struct {
	// HighConfidence is the floor at or above which Ship fires
	// regardless of stakes. Default 0.85.
	HighConfidence float64
	// LowConfidence is the threshold below which the action depends
	// on stakes — low/medium stakes ship with caveat, high stakes
	// escalate. Default 0.5.
	LowConfidence float64
	// EscalateAllowed is whether the dispatcher should pick
	// Escalate when high-stakes + low-confidence. When false (no
	// frontier configured), the action falls back to Refuse.
	// Operators set this based on whether they have a frontier
	// adapter wired.
	EscalateAllowed bool
}

// DefaultRuleTable returns the v3.4 starting thresholds. Escalation
// allowed = true; operators turn it off when there's no frontier.
func DefaultRuleTable() RuleTable {
	return RuleTable{
		HighConfidence:  0.85,
		LowConfidence:   0.5,
		EscalateAllowed: true,
	}
}

// Decide picks an Action given the effective confidence and stakes.
//
// Special case: confidence == 0 means "no confidence reported"
// (zero value), NOT "zero confidence." When that happens we treat
// it as MidConfidence (between Low and High) — the model didn't
// tell us, so we don't know; ship-with-caveat is the safe default.
// Operators who want a different behavior wire a custom rule table
// and check Confidence == 0 themselves.
//
// Stakes zero value reads as Medium via stakes.Effective, matching
// the v3.0 backwards-compat guarantee.
func (rt RuleTable) Decide(effectiveConfidence float64, level stakes.Level) Action {
	eff := stakes.Effective(level)

	// "No confidence" → treat as mid-band (caveat).
	if effectiveConfidence == 0 {
		return ShipWithCaveat
	}

	if effectiveConfidence >= rt.HighConfidence {
		return Ship
	}

	if effectiveConfidence >= rt.LowConfidence {
		// Medium band — qualify but ship regardless of stakes.
		return ShipWithCaveat
	}

	// Low band — stakes decides whether to escalate.
	if eff == stakes.High {
		if rt.EscalateAllowed {
			return Escalate
		}
		return Refuse
	}
	return ShipWithCaveat
}

// Decision is the dispatcher's return when callers want both the
// Action and the structured rationale. Use this for trace events
// and report columns — Decide() alone is the bare lookup.
type Decision struct {
	Action     Action
	Confidence float64
	Stakes     stakes.Level
	// Rationale is a short human-readable explanation of why the
	// action was chosen. Goes into logs and per-case trace events.
	Rationale string
}

// Explain returns the Decision with rationale populated.
func (rt RuleTable) Explain(effectiveConfidence float64, level stakes.Level) Decision {
	action := rt.Decide(effectiveConfidence, level)
	eff := stakes.Effective(level)
	rationale := ""
	switch action {
	case Ship:
		rationale = "confidence at or above ship floor"
	case ShipWithCaveat:
		switch {
		case effectiveConfidence == 0:
			rationale = "no confidence reported; shipping with caveat"
		case effectiveConfidence >= rt.LowConfidence:
			rationale = "medium-band confidence; qualify but ship"
		default:
			rationale = "low confidence but stakes are low/medium; ship with caveat"
		}
	case Escalate:
		rationale = "low confidence on high-stakes case; escalate to frontier"
	case Refuse:
		rationale = "low confidence on high-stakes case but no escalation path; refuse"
	}
	return Decision{
		Action:     action,
		Confidence: effectiveConfidence,
		Stakes:     eff,
		Rationale:  rationale,
	}
}
