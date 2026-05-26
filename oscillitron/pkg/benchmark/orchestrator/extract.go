package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

// goalExtractionPrompt is the system-level instruction for DeriveGoal.
// It forces the model to describe the answer FORMAT without solving
// the task — the goal is consumed downstream by LLMExtractor to pull
// the canonical answer from model output.
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

// extractionPrompt is the system-level instruction for LLMExtractor.
// It takes a GOAL (from DeriveGoal) and a RESPONSE (from the
// orchestrator) and extracts the canonical answer.
const extractionPreamble = `Extract the final answer from the response below. Respond with ONLY the extracted answer — nothing else.

`

// DeriveGoal makes a one-shot LLM call to extract a format
// description from the case prompt. Returns empty string on error
// (non-fatal — callers should proceed without a goal rather than
// abort the benchmark).
func DeriveGoal(ctx context.Context, a adapter.Adapter, tracer trace.Tracer, c benchmark.Case) string {
	if tracer == nil {
		tracer = trace.Discard{}
	}
	start := time.Now()

	env := session.NewRoot(
		session.ID(fmt.Sprintf("bench-%s-goal", c.ID)),
		goalExtractionPrompt+"\n\n---\n\n"+c.Prompt,
		"",
		classification.Internal,
		session.Budget{TokensRemaining: 2000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{
		Playbook:   session.PlaybookProcess,
		Confidence: 1.0,
	}

	out, err := a.Execute(ctx, env)
	if err != nil {
		trace.Error(tracer, ctx, "extractor.goal_derive_error",
			slog.String("case", c.ID),
			slog.String("err", err.Error()),
		)
		return ""
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		trace.Error(tracer, ctx, "extractor.goal_derive_error",
			slog.String("case", c.ID),
			slog.String("err", "empty return_result"),
		)
		return ""
	}

	goal := strings.TrimSpace(out.Execute.ReturnResult.Result.Content)
	if goal == "" {
		trace.Error(tracer, ctx, "extractor.goal_derive_error",
			slog.String("case", c.ID),
			slog.String("err", "empty goal after parsing"),
		)
		return ""
	}

	trace.Info(tracer, ctx, "extractor.goal_derived",
		slog.String("case", c.ID),
		slog.String("goal", goal),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	return goal
}

// LLMExtractor uses an LLM call to extract the canonical answer from
// a raw model response, guided by a goal (format description).
// Implements the Extractor interface.
type LLMExtractor struct {
	Adapter  adapter.Adapter
	Tracer   trace.Tracer
	Governor *vram.Governor
}

// Compile-time check.
var _ Extractor = LLMExtractor{}

// Extract implements Extractor. Makes one LLM call per invocation.
// Returns empty string on error (non-fatal).
func (e LLMExtractor) Extract(ctx context.Context, goal string, raw string) string {
	tracer := e.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}
	start := time.Now()

	lease, err := e.Governor.Acquire(ctx)
	if err != nil {
		trace.Error(tracer, ctx, "extractor.llm_extract_error",
			slog.String("err", fmt.Sprintf("governor acquire: %v", err)),
		)
		return ""
	}
	defer lease.Release()

	prompt := extractionPreamble + goal + "\n\n[RESPONSE]\n" + raw
	env := session.NewRoot(
		session.ID("extract"),
		prompt,
		"",
		classification.Internal,
		session.Budget{TokensRemaining: 2000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{
		Playbook:   session.PlaybookProcess,
		Confidence: 1.0,
	}

	out, err := e.Adapter.Execute(ctx, env)
	if err != nil {
		trace.Error(tracer, ctx, "extractor.llm_extract_error",
			slog.String("err", err.Error()),
		)
		return ""
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		trace.Error(tracer, ctx, "extractor.llm_extract_error",
			slog.String("err", "empty return_result"),
		)
		return ""
	}

	extracted := strings.TrimSpace(out.Execute.ReturnResult.Result.Content)
	confidence := out.Execute.ReturnResult.Confidence

	truncated := raw
	if len(truncated) > 200 {
		truncated = truncated[:200]
	}

	if extracted == "" {
		trace.Info(tracer, ctx, "extractor.extract_empty",
			slog.String("goal", goal),
			slog.String("raw_truncated", truncated),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
		return ""
	}

	trace.Info(tracer, ctx, "extractor.llm_extract",
		slog.String("goal", goal),
		slog.String("raw_truncated", truncated),
		slog.String("extracted", extracted),
		slog.Float64("confidence", confidence),
		slog.Int("tokens_used", out.Execute.TokensUsed),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	return extracted
}
