package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

const goalExtractionPrompt = `Describe the intent of the following prompt. Do not solve it.`

const extractionPreamble = `Extract the final answer from the response below. Respond with ONLY the extracted answer — nothing else.

`

// rawCall tries adapter.RawCaller first (natural text, no playbook
// wrapping). Falls back to a string-return helper if the adapter
// only implements the standard Evaluate/Execute interface.
func rawCall(ctx context.Context, a adapter.Adapter, prompt string) (string, error) {
	if rc, ok := a.(adapter.RawCaller); ok {
		return rc.RawCall(ctx, prompt)
	}
	return "", fmt.Errorf("adapter %q does not implement RawCaller", a.Name())
}

// DeriveGoal asks the LLM what the goal of the prompt is.
// Returns empty string on error (non-fatal).
func DeriveGoal(ctx context.Context, a adapter.Adapter, tracer trace.Tracer, c benchmark.Case) string {
	if tracer == nil {
		tracer = trace.Discard{}
	}
	start := time.Now()

	prompt := goalExtractionPrompt + "\n\n---\n\n" + c.Prompt
	result, err := rawCall(ctx, a, prompt)
	if err != nil {
		trace.Error(tracer, ctx, "extractor.goal_derive_error",
			slog.String("case", c.ID),
			slog.String("err", err.Error()),
		)
		return ""
	}

	goal := strings.TrimSpace(result)
	if goal == "" {
		trace.Error(tracer, ctx, "extractor.goal_derive_error",
			slog.String("case", c.ID),
			slog.String("err", "empty goal"),
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
// a raw model response, guided by a goal. Implements Extractor.
// Does NOT acquire a governor lease — the calling orchestrator
// already holds one.
type LLMExtractor struct {
	Adapter adapter.Adapter
	Tracer  trace.Tracer
}

var _ Extractor = LLMExtractor{}

func (e LLMExtractor) Extract(ctx context.Context, goal string, raw string) string {
	tracer := e.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}
	start := time.Now()

	prompt := extractionPreamble + goal + "\n\n[RESPONSE]\n" + raw
	result, err := rawCall(ctx, e.Adapter, prompt)
	if err != nil {
		trace.Error(tracer, ctx, "extractor.llm_extract_error",
			slog.String("err", err.Error()),
		)
		return ""
	}

	extracted := strings.TrimSpace(result)

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
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	return extracted
}
