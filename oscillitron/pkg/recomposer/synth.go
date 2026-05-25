// LLM-driven Recomposer variant.
//
// Concat's binary reducer is text-level: it joins two payloads with a
// separator. That is fine for trace-fidelity and for workloads where
// the children are already a coherent answer per se. It is *not* the
// shape that produces good prose, code, or analysis from a multi-step
// plan — the parts need to be re-said together, not just stapled.
//
// Synth plugs an LLM-shaped synthesizer into the same BinaryReducer
// seam Concat uses. Sequential and Pairwise folds work identically;
// the only difference is what happens when two payloads collapse into
// one. The Synthesizer interface is deliberately narrow (Synthesize on
// a pair, returning one) so a real implementation can call into a
// frontier API, an adapter.Adapter pointed at a Hermes specialist, or
// any other synthesis surface without the recomposer needing to know.
//
// Stub implementation ships in this file for tests and the demo; a
// real frontier-backed Synthesizer is the natural follow-up — same
// pattern as judge.Judge / pkg/judge (stub + interface here; substrate
// piece next).
package recomposer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// SynthesizeRequest carries the inputs to one binary synthesis. The
// Synthesizer is expected to read Left + Right's Result.Content and
// produce a single coherent payload. Signals carry through via
// MergeSignals on the Synth side (same semantics as Concat's mergeSignals).
type SynthesizeRequest struct {
	// Left and Right are the two payloads being collapsed into one.
	// Order matters for non-commutative synthesizers (e.g. one that
	// treats Left as "prior context" and Right as "new contribution").
	Left, Right session.ReturnResultPayload
	// RecomposeSpec is the spec the parent plan emitted. Useful when a
	// synthesizer wants to distinguish "we are folding a long chain
	// sequentially" from "we are folding two earlier reductions
	// pairwise" for prompt construction.
	RecomposeSpec session.RecomposeSpec
	// StepIndex is 0-based: which reduction in the current fold this
	// is. For an N-child sequential fold, StepIndex runs 0..N-2; for
	// pairwise it counts every individual pair across all rounds.
	StepIndex int
	// Goal is a freeform natural-language statement of what the
	// tree's final output should look like. Derived from the input
	// by the Tree orchestrator before planning. Synthesizers use
	// this as the last directive so the recomposed output matches
	// what the original prompt asked for.
	Goal string
}

// SynthesizeResponse is the synthesizer's output. The recomposer
// builds the resulting ReturnResultPayload by taking Content as the
// merged result content, Confidence as the merged confidence, and
// applying MergeSignals to the two inputs' Signals bundles. The
// synthesizer can override the merged confidence via Confidence (set
// >0); leaving it at zero falls back to the weakest-link min().
type SynthesizeResponse struct {
	// Content is the combined Result.Content the recomposer publishes.
	Content string
	// Kind defaults to "result" when empty; synthesizers may set
	// a domain-specific kind (e.g. "summary", "diff").
	Kind string
	// Confidence is the synthesizer's confidence in the combined
	// payload. <=0 means "fall back to weakest-link min".
	Confidence float64
	// TokensUsed is the cost-bookkeeping handle for the call.
	TokensUsed int
}

// Synthesizer is the narrow binary-merge interface. A real
// implementation calls a frontier (or local) model with the two input
// payloads and asks for a coherent merger. Implementations should be
// safe for concurrent use; the v0 runner is sync but a future
// async runtime may share one Synthesizer across siblings.
type Synthesizer interface {
	// Name identifies the synthesizer in logs and the cost tracker.
	Name() string
	// Synthesize collapses two payloads into one combined payload.
	// Errors propagate through Recompose and surface to the runner,
	// which marks the plan as inhibited (same as a recompose error
	// from any other Recomposer).
	Synthesize(ctx context.Context, req SynthesizeRequest) (SynthesizeResponse, error)
}

// Synth is the LLM-driven Recomposer. It uses a Synthesizer as the
// binary reducer behind foldSequential / foldPairwise. None,
// Sequential, and Pairwise specs are honored with the same semantics
// as Concat — only the per-reduction merger differs.
type Synth struct {
	// Synthesizer performs each binary merge. Required.
	Synthesizer Synthesizer
	// Goal is threaded into every SynthesizeRequest so the
	// synthesizer knows what the final output should look like.
	// Set per-case by the Tree orchestrator.
	Goal string
}

// ErrSynthesizerRequired is returned by Recompose when Synth has no
// Synthesizer wired.
var ErrSynthesizerRequired = errors.New("recomposer: Synth.Synthesizer is required")

