// Package orchestrator implements benchmark.Orchestrator strategies.
//
//   - Single: one adapter call per case. The frontier-baseline arm.
//   - Vote:   N adapter calls + majority vote on the extracted
//     answer. The orchestration arm.
//
// Both consume an adapter.Adapter directly (single Execute call per
// attempt, PlaybookProcess hardcoded) — they don't engage pkg/runner's
// call-tree machinery. Engaging the full runner would mean plan →
// process × N → critique → compose for every benchmark question, which
// doesn't match how benchmark questions look. The runner's value is in
// open-ended workloads (email drafting, agent tasks); for closed-form
// benchmarks the right shape is direct adapter calls with simple
// aggregation.
//
// VRAM coordination: both orchestrators take an optional *vram.Governor.
// When set, every adapter call Acquires a lease so concurrent attempts
// (vote) and concurrent components (orchestrator + grader sharing the
// same substrate) share one budget.
package orchestrator

import (
	"context"
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

// Extractor turns a raw model response into the canonical answer
// form (letter for MCQ, number for math, free text for HLE). Used by
// Vote to count votes and by Single to populate Answer.Extracted
// when set.
type Extractor interface {
	Extract(ctx context.Context, goal string, raw string) string
}

// ExtractorFunc adapts a plain function to the Extractor interface.
type ExtractorFunc func(context.Context, string, string) string

// Extract implements Extractor.
func (f ExtractorFunc) Extract(ctx context.Context, goal, raw string) string {
	return f(ctx, goal, raw)
}

// Single calls the adapter once per case. Wire with a frontier model
// (Sonnet/Opus) for the baseline arm of a benchmark — the yardstick
// the orchestration arm (Vote) is measured against.
type Single struct {
	// NameStr appears in benchmark reports (e.g., "sonnet-baseline").
	// Required.
	NameStr string
	// Adapter is the substrate. Required.
	Adapter adapter.Adapter
	// Extractor optionally extracts a canonical answer form from the
	// raw response. When nil, Answer.Extracted equals Answer.Raw and
	// the Grader does any extraction. When set, the Grader can
	// compare Answer.Extracted directly to Case.Expected.
	Extractor Extractor
	// Governor optionally coordinates VRAM with other components
	// hitting the same substrate.
	Governor *vram.Governor
}

// Name implements benchmark.Orchestrator.
func (s Single) Name() string { return s.NameStr }

// Answer implements benchmark.Orchestrator.
func (s Single) Answer(ctx context.Context, c benchmark.Case) (benchmark.Answer, error) {
	if s.Adapter == nil {
		return benchmark.Answer{}, fmt.Errorf("single: Adapter is required")
	}
	lease, err := s.Governor.Acquire(ctx)
	if err != nil {
		return benchmark.Answer{}, fmt.Errorf("single: governor acquire: %w", err)
	}
	defer lease.Release()

	env := session.NewRoot(
		session.ID("bench-"+c.ID),
		c.Prompt,
		"{answer}",
		classification.Internal,
		session.Budget{TokensRemaining: 32_000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{
		Playbook:   session.PlaybookProcess,
		Confidence: 1.0,
	}
	out, err := s.Adapter.Execute(ctx, env)
	if err != nil {
		return benchmark.Answer{}, fmt.Errorf("single: execute: %w", err)
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		return benchmark.Answer{}, fmt.Errorf("single: empty return_result")
	}
	raw := out.Execute.ReturnResult.Result.Content
	extracted := raw
	if s.Extractor != nil {
		extracted = s.Extractor.Extract(ctx, c.Goal, raw)
	}
	return benchmark.Answer{
		Raw:        raw,
		Extracted:  extracted,
		Calls:      1,
		TokensUsed: out.Execute.TokensUsed,
		Confidence: out.Execute.ReturnResult.Confidence,
	}, nil
}

// Compile-time check.
var _ benchmark.Orchestrator = Single{}
