# LLM-Based Generic Answer Extraction

**Date:** 2026-05-26
**Status:** Design approved, pending implementation

## Problem

The benchmark system uses per-benchmark regex extractors (`ExtractLetter`, `ExtractBoxed`) to pull canonical answers from model responses. This couples the system to specific benchmark formats and fails when models produce correct answers in unexpected formats (e.g., GSM8K: model computes `$18` but doesn't wrap it in `\boxed{18}`, so extraction returns empty and grader fails).

The tree orchestrator's goal extraction (FORMAT DETECTOR) guides the recomposer but is disconnected from the extraction pipeline — the goal describes the answer format, but the extractor ignores it.

## Solution

Replace regex-based extraction with an LLM extraction call. Given the goal (format description) and the final response, the LLM extracts the canonical answer. This is generic across all benchmark formats — no per-benchmark wiring needed.

The goal serves two purposes:
1. **Tree guidance** — threads through children and recomposer to keep deep decomposition on track (existing behavior, unchanged).
2. **Extraction contract** — tells the LLM extractor what to pull from the response (new behavior).

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Extraction substrate | Same as orchestrator | One fewer config knob; keeps cost comparison honest |
| Regex fast path | None — LLM-only | Simpler code path; no per-benchmark regex maintenance |
| Scope | All orchestrators (Single, Vote, Tree, Coping) | Uniform extraction; every arm benefits |
| Goal ownership | Benchmark runner derives once per case | One LLM call per case, shared across all arms |
| Output format | JSON structured output | Guarantees parseable extraction result |

## Architecture

### Goal derivation moves to the benchmark runner

Currently `Tree.deriveGoal()` is private to the tree orchestrator. It moves to a public function:

```go
// pkg/benchmark/orchestrator/extract.go
func DeriveGoal(ctx context.Context, a adapter.Adapter, tracer trace.Tracer, c benchmark.Case) string
```

The benchmark `Runner` calls this once per case before looping over orchestrators. The goal is stored on `Case.Goal` (new field). Tree continues using it for recomposer guidance AND extraction. Single/Vote/Coping use it only at extraction time.

Trace event: `extractor.goal_derived` — carries `case`, `goal`.

### LLM Extractor replaces regex extractors

New type implementing the `Extractor` interface:

```go
// pkg/benchmark/orchestrator/extract.go
type LLMExtractor struct {
    Adapter  adapter.Adapter
    Tracer   trace.Tracer
    Governor *vram.Governor
}

func (e LLMExtractor) Extract(ctx context.Context, goal, raw string) string
```

The extraction call:
- One-shot `adapter.Execute` with `PlaybookProcess` forced (same pattern as goal derivation).
- JSON structured output: `{"extracted": "<answer>", "confidence": <0.0-1.0>}`.
- Budget: 2,000 tokens max, depth 1.
- Error handling: returns `""` on failure (grader handles as `format_no_letter`). Non-fatal.

Trace events:
- `extractor.llm_extract` — per orchestrator per case. Carries `case`, `orchestrator`, `goal`, `raw` (truncated to 200 chars), `extracted`, `confidence`, `tokens_used`, `duration_ms`.
- `extractor.extract_empty` — when extraction returns empty. Carries `case`, `orchestrator`, `goal`.

### Extractor interface change

```go
// Before
type Extractor interface {
    Extract(raw string) string
}

// After
type Extractor interface {
    Extract(ctx context.Context, goal string, raw string) string
}
```

`ExtractorFunc` updates to match. Old regex extractors (`grader.ExtractLetter`, `grader.ExtractBoxed`) stay as library functions — graders still use them internally as fallback when `Answer.Extracted` is empty. They stop being wired as `Extractor` implementations.

### Extraction prompt

System message:
```
You are an ANSWER EXTRACTOR. You do NOT solve tasks or reason about correctness.

You are given:
1. A GOAL describing what format the answer should be in.
2. A RESPONSE from another system that attempted the task.

Extract the answer from the RESPONSE that satisfies the GOAL.
If the response contains reasoning, ignore the reasoning — extract only the final answer.
If the response states multiple candidates, extract the one the response commits to.
If no answer can be extracted, return an empty string.
```

User message:
```
[GOAL]
<goal>

[RESPONSE]
<raw>
```

JSON schema:
```json
{
  "type": "object",
  "properties": {
    "extracted": { "type": "string" },
    "confidence": { "type": "number", "minimum": 0, "maximum": 1 }
  },
  "required": ["extracted", "confidence"],
  "additionalProperties": false
}
```

Confidence is extraction confidence (not answer correctness). Logged in trace, not propagated to `Answer.Confidence`.

### Per-orchestrator wiring

**Single:** Calls `extractor.Extract(ctx, case.Goal, raw)` after the adapter call.

**Vote:** Calls `extractor.Extract(ctx, case.Goal, attempt.raw)` per attempt for vote tallying. N extraction calls per case (one per vote attempt).

**Tree:** Receives `case.Goal`, threads it into the recomposer (existing behavior). Calls `extractor.Extract(ctx, case.Goal, recomposedRaw)` on the final output.

**Coping:** No changes — delegates extraction to its inner/frontier orchestrators.

**Benchmark runner:** Calls `DeriveGoal(ctx, adapter, tracer, case)` once per case, sets `case.Goal`. The adapter for goal derivation is the same adapter wired into the `LLMExtractor` — constructed once in `cmd/bench` from the orchestrator substrate config.

### cmd/bench simplification

`buildBenchmark` no longer switches on benchmark type to pick an extractor. Constructs one `LLMExtractor{Adapter, Tracer, Governor}` and passes it to all orchestrators.

### Removed

- Per-benchmark extractor wiring in `cmd/bench/main.go`.
- `ExtractorFunc` wrappers around `grader.ExtractLetter` / `grader.ExtractBoxed` as orchestrator-level extractors.
- Regex extractors stay as grader-internal fallback (unchanged).

## Trace events summary

| Event | When | Key fields |
|---|---|---|
| `extractor.goal_derived` | Once per case, before orchestrators run | `case`, `goal` |
| `extractor.llm_extract` | Per orchestrator (or per vote attempt) | `case`, `orchestrator`, `goal`, `raw` (truncated), `extracted`, `confidence`, `tokens_used`, `duration_ms` |
| `extractor.extract_empty` | When LLM extraction returns empty | `case`, `orchestrator`, `goal` |

## Files changed

| File | Change |
|---|---|
| `pkg/benchmark/orchestrator/extract.go` | **New.** `DeriveGoal`, `LLMExtractor`, extraction prompt, JSON schema. |
| `pkg/benchmark/orchestrator/extract_test.go` | **New.** Tests for `DeriveGoal` and `LLMExtractor`. |
| `pkg/benchmark/benchmark.go` | Add `Goal string` to `Case`. Runner calls `DeriveGoal` per case. |
| `pkg/benchmark/orchestrator/single.go` | Update `Extractor` interface, pass `ctx` + `goal` to `Extract`. |
| `pkg/benchmark/orchestrator/vote.go` | Update `Extract` calls per attempt. |
| `pkg/benchmark/orchestrator/tree.go` | Remove `deriveGoal`, `goalExtractionPrompt`. Use `case.Goal`. |
| `pkg/benchmark/orchestrator/coping.go` | No changes (delegates to inner). |
| `cmd/bench/main.go` | Remove per-benchmark extractor switch. Construct `LLMExtractor`. |
