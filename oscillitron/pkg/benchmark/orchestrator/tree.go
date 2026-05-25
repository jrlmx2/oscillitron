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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	// TraceDir, when non-empty, writes a per-case tree trace file
	// to this directory. Each file is <case-id>.tree.txt and
	// contains the full prompt→response path through the tree.
	TraceDir string
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

	goal, err := t.deriveGoal(ctx, c)
	if err != nil {
		goal = ""
	}

	outputSchema := goal
	if outputSchema == "" {
		outputSchema = "{answer}"
	}

	root := session.NewRoot(
		session.ID(fmt.Sprintf("bench-tree-%s", c.ID)),
		c.Prompt,
		outputSchema,
		classification.Internal,
		session.Budget{TokensRemaining: 64_000, DepthRemaining: maxDepth},
	)
	root.Stakes = c.Stakes

	cfg := runner.Config{
		Adapter:    treeAdapter{inner: t.Adapter, originalTask: c.Prompt},
		Recomposer: recomposer.Synth{Synthesizer: t.Synthesizer, Goal: goal, OriginalTask: c.Prompt},
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
	calls := res.State.ExecuteCount + res.State.EvaluateCount

	t.emitTreeTrace(ctx, c, goal, res, extracted)

	return benchmark.Answer{
		Raw:        rawContent,
		Extracted:  extracted,
		Calls:      calls,
		Confidence: res.ResolvedPayload.Confidence,
	}, nil
}

func (t Tree) emitTreeTrace(ctx context.Context, c benchmark.Case, goal string, res runner.Result, extracted string) {
	tt := BuildTreeTrace(c.ID, c.Expected, goal, res, extracted)
	rendered := tt.RenderText()
	tracer := t.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}
	trace.Info(tracer, ctx, "tree.trace",
		slog.String("case", c.ID),
		slog.String("trace", rendered),
	)
	if t.TraceDir != "" {
		path := filepath.Join(t.TraceDir, c.ID+".tree.txt")
		_ = os.WriteFile(path, []byte(rendered), 0644)
	}
}

// deriveGoal extracts a freeform goal statement from the input.
// Mechanical patterns are tried first (cheap, reliable). LLM
// fallback only when no pattern matches.
func (t Tree) deriveGoal(ctx context.Context, c benchmark.Case) (string, error) {
	if g := extractGoalMechanical(c.Prompt); g != "" {
		return g, nil
	}
	return t.deriveGoalLLM(ctx, c)
}

// extractGoalMechanical scans for common answer-instruction patterns.
// Returns empty string when no pattern matches.
func extractGoalMechanical(prompt string) string {
	lower := strings.ToLower(prompt)
	patterns := []struct {
		contains string
		goal     string
	}{
		{"answer with a single letter", "Your final answer must be exactly one letter: A, B, C, or D. Nothing else."},
		{"answer with the letter", "Your final answer must be exactly one letter: A, B, C, or D. Nothing else."},
		{"choose one of the following", "Your final answer must be exactly one letter: A, B, C, or D. Nothing else."},
		{"\\boxed{", "Your final answer must be a mathematical expression inside \\boxed{}."},
		{"what is the value", "Your final answer must be a single numerical value."},
	}
	for _, p := range patterns {
		if strings.Contains(lower, p.contains) {
			return p.goal
		}
	}
	return ""
}

func (t Tree) deriveGoalLLM(ctx context.Context, c benchmark.Case) (string, error) {
	env := session.NewRoot(
		session.ID(fmt.Sprintf("bench-tree-%s-goal", c.ID)),
		goalExtractionPrompt+"\n\n---\n\n"+c.Prompt,
		"",
		classification.Internal,
		session.Budget{TokensRemaining: 2_000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{
		Playbook:   session.PlaybookProcess,
		Confidence: 1.0,
	}
	out, err := t.Adapter.Execute(ctx, env)
	if err != nil {
		return "", err
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		return "", fmt.Errorf("goal extraction returned no result")
	}
	return out.Execute.ReturnResult.Result.Content, nil
}

const goalExtractionPrompt = `You are a FORMAT DETECTOR. You do NOT solve tasks.

Given the task below, state in ONE sentence what FORMAT the final answer must be in.

GOOD examples of format descriptions:
- "The answer must be exactly one letter: A, B, C, or D."
- "The answer must be a number in electron-volts."
- "The answer must be a short paragraph."

BAD examples (these solve the task — NEVER do this):
- "A" (this is solving the MCQ)
- "10^-4 eV" (this is computing the answer)
- "The reaction produces 11 carbon atoms" (this is answering the question)

Describe ONLY the format. Do NOT reason about the content.`

// Compile-time check.
var _ benchmark.Orchestrator = Tree{}

// treeAdapter wraps an adapter with tree-specific behavior:
//   - Forces PlaybookPlan on the root's Evaluate step.
//   - Gives every child AP lineage back to the original question
//     so children reason in context rather than guessing in isolation.
type treeAdapter struct {
	inner        adapter.Adapter
	originalTask string // the full original prompt
}

func (a treeAdapter) Name() string { return a.inner.Name() }

func (a treeAdapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if env.ParentID == nil {
		env.Evaluate = &session.Evaluate{
			Playbook:   session.PlaybookPlan,
			Confidence: 1.0,
		}
		return env, nil
	}
	return a.inner.Evaluate(ctx, env)
}

func (a treeAdapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if a.originalTask != "" && env.ParentID != nil {
		env.Input.Content = childPrompt(a.originalTask, env.Input.Content)
	}
	out, err := a.inner.Execute(ctx, env)
	if err != nil {
		return out, err
	}
	if out.Execute != nil && out.Execute.EmitSubtree != nil &&
		out.Execute.EmitSubtree.Recompose == session.RecomposeNone &&
		len(out.Execute.EmitSubtree.SubAPs) > 0 {
		out.Execute.EmitSubtree.Recompose = session.RecomposeSequential
	}
	return out, nil
}

const childPreamble = `You are working on a sub-part of a larger question. ` +
	`Your job is to reason about your specific sub-task and explain your findings. ` +
	`State what you concluded and WHY — show your work. ` +
	`If your findings suggest an answer to the original question, say which and why.

`

func childPrompt(originalTask, subtask string) string {
	return childPreamble +
		"[ORIGINAL QUESTION]\n" + originalTask + "\n\n" +
		"[YOUR SUB-TASK]\n" + subtask
}

var _ adapter.Adapter = treeAdapter{}
