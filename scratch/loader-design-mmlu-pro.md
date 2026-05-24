<!-- CLAUDE GENERATED -->

# Loader design — MMLU-Pro

**Status:** *draft, 2026-05-23.* Implementation queued behind PR #58 + the v4 docs PR. Single-PR scope per the workflow lock.

## Why MMLU-Pro

GPQA Diamond is hard-science MCQ. To test whether the v3.5 cross-substrate findings (voting hurts on Haiku; overconfidence universal; escalate path dead) generalize across cognitive shapes, we need a broader-knowledge MCQ benchmark.

MMLU-Pro is the natural complement:

- ~12,000 questions across 14 categories (math, physics, chemistry, law, psychology, business, etc.)
- 10-option multiple choice (vs GPQA's 4-option) — more vote dilution headroom
- Has category metadata — feeds the v4 calibration store's `domain` key dimension directly
- Mature dataset, stable HuggingFace presence (`TIGER-Lab/MMLU-Pro`)

## Dataset shape

Per HuggingFace's `TIGER-Lab/MMLU-Pro` (test split):

```json
{
  "question_id": 70,
  "question": "Find the degree for the given field extension Q(sqrt(2), sqrt(3), sqrt(18)) over Q.",
  "options": ["0", "4", "2", "6", "8", "1", "5", "3", "10", "7"],
  "answer": "B",
  "answer_index": 1,
  "cot_content": "Let's think step by step…",
  "category": "math",
  "src": "ori_mmlu-abstract_algebra"
}
```

Notable differences from GPQA:

- **10 options, not 4.** The Multichoice grader currently extracts `[A-D]` regex; needs to handle `[A-J]`.
- **`answer` already a letter.** Don't need GPQA's deterministic-letter-placement trick — the source data already commits a specific letter. (We could still permute for reproducibility-with-variance, but v0 keeps the source's letter to avoid breaking parity with how MMLU-Pro is benchmarked elsewhere.)
- **`cot_content` field.** Reasoning trace for the correct answer; useful as eval-set background but not for the run prompt.

## Package layout

```
pkg/benchmark/loader/mmlu_pro/
  mmlu_pro.go         — Loader struct, Load(), Build() — same shape as gpqa.go
  mmlu_pro_test.go    — Round-trip parse test, deterministic-order test, limit test
```

## Loader struct (mirrors gpqa)

```go
type Loader struct {
    Path    string // JSON file path
    NameStr string // defaults to "mmlu-pro"
    Limit   int    // 0 = all
}

func (l Loader) Name() string { return "mmlu-pro" }
func (l Loader) Load(_ context.Context) ([]benchmark.Case, error) { … }
```

The case prompt:

```
Question: <text>

A) <option 0>
B) <option 1>
…
J) <option 9>

End your response with the single letter (A through J) as your final character.
```

(Reuses the closing-position discipline from bug #5's fix to ProcessInstructions — the loader prompts get the same imperative.)

## Grader changes

The Multichoice grader at `pkg/benchmark/grader.Multichoice` extracts `[A-D]` letters. For MMLU-Pro it needs `[A-J]`. Two options:

1. **Make the letter set configurable on the grader.** `Multichoice{Letters: "ABCD"}` → `Multichoice{Letters: "ABCDEFGHIJ"}`. Cleanest.
2. **Per-loader grader.** Each loader bundles a grader. More boilerplate.

Recommend option 1. The change is ~5 lines + adding a default value when omitted.

## Dataset acquisition

Per project convention (`cmd/bench/cases/README.md`), datasets are operator-downloaded:

```
huggingface-cli download TIGER-Lab/MMLU-Pro test --repo-type dataset
# Convert parquet → JSON via small operator script (will land alongside the loader).
```

`cmd/bench/cases/mmlu_pro.json` will be gitignored per the existing rule.

## Test scoping

For first-pass measurement, run a **400-case stratified sample** rather than the full ~12k:

- Stratify by category: ~28 questions per category × 14 = 392, round to 400.
- Deterministic sampling (hash-sort by question_id) so reruns produce identical case sets.
- Operator can override with `--limit` or pass a custom dataset file.

Rationale: 12k × 6 calls/case × Haiku rates ≈ $40+ per full run. 400 cases gives statistical power without burning budget on the first measurement; full-scale is a downstream decision when we know what we're looking for.

## Acceptance criteria

- `go test ./pkg/benchmark/loader/mmlu_pro/...` green.
- `cmd/bench --benchmark mmlu-pro --cases <path>` runs successfully against a 10-case smoke file.
- Multichoice grader's letter-set is configurable; default `ABCD` preserves all existing tests.
- Loader produces `benchmark.Case.Metadata["category"]` populated from the source `category` field (feeds v4 calibration's `domain` dimension).
