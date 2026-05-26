# Session handoff — 2026-05-25/26

A new session can pick up from this file alone. This session's arc: fix the Tree orchestrator from 0% (broken) to functional, then iterate on quality through 7 design iterations with empirical testing on 3 benchmarks.

## TL;DR

**Tree orchestrator went from 198/198 errors to 85% on small-business MCQ** (vs 75% baseline). The structural work is done; the quality gains are real but small and noisy on 20 cases. Larger-scale validation needed on ARC-Challenge (70% baseline — the right difficulty band for measuring tree lift).

**PR #70** (merged): per-playbook response_format schemas. Fixed the 100% error rate.
**PR #72** (merged): goal-driven decomposition, confidence selection, tree trace format.
**PR #73** (merged): lineage threading, structural fixes, free evaluate.
**Branch `claude/tree-goal-recompose`**: 8 commits ahead of main with latest fixes + benchmark datasets. Needs a PR.

## What's on the branch (not yet merged)

8 commits on `claude/tree-goal-recompose` after PR #73 merge:

1. `5292ce9` — Thread original prompt + lineage to children and recomposer
2. `e641955` — Override recompose=none → sequential when subtree has children
3. `c03f14f` — Force children to process playbook (no re-planning)
4. `368db27` — Skip inner Evaluate for children (reverted in 36c89db)
5. `36c89db` — Revert skip-evaluate (children call inner Evaluate again)
6. `8905b8a` — Constrain root Evaluate to plan or process, error fallback
7. `d14390a` — Let root Evaluate freely (model decides plan vs process)
8. `b243738` — Add benchmark datasets + variable-option loader + LLM-only goal

**Push to PR and merge before next session.**

## Empirical results across all runs

### Small Business (20 custom MCQ cases, llama3.1:8b-q6)

| Run | Pass | Rate | Key change |
|---|---|---|---|
| Baseline (1 call) | 15/20 | **75%** | — |
| Forced plan (tree v1) | 14/20 | 70% | 3 flips, 3 regressions, 2 errors |
| Free eval v6 (no constraint) | 10/20 | 50% | Model picks critique/verify on root → empty output |
| **Free eval v7 (w/ constraint)** | **17/20** | **85%** | 4 flips, 1 regression, 0 errors |

v7 is the best configuration: root Evaluate freely picks plan or process (constrained to those two), children forced to process, recompose=none overridden to sequential.

### ARC-Challenge baseline (20 cases, llama3.1:8b-q6)

| Arm | Pass | Rate |
|---|---|---|
| Frontier (1 call) | 14/20 | 70% |
| Vote-1 | 16/20 | 80% |
| Tree | **not yet run** | — |

**ARC is the next benchmark to test the tree on.** 70% baseline with 30pp headroom is the ideal difficulty band.

### GPQA Diamond (198 cases pre-fix, various smaller runs post-fix)

All arms at floor level (~20%). Model can't do graduate physics/chemistry. Not useful for measuring tree lift.

### MMLU-Pro (23 cases before killed)

Floor level (~15% across all arms). 10-option format too hard for 8b.

## Key architectural findings

### What works
1. **Goal extraction** — LLM reads the prompt, describes the expected output format. Few-shot prompt with good/bad examples prevents the model from solving instead of describing.
2. **Lineage threading** — children see `[ORIGINAL QUESTION]` + `[YOUR SUB-TASK]` + instructions to reason and explain. Produces meaningful reasoning instead of letter-guessing.
3. **Root constrained to plan/process** — prevents critique/verify_grounded on root (which produce empty verifier_signal output). Error fallback catches Evaluate JSON parse failures.
4. **Children forced to process** — prevents re-planning cascades (depth 4+ trees), wasted verify/critique routing. Tree is always: goal → evaluate → plan-or-process → (if plan) children(process) → recompose.
5. **recompose=none override** — prevents the plan from silently discarding children's work.
6. **Confidence-weighted selection** — when all children produce short answers (≤50 chars), picks highest-confidence child instead of LLM synthesis fold. Avoids garbled merges of competing single-letter answers.
7. **Tree trace format** — `BuildTreeTrace` + `RenderText` + `--tree-trace-dir` for per-case debugging.

### What doesn't work (yet)
1. **Confidence calibration** — model reports 0.95-1.0 on everything regardless of correctness. Confidence-weighted selection degenerates to random pick when all confidences are equal.
2. **Decomposition judgment** — the model rarely plans voluntarily (18/20 cases picked process in free-eval). When it does plan, results are mixed. The model can't reliably judge when decomposition helps.
3. **Recomposer vote counting** — sb-007 had 2/3 children saying "D" (correct) but the sequential fold picked the minority "A". The recomposer doesn't count votes — it folds pairs, and the fold order determines the winner.

