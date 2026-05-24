<!-- CLAUDE GENERATED -->

# Loader design — MATH-500

**Status:** *draft, 2026-05-23.* Implementation queued behind the MMLU-Pro loader PR. Single-PR scope per the workflow lock.

## Why MATH-500

GPQA Diamond and MMLU-Pro are both MCQ — the model picks a letter from a discrete option set. MATH-500 is **open-ended math** with stepwise reasoning and a boxed final answer. Different cognitive shape:

- No multiple choice → no "lucky guess" baseline (random chance for MCQ is 25% on 4-option, 10% on 10-option; MATH-500 has effectively zero random-chance accuracy).
- Voting semantics change — there's no majority over a discrete option set. Voting on MATH-500 means voting on final answers (which can be any number, expression, or simplified form).
- Confidence calibration may behave differently — stepwise reasoning produces a final answer where the model often "knows it's not sure" mid-derivation but commits anyway at the boxed step.

This is a genuinely different test of the v3 chain. The v4 calibration story may or may not transfer; that's the point of running it.

## Dataset shape

Per `HuggingFaceH4/MATH-500`:

```json
{
  "problem": "Let $f(x) = 3x + 2$. Find $f(f(f(1)))$.",
  "solution": "We have $f(1) = 5$, $f(5) = 17$, and $f(17) = 53$. So $f(f(f(1))) = \\boxed{53}$.",
  "answer": "53",
  "subject": "Algebra",
  "level": 1,
  "unique_id": "test/algebra/24.json"
}
```

500 problems, 7 subject categories (Algebra, Counting & Probability, Geometry, Intermediate Algebra, Number Theory, Prealgebra, Precalculus), 5 difficulty levels (1=easiest, 5=hardest).

## Package layout

```
pkg/benchmark/loader/math500/
  math500.go      — Loader struct, Load(), Build()
  math500_test.go — Round-trip parse, boxed-answer extraction, deterministic-order
```

## Prompt format

```
Solve the following problem. Show your work step by step, then state your final answer inside \boxed{...}.

Problem: <problem text>
```

The `\boxed{}` convention is canonical in the math literature and the dataset itself uses it. Models trained on math benchmarks expect this format.

## Grader: BoxedAnswer

GPQA's Multichoice grader can't handle MATH-500. Need a new grader:

```go
package grader

// BoxedAnswer extracts the final \boxed{...} answer from a math
// response. Last-match-wins (the model may write multiple \boxed
// expressions in the work; the final one is the committed answer).
type BoxedAnswer struct {
    // Normalize collapses whitespace, lowercases, and strips trailing
    // punctuation before comparing. Some answer formats have
    // legitimate latex variants (e.g., "1/2" vs "\frac{1}{2}"). v0
    // does light normalization only; sophisticated answer-equivalence
    // (semantic match) is v0.x scope.
    Normalize bool
}

var boxedRE = regexp.MustCompile(`\\boxed\{([^}]*(?:\{[^}]*\}[^}]*)*)\}`)

func (b BoxedAnswer) Grade(c benchmark.Case, raw string) benchmark.Verdict { … }
```

**Critical caveat:** simple string-equality grading misses true positives because of latex equivalence (`\\frac{1}{2}` vs `0.5` vs `1/2`). Standard practice in math benchmarks is to use a *normalizer* (Hendrycks's `math_equivalence.py` is the canonical one). v0 ships with light normalization only; a `math_equivalence`-style normalizer is queued as v0.x once we see how much it actually matters on the data.

## Loader struct

```go
type Loader struct {
    Path    string // JSON file path
    NameStr string // defaults to "math-500"
    Limit   int    // 0 = all
}

func (l Loader) Name() string { return "math-500" }
func (l Loader) Load(_ context.Context) ([]benchmark.Case, error) { … }
```

Case metadata populated: `subject` and `level` from the source. The v4 calibration store's `domain` key uses `subject`; `level` could feed a future v4.x difficulty dimension.

## Voting semantics

This is the interesting question. Vote-5 on MCQ means "5 attempts, each picks A/B/C/D, majority wins." Vote-5 on MATH-500 means "5 attempts, each produces a free-form boxed answer; majority wins by exact match." Possible failure modes:

- All 5 attempts produce different numeric forms of the same answer (`1/2`, `0.5`, `\frac{1}{2}`) → no majority → vote fails to commit, even though all 5 are correct.
- 4 attempts produce a wrong answer in lockstep, 1 produces the right answer → vote ships the wrong answer.

For v0, **don't change Vote**. Run vote-5 against MATH-500 with the existing exact-match aggregation and see what breaks. The failure modes above are themselves interesting empirical signals — they tell us whether voting on free-form answers is even the right pattern.

If exact-match voting collapses on MATH-500 (the realistic outcome), the natural next step is *answer-equivalence-aware voting* — bucket attempts by canonicalized answer before tallying. That's a v0.x follow-up, not v0.

## Dataset acquisition

```
huggingface-cli download HuggingFaceH4/MATH-500 --repo-type dataset
# Convert the test split to JSON via small operator script.
```

## Test scoping

500 problems is small enough to run in full. No subsetting needed for first measurements.

Per-run cost on Haiku: 500 × 6 calls ≈ $3 (vs $40+ for full MMLU-Pro). Affordable as a regular bench.

Note: math problems often produce longer responses than MCQ (step-by-step reasoning). Token cost per call is typically 2-3× MCQ. Budget accordingly.

## Acceptance criteria

- `go test ./pkg/benchmark/loader/math500/...` green.
- `go test ./pkg/benchmark/grader/...` includes a `TestBoxedAnswer_LastMatchWins` and `TestBoxedAnswer_BasicNormalization` test.
- `cmd/bench --benchmark math-500 --cases <path>` runs against a 10-problem smoke.
- Loader populates `benchmark.Case.Metadata["subject"]` and `["level"]`.
- Documentation note in the PR: voting may produce surprising results on free-form answers; that's an empirical signal to investigate in a follow-up, not a v0 blocker.