// Recompose implements Recomposer.
//
// The synthesizer is invoked once per pairwise reduction (N-1 times
// for a Sequential fold over N children; ⌈log2(N)⌉ rounds for
// Pairwise but still N-1 total calls — the trailing-element pass
// in odd rounds does not call the synthesizer). Each call carries a
// SynthesizeRequest with the spec and a 0-based step index so a
// synthesizer can craft prompts that know where they are in the fold.
//
// Context cancellation propagates from the runner: a cancellation
// between steps returns ctx.Err() with whatever payload has been
// accumulated so far thrown away (no partial recomposition).
func (s Synth) Recompose(ctx context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error) {
	if err := ctx.Err(); err != nil {
		return session.ReturnResultPayload{}, err
	}
	if s.Synthesizer == nil {
		return session.ReturnResultPayload{}, ErrSynthesizerRequired
	}
	switch spec {
	case session.RecomposeNone:
		return session.ReturnResultPayload{}, nil
	case session.RecomposeSequential, session.RecomposePairwise:
		if len(children) == 0 {
			return session.ReturnResultPayload{}, ErrNoChildren
		}
		if len(children) == 1 {
			return children[0], nil
		}
		if s.allShort(children) {
			return s.selectByConfidence(children), nil
		}
		return s.fold(ctx, spec, children)
	default:
		return session.ReturnResultPayload{}, fmt.Errorf("%w: %q", ErrUnknownSpec, spec)
	}
}

// SelectionThreshold is the max content length (chars) for a child
// to be considered a "short answer." When ALL children are short,
// Synth switches from synthesis (merge content) to selection (pick
// highest confidence). This avoids the nonsensical LLM fold when
// children produce competing final answers like "A" vs "C".
const SelectionThreshold = 50

func (s Synth) allShort(children []session.ReturnResultPayload) bool {
	for _, c := range children {
		if len(strings.TrimSpace(c.Result.Content)) > SelectionThreshold {
			return false
		}
	}
	return true
}

func (s Synth) selectByConfidence(children []session.ReturnResultPayload) session.ReturnResultPayload {
	best := 0
	for i := 1; i < len(children); i++ {
		if children[i].Confidence > children[best].Confidence {
			best = i
		}
	}
	return children[best]
}

// fold drives the synthesizer through the requested fold shape. We
// thread ctx + the per-step counter through a custom variant of the
// foldSequential / foldPairwise helpers since the BinaryReducer type
// has no error channel — synth calls can fail.
func (s Synth) fold(ctx context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error) {
	step := 0
	reduce := func(left, right session.ReturnResultPayload) (session.ReturnResultPayload, error) {
		if err := ctx.Err(); err != nil {
			return session.ReturnResultPayload{}, err
		}
		req := SynthesizeRequest{
			Left:          left,
			Right:         right,
			RecomposeSpec: spec,
			StepIndex:     step,
			Goal:          s.Goal,
		}
		step++
		resp, err := s.Synthesizer.Synthesize(ctx, req)
		if err != nil {
			return session.ReturnResultPayload{}, fmt.Errorf("recomposer: synthesize step %d: %w", req.StepIndex, err)
		}
		conf := resp.Confidence
		if conf <= 0 {
			conf = minConfidence(left.Confidence, right.Confidence)
		}
		kind := resp.Kind
		if kind == "" {
			kind = "result"
		}
		return session.ReturnResultPayload{
			Result:     session.Payload{Kind: kind, Content: resp.Content},
			Confidence: conf,
			Signals:    mergeSignals(left.Signals, right.Signals),
		}, nil
	}
	if spec == session.RecomposeSequential {
		return foldSequentialErr(children, reduce)
	}
	return foldPairwiseErr(children, reduce)
}

// foldSequentialErr is foldSequential's error-returning twin.
func foldSequentialErr(children []session.ReturnResultPayload, reduce func(l, r session.ReturnResultPayload) (session.ReturnResultPayload, error)) (session.ReturnResultPayload, error) {
	acc := children[0]
	for i := 1; i < len(children); i++ {
		next, err := reduce(acc, children[i])
		if err != nil {
			return session.ReturnResultPayload{}, err
		}
		acc = next
	}
	return acc, nil
}

// foldPairwiseErr is foldPairwise's error-returning twin.
func foldPairwiseErr(children []session.ReturnResultPayload, reduce func(l, r session.ReturnResultPayload) (session.ReturnResultPayload, error)) (session.ReturnResultPayload, error) {
	level := append([]session.ReturnResultPayload(nil), children...)
	for len(level) > 1 {
		next := make([]session.ReturnResultPayload, 0, (len(level)+1)/2)
		for i := 0; i+1 < len(level); i += 2 {
			merged, err := reduce(level[i], level[i+1])
			if err != nil {
				return session.ReturnResultPayload{}, err
			}
			next = append(next, merged)
		}
		if len(level)%2 == 1 {
			next = append(next, level[len(level)-1])
		}
		level = next
	}
	return level[0], nil
}

var _ Recomposer = Synth{}