### Open design questions
1. **Vote-before-fold in recomposer** — when short answers disagree, count majority before falling back to confidence selection.
2. **Complexity heuristic for decomposition** — instead of letting the model choose, use a signal (prompt length? multi-step indicators? model uncertainty on first pass?) to force plan on complex tasks.
3. **Token accounting in tree** — `Answer.TokensUsed` is always 0 for tree cases (known gap at tree.go:126).

## Benchmark datasets available

All at `cmd/bench/cases/`, 200 cases each:

| File | Format | Loader | Published 8b rate | Best for |
|---|---|---|---|---|
| `arc_challenge_200.json` | GPQA raw | `gpqa` | ~55% | **Next tree test** |
| `hellaswag_200.json` | GPQA raw | `gpqa` | ~78% | Pattern matching (unlikely to benefit from tree) |
| `winogrande_200.json` | GPQA raw (2 options) | `gpqa` | ~72% | Coreference (unlikely) |
| `gsm8k_200_math.json` | MATH-500 raw | `math-500` | ~50% | Multi-step arithmetic (**strong candidate**) |
| `small_business_20.json` | GPQA raw | `gpqa` | 75% | Business reasoning (tested, tree shows lift) |

GPQA loader now handles variable option counts (2/4/10). GSM8K converted to MATH-500 format for the boxed-answer extraction path.

## What NOT to do at session start

- **Don't run GPQA or MMLU-Pro** — floor level on this model, no signal.
- **Don't hardcode goal patterns** — LLM-only goal extraction is the right approach.
- **Don't force plan on root** — model should decide; constrained to plan/process.
- **Don't skip the Evaluate call for children** — reverted; the model's routing reasoning is valuable even though we override to process.

## Resume sequence

```bash
# 1. Merge remaining commits
git checkout claude/tree-goal-recompose
git push origin claude/tree-goal-recompose
# Open PR, merge to main

# 2. Run ARC-Challenge tree test (best next benchmark)
go run ./cmd/bench \
  --benchmark gpqa \
  --cases cmd/bench/cases/arc_challenge_200.json \
  --orchestrator-substrate ollama \
  --orchestrator-url http://localhost:11434 \
  --orchestrator-model llama3.1:8b-instruct-q6_K \
  --frontier-substrate ollama \
  --frontier-url http://localhost:11434 \
  --frontier-model llama3.1:8b-instruct-q6_K \
  --tree --tree-max-depth 5 \
  --tree-trace-dir /tmp/tree-traces-arc \
  --vote-n 1 --stakes medium --notice \
  --limit 20 \
  --stream-out /tmp/arc-tree.jsonl \
  -v 2>&1 | tee /tmp/arc-tree.log

# 3. Compare to baseline
# Baseline already at /tmp/arc-baseline.jsonl (70% frontier, 80% vote-1)
```

## Suggested next-session priorities

1. **Run ARC tree test** — 20 cases, compare to 70% baseline. This is the validation that determines if tree lift is real or noise.
2. **Run GSM8K** — multi-step math is where chain-of-thought decomposition has the strongest literature support. Needs `--benchmark math-500 --cases cmd/bench/cases/gsm8k_200_math.json`.
3. **Vote-in-recompose** — if sb-007 pattern repeats (majority overridden by fold order), add majority counting to the short-answer selection path.
4. **Confidence calibration** — the model's self-reported confidence is useless. Consider using agreement-across-children as a proxy for confidence instead.
5. **v4 calibration design review** — `scratch/v4-design.md` is still pinned for fresh review.

## Critical files

- `pkg/benchmark/orchestrator/tree.go` — the tree orchestrator with all fixes
- `pkg/benchmark/orchestrator/treetrace.go` — tree trace format
- `pkg/recomposer/synth.go` — confidence selection + goal threading
- `pkg/recomposer/adapter_synth.go` — synthesis prompt with goal + original task
- `pkg/adapter/minimal/minimal.go` — per-playbook JSON schemas
- `scratch/2026-05-25-per-playbook-response-format-design.md` — original design spec
- `docs/superpowers/plans/2026-05-25-per-playbook-response-format.md` — implementation plan

## Bench result files on disk

| File | What |
|---|---|
| `/tmp/sb-baseline.jsonl` | SB 20-case frontier baseline (75%) |
| `/tmp/sb-tree.jsonl` | SB forced-plan tree (70%) |
| `/tmp/sb-v6.jsonl` | SB free-eval v6 no constraint (50%) |
| `/tmp/sb-v7.jsonl` | SB free-eval v7 with constraint (85%) |
| `/tmp/arc-baseline.jsonl` | ARC 20-case frontier baseline (70%) |
| `/tmp/tree-traces-sb-v7/` | SB v7 per-case tree traces |
| `/tmp/full-tree-198.log` | GPQA 198-case pre-fix run (all tree errors) |
| `/tmp/smoke-tree-10case.jsonl` | First post-response-format-fix run |
