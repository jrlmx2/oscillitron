# LLM-Based Generic Answer Extraction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-benchmark regex extractors with a single LLM extraction call that works generically across all benchmark formats.

**Architecture:** Goal derivation moves from the tree orchestrator to the benchmark runner (one call per case). A new `LLMExtractor` makes a one-shot adapter call with the goal + response to extract the answer. The `Extractor` interface gains `context.Context` and `goal string` parameters. All orchestrators use the same extractor.

**Tech Stack:** Go, existing `adapter.Adapter` / `session.Envelope` / `trace.Tracer` machinery. No new dependencies.

---

### Task 1: Change the Extractor interface

**Files:**
- Modify: `pkg/benchmark/orchestrator/single.go:37-45`

- [ ] **Step 1: Write the failing test**

Create a test that constructs an `ExtractorFunc` with the new 3-arg signature and calls it.

```go
// In pkg/benchmark/orchestrator/single_test.go — add at bottom:

func TestExtractorFunc_NewSignature(t *testing.T) {
	ext := ExtractorFunc(func(ctx context.Context, goal, raw string) string {
		return goal + ":" + raw
	})
	got := ext.Extract(context.Background(), "letter", "A")
	if got != "letter:A" {
		t.Errorf("got %q, want %q", got, "letter:A")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run TestExtractorFunc_NewSignature -v`
Expected: Compilation error — `ExtractorFunc` has wrong signature.

- [ ] **Step 3: Update the interface and ExtractorFunc**

In `pkg/benchmark/orchestrator/single.go`, replace lines 37-45:

```go
// Extractor turns a raw model response into the canonical answer
// form. The goal describes the expected output format (derived once
// per case by the benchmark runner). ctx carries trace correlation.
type Extractor interface {
	Extract(ctx context.Context, goal string, raw string) string
}

// ExtractorFunc adapts a plain function to the Extractor interface.
type ExtractorFunc func(context.Context, string, string) string

// Extract implements Extractor.
func (f ExtractorFunc) Extract(ctx context.Context, goal, raw string) string { return f(ctx, goal, raw) }
```

Add `"context"` to the imports if not already present (it is — used by `Answer`).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run TestExtractorFunc_NewSignature -v`
Expected: PASS

- [ ] **Step 5: DO NOT commit yet** — the codebase won't compile because callers use the old signature. Task 2 fixes them.

---

### Task 2: Update all Extractor call sites

**Files:**
- Modify: `pkg/benchmark/orchestrator/single.go:100-102` (Single.Answer)
- Modify: `pkg/benchmark/orchestrator/vote.go:153,182` (Vote.Answer — two call sites)
- Modify: `pkg/benchmark/orchestrator/tree.go:133` (Tree.Answer)
- Modify: `pkg/benchmark/orchestrator/single_test.go` (all ExtractorFunc constructors)
- Modify: `pkg/benchmark/orchestrator/vote_test.go` (all ExtractorFunc constructors)
- Modify: `pkg/benchmark/orchestrator/tree_test.go` (letterExtractor + all ExtractorFunc constructors)
- Modify: `cmd/bench/main.go:781-799` (buildBenchmark)

- [ ] **Step 1: Update Single.Answer**

In `pkg/benchmark/orchestrator/single.go`, replace lines 100-102:

```go
	extracted := raw
	if s.Extractor != nil {
		extracted = s.Extractor.Extract(ctx, c.Goal, raw)
	}
```

- [ ] **Step 2: Update Vote.Answer — both call sites**

In `pkg/benchmark/orchestrator/vote.go`, line 153 (inside the goroutine):

```go
			extracted := v.Extractor.Extract(aCtx, c.Goal, results[i].raw)
```

Line 182 (in the tally loop):

```go
		extracted := v.Extractor.Extract(ctx, c.Goal, r.raw)
```

- [ ] **Step 3: Update Tree.Answer**

In `pkg/benchmark/orchestrator/tree.go`, line 133:

```go
	extracted := t.Extractor.Extract(ctx, c.Goal, rawContent)
```

- [ ] **Step 4: Add Goal field to benchmark.Case**

In `pkg/benchmark/benchmark.go`, add after the `Stakes` field (line 58):

```go
	// Goal is a one-sentence description of the expected answer format,
	// derived by the benchmark runner via LLM call before orchestrators
	// run. Used by the Extractor to pull the canonical answer from
	// model output. Empty when goal derivation is not wired.
	Goal string
