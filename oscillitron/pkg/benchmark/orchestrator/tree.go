// Tree orchestrator wraps the runner+recomposer call tree as a
// benchmark.Orchestrator. The bench's per-case Answer is the result of
// the full Oscillitron orchestration pattern: plan → emit_subtree →
// child APs (process) → synthesize (recompose).
//
// This is the *orchestration arm* of the cross-substrate matrix, distinct
// from:
//   - Single   — frontier baseline, 1 call.
//   - Vote     — N parallel attempts + majority vote.
//   - Coping   — Vote wrapped with confidence-gated escalation.
//
// Tree forces PlaybookPlan on the root so we actually exercise the
// decompose path. Otherwise the model's own Evaluate step would likely
// pick Process on every call (fewer tokens, faster) and Tree would
// degenerate to a single-call Single. The point of this orchestrator
// is to measure decompose+recompose value; we make sure it happens.
//
// Recomposition uses recomposer.Synth backed by the caller-provided
// Synthesizer (typically recomposer.AdapterSynth wrapping the same
// substrate the orchestrator runs on). Each binary reduction is a real
// substrate call that integrates two prior child results — not naive
// string concat. The substrate participates in both divide and conquer.

package orchestrator

import (
	"context"
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/runner"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

// Tree is the call-tree orchestrator. See package doc.
type Tree struct {
	// NameStr appears in benchmark reports. Defaults to "tree".
	NameStr string
	// Adapter runs Evaluate and Execute for every AP in the tree.
	// Required.
	Adapter adapter.Adapter
	// Synthesizer is used by recomposer.Synth to combine pairs of
	// child results into a single synthesized result. Required.
	// Typically recomposer.AdapterSynth wrapping the same Adapter
	// so the substrate participates in synthesis as well as drafting.
	Synthesizer recomposer.Synthesizer
	// Extractor pulls the canonical answer form from the recomposed
	// payload's Result.Content. Required. Use the same extractor as
	// Single / Vote use (letter extraction for MCQ, boxed for math).
	Extractor Extractor
	// Governor optionally coordinates VRAM across all components
	// hitting the same substrate. Forwarded to runner.Config.
	Governor *vram.Governor
	// Tracer emits structured events. Defaults to trace.Discard{}.
	Tracer trace.Tracer
	// MaxDepth caps the path length of the call tree. Defaults to 10.
	// The root plans (depth 0), its children execute or recurse
	// (depth 1), grandchildren only if a child itself plans (depth 2),
	// etc. MaxDepth=10 leaves lots of room for genuinely deep
	// decomposition; bench cases typically won't go that deep
	// because the model decomposes 1-2 levels and stops.
	MaxDepth int
}

// Name implements benchmark.Orchestrator.
func (t Tree) Name() string {
	if t.NameStr != "" {
		return t.NameStr
	}
	return "tree"
}

// Answer implements benchmark.Orchestrator.
func (t Tree) Answer(ctx context.Context, c benchmark.Case) (benchmark.Answer, error) {
	if t.Adapter == nil {
		return benchmark.Answer{}, fmt.Errorf("tree: Adapter is required")
	}
	if t.Synthesizer == nil {
		return benchmark.Answer{}, fmt.Errorf("tree: Synthesizer is required")
	}
	if t.Extractor == nil {
		return benchmark.Answer{}, fmt.Errorf("tree: Extractor is required")
	}

	maxDepth := t.MaxDepth
	if maxDepth == 0 {
		maxDepth = 10
	}

	root := session.NewRoot(
		session.ID(fmt.Sprintf("bench-tree-%s", c.ID)),
		c.Prompt,
		"{answer}",
		classification.Internal,
		session.Budget{TokensRemaining: 64_000, DepthRemaining: maxDepth},
	)
	root.Stakes = c.Stakes
	// PlaybookPlan is forced on the root via the forcePlanOnRoot
	// wrapper below — pre-stamping env.Evaluate doesn't work because
	// the runner always calls adapter.Evaluate, which routinely
	// overrides any pre-stamp.

	cfg := runner.Config{
		Adapter:    forcePlanOnRoot{inner: t.Adapter},
		Recomposer: recomposer.Synth{Synthesizer: t.Synthesizer},
		Tracer:     t.Tracer,
		Governor:   t.Governor,
		MaxDepth:   maxDepth,
	}
	res, err := runner.Run(ctx, cfg, root)
	if err != nil {
		return benchmark.Answer{}, fmt.Errorf("tree: run: %w", err)
	}

	rawContent := res.ResolvedPayload.Result.Content
	extracted := t.Extractor.Extract(rawContent)

	// Calls: Execute counts (one per AP execute) + Evaluate counts
	// (one per AP evaluate). Token tally is harder — RunState doesn't
	// aggregate it across the tree. v0 leaves TokensUsed at 0; cost
	// tracker integration is a follow-up if we need exact accounting.
	calls := res.State.ExecuteCount + res.State.EvaluateCount

	return benchmark.Answer{
		Raw:        rawContent,
		Extracted:  extracted,
		Calls:      calls,
		Confidence: res.ResolvedPayload.Confidence,
	}, nil
}

// Compile-time check.
var _ benchmark.Orchestrator = Tree{}

// forcePlanOnRoot wraps an adapter so the root AP's Evaluate step
// produces PlaybookPlan unconditionally. Pre-stamping env.Evaluate
// on the root before runner.Run doesn't work — the runner always
// calls adapter.Evaluate, and adapters routinely overwrite the
// pre-stamp with their own playbook pick.
//
// This wrapper intercepts only the root's Evaluate (ParentID == nil)
// and pins it to Plan. Child APs delegate to the inner adapter so
// the inner Evaluator decides plan vs. process vs. compose etc.
// naturally as the tree descends.
type forcePlanOnRoot struct {
	inner adapter.Adapter
}

func (f forcePlanOnRoot) Name() string { return f.inner.Name() }

func (f forcePlanOnRoot) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if env.ParentID == nil {
		env.Evaluate = &session.Evaluate{
			Playbook:   session.PlaybookPlan,
			Confidence: 1.0,
		}
		return env, nil
	}
	return f.inner.Evaluate(ctx, env)
}

func (f forcePlanOnRoot) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	return f.inner.Execute(ctx, env)
}

var _ adapter.Adapter = forcePlanOnRoot{}
