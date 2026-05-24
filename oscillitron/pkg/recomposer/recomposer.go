// Package recomposer combines a parent plan's resolved children into
// a single composed return_result payload under the uniform-node +
// evaluate/execute model.
//
// The parent plan's emit_subtree output carries a RecomposeSpec that
// tells this package how the N children fold into one:
//
//   - RecomposeSequential — left-to-right fold via a binary reducer.
//     For N children: reduce(reduce(reduce(c0, c1), c2), c3)...
//   - RecomposePairwise   — pair-wise tournament fold via the same
//     binary reducer. For N children: round 1 reduces (c0,c1)(c2,c3)
//     ..., round 2 reduces the round-1 results pairwise, until one
//     remains. For commutative+associative reducers (e.g. Concat),
//     the result is identical to Sequential; the v0 distinction is
//     procedural, not semantic. The split matters for non-commutative
//     reducers (LLM-driven synthesize-these-two-summaries) and
//     paves the way for sibling-parallel hardware in v1.
//   - RecomposeNone       — no recomposition; the parent's caller is
//     expected not to read the returned payload. The recomposer
//     returns a zero ReturnResultPayload and no error.
//
// The "compose playbook" in the v0 playbook set is conceptually a
// dispatched AP that pulls pairs from a parent scope channel. In v0,
// that orchestration is owned by the runner+recomposer pair: the
// runner gathers the children's return_result payloads in the order
// they resolve (which under the locked randomized-sibling-dispatch
// rule is non-deterministic), and the recomposer here supplies the
// fold. A compose-as-a-dispatched-AP path with actual scope channels
// is a v1 concern.
package recomposer

import (
	"context"
	"errors"
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Recomposer collapses N resolved children into a single composed
// return_result payload. Inputs are the children's return_result
// payloads only; emit_subtree and verifier_signal children are not
// directly composable (the runner filters them out before calling
// Recompose).
type Recomposer interface {
	Recompose(ctx context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error)
}

// ErrNoChildren is returned when Recompose is called with an empty
// children slice. The runner short-circuits to avoid this; callers
// constructing Recompose directly should check.
var ErrNoChildren = errors.New("recomposer: no children to recompose")

// ErrUnknownSpec is returned for a RecomposeSpec the recomposer
// doesn't know how to handle.
var ErrUnknownSpec = errors.New("recomposer: unknown recompose spec")

// BinaryReducer combines two return_result payloads into one.
// Implementations are expected to be deterministic so trace
// replay is meaningful.
type BinaryReducer func(left, right session.ReturnResultPayload) session.ReturnResultPayload

// Concat is the v0 sequential/pairwise recomposer. It joins
// children's Result.Content with a Separator, takes the minimum
// confidence (weakest-link semantics), and unions the children's
// signal bundles.
//
// Separator is honored as-is, including the empty string. There is
// no built-in default — callers pick what they want. The package
// exposes DefaultSeparator as the suggested choice.
type Concat struct {
	Separator string
}

// DefaultSeparator is the suggested join string for Concat.
// Callers wanting a familiar default should set
// `Concat{Separator: recomposer.DefaultSeparator}`.
const DefaultSeparator = " | "

// Recompose implements Recomposer.
func (c Concat) Recompose(ctx context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error) {
	if err := ctx.Err(); err != nil {
		return session.ReturnResultPayload{}, err
	}
	switch spec {
	case session.RecomposeNone:
		// No recomposition; caller is expected not to read the payload.
		return session.ReturnResultPayload{}, nil
	case session.RecomposeSequential, session.RecomposePairwise:
		if len(children) == 0 {
			return session.ReturnResultPayload{}, ErrNoChildren
		}
		if len(children) == 1 {
			return children[0], nil
		}
		reducer := c.reduce
		if spec == session.RecomposeSequential {
			return foldSequential(children, reducer), nil
		}
		return foldPairwise(children, reducer), nil
	default:
		return session.ReturnResultPayload{}, fmt.Errorf("%w: %q", ErrUnknownSpec, spec)
	}
}

// reduce is Concat's binary reducer.
func (c Concat) reduce(left, right session.ReturnResultPayload) session.ReturnResultPayload {
	return session.ReturnResultPayload{
		Result: session.Payload{
			Kind:    "result",
			Content: left.Result.Content + c.Separator + right.Result.Content,
		},
		Confidence: minConfidence(left.Confidence, right.Confidence),
		Signals:    mergeSignals(left.Signals, right.Signals),
	}
}

// foldSequential reduces children left-to-right.
func foldSequential(children []session.ReturnResultPayload, reduce BinaryReducer) session.ReturnResultPayload {
	acc := children[0]
	for i := 1; i < len(children); i++ {
		acc = reduce(acc, children[i])
	}
	return acc
}

// foldPairwise reduces children pair-by-pair across rounds until one
// remains. Odd-count rounds pass the trailing element through
// unchanged to the next round.
func foldPairwise(children []session.ReturnResultPayload, reduce BinaryReducer) session.ReturnResultPayload {
	level := append([]session.ReturnResultPayload(nil), children...)
	for len(level) > 1 {
		next := make([]session.ReturnResultPayload, 0, (len(level)+1)/2)
		for i := 0; i+1 < len(level); i += 2 {
			next = append(next, reduce(level[i], level[i+1]))
		}
		if len(level)%2 == 1 {
			// Pass the trailing element through to the next round.
			next = append(next, level[len(level)-1])
		}
		level = next
	}
	return level[0]
}

// minConfidence is the weakest-link confidence aggregator.
func minConfidence(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// mergeSignals unions the children's signal bundles. Contradictions
// and OpenQuestions are concatenated (de-duplication is intentionally
// not done in v0 — the curation layer is the right place for that).
// GroundedPass is the AND of both children, but only when both have
// it set; if either is nil, the merged value is nil too.
func mergeSignals(a, b session.Signals) session.Signals {
	out := session.Signals{
		Contradictions: appendNonNil(a.Contradictions, b.Contradictions),
		OpenQuestions:  appendNonNil(a.OpenQuestions, b.OpenQuestions),
	}
	if a.GroundedPass != nil && b.GroundedPass != nil {
		v := *a.GroundedPass && *b.GroundedPass
		out.GroundedPass = &v
	}
	return out
}

// appendNonNil produces a fresh slice from a + b, returning nil when
// both inputs are empty so callers get omitempty-friendly output.
func appendNonNil(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

var _ Recomposer = Concat{}
