// v3.4 Coping wrapper — applies the cope.RuleTable to an inner
// orchestrator's Answer and optionally escalates to a frontier
// orchestrator for high-stakes low-confidence cases.

package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/cope"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Coping wraps an Inner orchestrator with the v3.4 cope dispatcher.
// After Inner produces an Answer, Decide picks an Action:
//
//   - Ship: return the Answer as-is.
//   - ShipWithCaveat: return the Answer with CopeAction stamped;
//     downstream consumers may decorate output (e.g., append a
//     hedge) but the bench's grader sees the same Extracted.
//   - Escalate: re-run the case against Frontier and return its
//     Answer (CopeAction = "escalate"). When Frontier is nil, the
//     RuleTable's EscalateAllowed flips to false and the dispatcher
//     produces Refuse instead — a safer fail mode than "silently
//     skip escalation."
//   - Refuse: return an empty Answer with CopeAction = "refuse".
//     Grader sees Extracted=""; case will fail by extraction.
//
// Escalation costs add up — Frontier calls are typically the
// expensive substrate. The point of the rule table is to spend
// frontier tokens only where it matters (high stakes + low
// confidence). The calibration table (pkg/benchmark/calibration)
// is the empirical check that confidence is calibrated enough to
// trust the routing.
type Coping struct {
	// NameStr appears in benchmark reports. Required.
	NameStr string
	// Inner is the underlying orchestrator (typically Vote). The
	// Coping wrapper consults its Answer's Confidence + the Case's
	// Stakes to pick a coping action. Required.
	Inner benchmark.Orchestrator
	// Frontier is the escalation target. Optional — when nil,
	// Escalate becomes Refuse (safer than silently ignoring).
	// Typically wired to a Single orchestrator backed by the
	// anthropic adapter.
	Frontier benchmark.Orchestrator
	// Rules is the dispatcher config. Zero value uses
	// cope.DefaultRuleTable(); EscalateAllowed is set from
	// (Frontier != nil) at construction-fixup time.
	Rules cope.RuleTable
	// Tracer emits per-case `coping.decision` events. nil-safe.
	Tracer trace.Tracer
}

// Name implements benchmark.Orchestrator.
func (c Coping) Name() string { return c.NameStr }

// Answer implements benchmark.Orchestrator. Runs Inner; consults
// the rule table; optionally escalates.
func (c Coping) Answer(ctx context.Context, kase benchmark.Case) (benchmark.Answer, error) {
	if c.Inner == nil {
		return benchmark.Answer{}, fmt.Errorf("coping: Inner orchestrator is required")
	}
	tracer := c.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}

	// Fixup the rules: per-field default-fill so an operator who
	// customizes one threshold doesn't silently lose the others to
	// zero values. The earlier all-zero AND-condition bypassed
	// defaults entirely on partial config (e.g., setting only
	// HighConfidence collapsed LowConfidence to 0, erasing the
	// caveat band, and left EscalateAllowed at false-zero, silently
	// disabling escalation). Then AND with Frontier-presence so
	// "explicitly opt out by not providing Frontier" still holds.
	defaults := cope.DefaultRuleTable()
	rules := c.Rules
	if rules.HighConfidence == 0 {
		rules.HighConfidence = defaults.HighConfidence
	}
	if rules.LowConfidence == 0 {
		rules.LowConfidence = defaults.LowConfidence
	}
	if !rules.EscalateAllowed {
		rules.EscalateAllowed = defaults.EscalateAllowed
	}
	rules.EscalateAllowed = rules.EscalateAllowed && c.Frontier != nil

	inner, err := c.Inner.Answer(ctx, kase)
	if err != nil {
		return inner, err
	}

	decision := rules.Explain(inner.Confidence, kase.Stakes)
	trace.Info(tracer, ctx, "coping.decision",
		slog.String("case", kase.ID),
		slog.String("orchestrator", c.NameStr),
		slog.String("action", string(decision.Action)),
		slog.Float64("confidence", decision.Confidence),
		slog.String("stakes", string(decision.Stakes)),
		slog.String("rationale", decision.Rationale),
	)

	switch decision.Action {
	case cope.Ship:
		inner.CopeAction = string(cope.Ship)
		return inner, nil

	case cope.ShipWithCaveat:
		inner.CopeAction = string(cope.ShipWithCaveat)
		return inner, nil

	case cope.Escalate:
		// Frontier is non-nil here per the EscalateAllowed gate
		// above. Run it.
		frontAns, frontErr := c.Frontier.Answer(ctx, kase)
		if frontErr != nil {
			// Frontier failed; surface the inner answer with a
			// "tried to escalate, failed" tag. Grader still sees
			// inner's extracted answer — at worst we paid a
			// double-call cost for no value.
			trace.Error(tracer, ctx, "coping.frontier_failed",
				slog.String("case", kase.ID),
				slog.String("err", frontErr.Error()),
			)
			inner.CopeAction = string(cope.Escalate) + "-failed"
			return inner, nil
		}
		// Pool tokens: the frontier call is real cost, so add it
		// to the Answer. The Calls counter reflects total inference
		// calls for the case (inner + frontier).
		frontAns.CopeAction = string(cope.Escalate)
		frontAns.Calls += inner.Calls
		frontAns.TokensUsed += inner.TokensUsed
		return frontAns, nil

	case cope.Refuse:
		// Don't ship inner's potentially-wrong answer. Return an
		// empty Answer; bench grader sees Extracted="" and the case
		// fails by extraction. Operators who want a different
		// behavior wire a Refusal handler in a future v3.4.x.
		return benchmark.Answer{
			Raw:        inner.Raw,
			Extracted:  "",
			Calls:      inner.Calls,
			TokensUsed: inner.TokensUsed,
			Confidence: inner.Confidence,
			CopeAction: string(cope.Refuse),
		}, nil
	}

	// Unreachable — every Action is handled above. Belt-and-suspenders.
	inner.CopeAction = string(decision.Action)
	return inner, nil
}