```

- [ ] **Step 5: Update all test ExtractorFunc constructors**

Every test that constructs `ExtractorFunc(func(raw string) string { ... })` needs the new signature. The pattern is mechanical — add `_ context.Context, _ string,` as the first two params. Apply to:

In `pkg/benchmark/orchestrator/single_test.go` line 41:
```go
	ext := ExtractorFunc(func(_ context.Context, _, raw string) string {
```

In `pkg/benchmark/orchestrator/vote_test.go`, every `ExtractorFunc` constructor (lines 108, 117, 136, 161, 175, 203, 218, 234, 247, 260):
```go
	ext := ExtractorFunc(func(_ context.Context, _, raw string) string {
```

In `pkg/benchmark/orchestrator/tree_test.go`, the `letterExtractor` function (line 16):
```go
func letterExtractor() Extractor {
	return ExtractorFunc(func(_ context.Context, _, raw string) string {
```

- [ ] **Step 6: Update cmd/bench buildBenchmark**

In `cmd/bench/main.go`, update lines 781-799. Each `ExtractorFunc` wrapper gets the new signature:

```go
		Extractor: orchestrator.ExtractorFunc(func(_ context.Context, _, raw string) string {
			return grader.ExtractLetter(raw, grader.MultichoiceLetters)
		}),
```

```go
		Extractor: orchestrator.ExtractorFunc(func(_ context.Context, _, raw string) string {
			return grader.ExtractLetter(raw, letters)
		}),
```

```go
		Extractor: orchestrator.ExtractorFunc(func(_ context.Context, _, raw string) string {
			return grader.ExtractBoxed(raw)
		}),
```

- [ ] **Step 7: Verify full build + tests pass**

Run: `cd oscillitron && go build ./... && go test ./... -count=1`
Expected: All pass. The interface changed but every call site is updated.

- [ ] **Step 8: Commit**

```bash
cd oscillitron && git add pkg/benchmark/benchmark.go pkg/benchmark/orchestrator/single.go pkg/benchmark/orchestrator/single_test.go pkg/benchmark/orchestrator/vote.go pkg/benchmark/orchestrator/vote_test.go pkg/benchmark/orchestrator/tree.go pkg/benchmark/orchestrator/tree_test.go cmd/bench/main.go
git commit -m "$(cat <<'EOF'
refactor(extractor): add ctx + goal params to Extractor interface

Prepares for LLM-based extraction. The Extractor interface gains
context.Context (for adapter calls + trace correlation) and a goal
string (format description derived per case). All call sites and
tests updated mechanically — behavior unchanged.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Implement DeriveGoal and LLMExtractor

**Files:**
- Create: `pkg/benchmark/orchestrator/extract.go`
- Create: `pkg/benchmark/orchestrator/extract_test.go`

- [ ] **Step 1: Write the failing test for DeriveGoal**

```go
// pkg/benchmark/orchestrator/extract_test.go
package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

func TestDeriveGoal_ReturnsFormatDescription(t *testing.T) {
	// scriptAdapter returns the goal extraction prompt's answer.
	a := &scriptAdapter{answers: []string{
		`{"response":"The answer must be exactly one letter: A, B, C, or D.","confidence":1.0}`,
	}}
	c := benchmark.Case{ID: "test-001", Prompt: "Question: What is X?\nA) foo\nB) bar\nC) baz\nD) qux\nAnswer with a single letter."}
	goal := DeriveGoal(context.Background(), a, trace.Discard{}, c)
	if goal == "" {
		t.Fatal("DeriveGoal returned empty string")
	}
	// The goal should mention letters — it's a format description, not an answer.
	if !strings.Contains(strings.ToLower(goal), "letter") {
		t.Logf("goal = %q (doesn't mention 'letter' but may still be valid)", goal)
	}
}

func TestDeriveGoal_ErrorReturnsEmpty(t *testing.T) {
	a := &scriptAdapter{err: context.DeadlineExceeded}
	c := benchmark.Case{ID: "test-err", Prompt: "anything"}
	goal := DeriveGoal(context.Background(), a, trace.Discard{}, c)
	if goal != "" {
		t.Errorf("expected empty goal on error, got %q", goal)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run TestDeriveGoal -v`
Expected: Compilation error — `DeriveGoal` not defined.

- [ ] **Step 3: Implement DeriveGoal**

```go
// pkg/benchmark/orchestrator/extract.go
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

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

// DeriveGoal makes a one-shot LLM call to extract a format description
// from the case prompt. Returns empty string on error (non-fatal).
func DeriveGoal(ctx context.Context, a adapter.Adapter, tracer trace.Tracer, c benchmark.Case) string {
	start := time.Now()
	env := session.NewRoot(
		session.ID(fmt.Sprintf("bench-%s-goal", c.ID)),
		goalExtractionPrompt+"\n\n---\n\n"+c.Prompt,
		"",
		classification.Internal,
		session.Budget{TokensRemaining: 2_000, DepthRemaining: 1},
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
	raw := out.Execute.ReturnResult.Result.Content

	// The adapter may return JSON (structured output) or plain text.
	// Try to parse {"response":"..."} and fall back to raw text.
	goal := parseGoalResponse(raw)

	trace.Info(tracer, ctx, "extractor.goal_derived",
		slog.String("case", c.ID),
		slog.String("goal", goal),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	return goal
}

func parseGoalResponse(raw string) string {
	var obj struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err == nil && obj.Response != "" {
		return obj.Response
	}
	return raw
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run TestDeriveGoal -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for LLMExtractor**

Add to `pkg/benchmark/orchestrator/extract_test.go`:

```go
func TestLLMExtractor_ExtractsAnswer(t *testing.T) {
	// The adapter returns a JSON extraction response.
	a := &scriptAdapter{answers: []string{
		`{"response":"{\"extracted\":\"C\",\"confidence\":0.95}","confidence":1.0}`,
	}}
	ext := LLMExtractor{Adapter: a, Tracer: trace.Discard{}}
	got := ext.Extract(context.Background(), "a single letter A-D", "I think it must be C because...")
	if got != "C" {
		t.Errorf("Extract = %q, want %q", got, "C")
	}
}

func TestLLMExtractor_ReturnsEmptyOnError(t *testing.T) {
	a := &scriptAdapter{err: context.DeadlineExceeded}
	ext := LLMExtractor{Adapter: a, Tracer: trace.Discard{}}
	got := ext.Extract(context.Background(), "a letter", "anything")
	if got != "" {
		t.Errorf("Extract = %q, want empty", got)
	}
}

func TestLLMExtractor_ReturnsEmptyOnUnparseable(t *testing.T) {
	// Adapter returns text that doesn't parse as extraction JSON.
	a := &scriptAdapter{answers: []string{
		`{"response":"I cannot determine the answer","confidence":0.5}`,
	}}
	ext := LLMExtractor{Adapter: a, Tracer: trace.Discard{}}
	got := ext.Extract(context.Background(), "a letter", "mumbling response")
	// "I cannot determine the answer" won't parse as {"extracted":...}
	// so LLMExtractor should return empty.
	if got != "" {
		t.Errorf("Extract = %q, want empty", got)
	}
}

func TestLLMExtractor_ImplementsExtractor(t *testing.T) {
	var _ Extractor = LLMExtractor{}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run TestLLMExtractor -v`
Expected: Compilation error — `LLMExtractor` not defined.

- [ ] **Step 7: Implement LLMExtractor**

Add to `pkg/benchmark/orchestrator/extract.go`:

```go
const extractionPrompt = `You are an ANSWER EXTRACTOR. You do NOT solve tasks or reason about correctness.

You are given:
1. A GOAL describing what format the answer should be in.
2. A RESPONSE from another system that attempted the task.

Extract the answer from the RESPONSE that satisfies the GOAL.
If the response contains reasoning, ignore the reasoning — extract only the final answer.
If the response states multiple candidates, extract the one the response commits to.
If no answer can be extracted, return an empty string for "extracted".

Reply with a single JSON object: {"extracted": "<the answer>", "confidence": <0.0 to 1.0>}.`

// LLMExtractor uses a one-shot adapter call to extract the canonical
// answer from a model response. Generic across all benchmark formats.
type LLMExtractor struct {
	Adapter  adapter.Adapter
	Tracer   trace.Tracer
	Governor *vram.Governor
}

// Extract implements Extractor.
func (e LLMExtractor) Extract(ctx context.Context, goal, raw string) string {
	if e.Adapter == nil {
		return ""
	}
	tracer := e.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}

	start := time.Now()

	lease, err := e.Governor.Acquire(ctx)
	if err != nil {
		trace.Error(tracer, ctx, "extractor.llm_extract_error",
			slog.String("err", err.Error()),
		)
		return ""
	}
	defer lease.Release()

	userMsg := "[GOAL]\n" + goal + "\n\n[RESPONSE]\n" + raw
	env := session.NewRoot(
		session.ID("extract"),
		extractionPrompt+"\n\n---\n\n"+userMsg,
		"",
		classification.Internal,
		session.Budget{TokensRemaining: 2_000, DepthRemaining: 1},
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

	content := out.Execute.ReturnResult.Result.Content
	extracted, confidence := parseExtractionResponse(content)

	truncated := raw
	if len(truncated) > 200 {
		truncated = truncated[:200] + "..."
	}

	if extracted == "" {
		trace.Info(tracer, ctx, "extractor.extract_empty",
			slog.String("goal", goal),
			slog.String("raw_truncated", truncated),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	} else {
		trace.Info(tracer, ctx, "extractor.llm_extract",
			slog.String("goal", goal),
			slog.String("raw_truncated", truncated),
			slog.String("extracted", extracted),
			slog.Float64("confidence", confidence),
			slog.Int("tokens_used", out.Execute.TokensUsed),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	}

	return extracted
}

func parseExtractionResponse(content string) (string, float64) {
	// The adapter returns {"response": "<inner json>", "confidence": ...}
	// where the inner JSON is {"extracted": "...", "confidence": ...}.
	// Try nested first, then flat.
	var outer struct {
		Response   string  `json:"response"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(content), &outer); err == nil && outer.Response != "" {
		content = outer.Response
	}

	var result struct {
		Extracted  string  `json:"extracted"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(content), &result); err == nil {
		return result.Extracted, result.Confidence
	}
	return "", 0
}

// Compile-time check.
var _ Extractor = LLMExtractor{}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run "TestLLMExtractor|TestDeriveGoal" -v`
Expected: All PASS

- [ ] **Step 9: Run full test suite**

Run: `cd oscillitron && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 10: Commit**

```bash
cd oscillitron && git add pkg/benchmark/orchestrator/extract.go pkg/benchmark/orchestrator/extract_test.go
git commit -m "$(cat <<'EOF'
feat(extractor): add DeriveGoal and LLMExtractor

DeriveGoal makes a one-shot LLM call to extract a format description
from the case prompt. LLMExtractor uses the goal + response to extract
the canonical answer via LLM, replacing per-benchmark regex extractors.

Both emit trace events: extractor.goal_derived, extractor.llm_extract,
extractor.extract_empty for full observability.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Remove goal extraction from Tree, wire Case.Goal

**Files:**
- Modify: `pkg/benchmark/orchestrator/tree.go`
- Modify: `pkg/benchmark/orchestrator/tree_test.go`

- [ ] **Step 1: Update Tree to use Case.Goal instead of deriving its own**

In `pkg/benchmark/orchestrator/tree.go`:

Remove the `deriveGoal` method (lines 163-186) and the `goalExtractionPrompt` constant (lines 188-202) — these now live in `extract.go`.

Replace lines 101-109 in `Tree.Answer`:

```go
	goal := c.Goal
	outputSchema := goal
	if outputSchema == "" {
		outputSchema = "{answer}"
	}
```

Remove the `if err != nil` block for goal derivation (lines 102-104) since there's no error to handle — the goal is pre-derived.

- [ ] **Step 2: Update Tree tests to set Case.Goal**

In `pkg/benchmark/orchestrator/tree_test.go`, for each test that constructs a `benchmark.Case`, add `Goal: "a single letter A-D"`:

Find all `benchmark.Case{ID:` and add the Goal field. For example:
```go
	c := benchmark.Case{
		ID:       "q1",
		Prompt:   "What is 2+2? A) 3 B) 4 C) 5 D) 6\nAnswer with a single letter.",
		Expected: "B",
		Goal:     "a single letter A-D",
	}
```

- [ ] **Step 3: Run tests**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -v`
Expected: All PASS

- [ ] **Step 4: Run full suite**

Run: `cd oscillitron && go build ./... && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
cd oscillitron && git add pkg/benchmark/orchestrator/tree.go pkg/benchmark/orchestrator/tree_test.go
git commit -m "$(cat <<'EOF'
refactor(tree): use Case.Goal instead of internal goal derivation

Tree no longer derives its own goal — it reads Case.Goal set by the
benchmark runner. Goal extraction prompt and deriveGoal method moved
to extract.go in the previous commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Wire DeriveGoal into the benchmark runner

**Files:**
- Modify: `pkg/benchmark/benchmark.go`
- Modify: `cmd/bench/main.go`

- [ ] **Step 1: Add GoalDeriver function field to RunnerConfig**

In `pkg/benchmark/benchmark.go`, add to `RunnerConfig` after the `Tracer` field:

```go
	// GoalDeriver, when set, is called once per case before
	// orchestrators run. It derives a format-description goal from
	// the case prompt and returns it. The runner stores the result
	// in Case.Goal. When nil, Case.Goal stays empty.
	// Signature avoids importing pkg/benchmark/orchestrator (which
	// imports this package) — the concrete DeriveGoal is wired in
	// cmd/bench via a closure.
	GoalDeriver func(ctx context.Context, c Case) string
```

No new imports needed — `context` and `Case` are already in scope.

- [ ] **Step 2: Call GoalDeriver in the Run loop**

In `pkg/benchmark/benchmark.go`, in the `Run` function, insert after line 306 (`cr := CaseResult{...}`) and before the orchestrator loop (line 310):

```go
		if cfg.GoalDeriver != nil {
			c.Goal = cfg.GoalDeriver(caseCtx, c)
		}
		cr.Case = c
```

- [ ] **Step 3: Wire LLMExtractor and GoalDeriver in cmd/bench**

In `cmd/bench/main.go`, after building the orchestrator adapter (~line 400):

```go
	llmExtractor := orchestrator.LLMExtractor{
		Adapter:  orchAdapter,
		Tracer:   tracer,
		Governor: governor,
	}
```

Pass `llmExtractor` (not `benchCfg.Extractor`) to all orchestrators.

Wire the `GoalDeriver` closure on `RunnerConfig`:

```go
	GoalDeriver: func(ctx context.Context, c benchmark.Case) string {
		return orchestrator.DeriveGoal(ctx, orchAdapter, tracer, c)
	},
```

Remove the `Extractor` field from `benchmarkConfig` struct (line 772).

Remove the `extractor := benchCfg.Extractor` line (line 403) and update all orchestrator constructors to use `llmExtractor` instead.

- [ ] **Step 4: Build and test**

Run: `cd oscillitron && go build ./... && go test ./... -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
cd oscillitron && git add pkg/benchmark/benchmark.go cmd/bench/main.go
git commit -m "$(cat <<'EOF'
feat(bench): wire LLM goal derivation and extraction into runner

RunnerConfig gains GoalAdapter — when set, the runner derives a goal
once per case via DeriveGoal before orchestrators run. cmd/bench
constructs an LLMExtractor from the orchestrator adapter and passes
it to all orchestrators. Per-benchmark regex extractor wiring removed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Full integration test and cleanup

**Files:**
- Modify: `pkg/benchmark/orchestrator/extract_test.go` (add integration test)

- [ ] **Step 1: Write an integration test that exercises the full pipeline**

Add to `pkg/benchmark/orchestrator/extract_test.go`:

```go
func TestLLMExtractor_IntegrationWithGoal(t *testing.T) {
	// Simulate the full pipeline: DeriveGoal → Tree/Single → LLMExtractor.
	goalAdapter := &scriptAdapter{answers: []string{
		`{"response":"The answer must be exactly one letter: A, B, C, or D.","confidence":1.0}`,
	}}
	c := benchmark.Case{
		ID:       "integ-001",
		Prompt:   "Question: X?\nA) a\nB) b\nC) c\nD) d\nAnswer with a letter.",
		Expected: "C",
	}
	// Step 1: derive goal
	goal := DeriveGoal(context.Background(), goalAdapter, trace.Discard{}, c)
	if goal == "" {
		t.Fatal("DeriveGoal returned empty")
	}
	c.Goal = goal

	// Step 2: simulate orchestrator producing a response
	response := "After careful analysis, the answer is C because it best fits."

	// Step 3: extract via LLM
	extractAdapter := &scriptAdapter{answers: []string{
		`{"response":"{\"extracted\":\"C\",\"confidence\":0.99}","confidence":1.0}`,
	}}
	ext := LLMExtractor{Adapter: extractAdapter, Tracer: trace.Discard{}}
	extracted := ext.Extract(context.Background(), c.Goal, response)
	if extracted != "C" {
		t.Errorf("extracted = %q, want C", extracted)
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd oscillitron && go test ./pkg/benchmark/orchestrator/ -run TestLLMExtractor_Integration -v`
Expected: PASS

- [ ] **Step 3: Run the full test suite with race detector**

Run: `cd oscillitron && go test -race ./... -count=1`
Expected: All PASS, no races

- [ ] **Step 4: Verify build**

Run: `cd oscillitron && go build ./...`
Expected: Clean build

- [ ] **Step 5: Commit**

```bash
cd oscillitron && git add pkg/benchmark/orchestrator/extract_test.go
git commit -m "$(cat <<'EOF'
test(extractor): add integration test for goal + LLM extraction pipeline

Exercises DeriveGoal → Case.Goal → LLMExtractor.Extract end-to-end
with scripted adapters. Verifies the full extraction pipeline works
as designed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```
