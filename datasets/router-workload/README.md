## Status: VERIFIED-COMPLETE ✅ (500 tasks · 100/playbook · 70/70 grid · all ≥3 verification passes)

Growth + verification phases complete (iteration 34). Every task has ≥3 independent clean verification passes (coherence + label-correctness + answer-correctness); no duplicate IDs or prompts. The corpus is ready to feed the §2.12.9.3 router experiment. The loop now continues light periodic re-verification only.

# Router-workload corpus — playbook-heterogeneous

A curated test corpus for Oscillitron's **Thread A router** (kNN playbook-hint
router over `pkg/exemplar`). See `scratch/dense-router-design.md` §2.10, §2.12.9,
§4d.

## Why this exists

The router routes on **operation (playbook)**, not subject. For it to be
testable, the *correct playbook must vary across the corpus*. Standard MCQ
benchmarks (GPQA, MMLU-Pro) are **subject-heterogeneous but playbook-homogeneous**
— every case is the same `process` operation — so the router's disagreement rate
is ~0 by construction and proves nothing (§2.12.9).

This corpus is the fair test: the **operation varies per task**, and **domain is
crossed orthogonally** so a `plan` task in law and a `plan` task in physics share
a playbook, while a `plan` and a `critique` task in physics do not. The router
must route on the operation axis, not the domain axis.

## The operation axis (Oscillitron's v0 playbooks)

| `expected_playbook` | The task genuinely needs… |
|---|---|
| `plan` | decomposition into subtasks + a recompose spec (multi-step) |
| `process` | a single-step transform / direct answer |
| `critique` | evaluation of a *given* prior result for issues |
| `verify_grounded` | a pass/fail check of a result against a stated spec |
| `compose` | merging multiple given parts into one coherent whole |

## The domain axis (orthogonal, surface diversity)

math, law, physics, chemistry, engineering, health, history, biology, economics,
cs, psychology, business, philosophy, geography.

## Layout

```
datasets/router-workload/
  README.md            ← this file
  manifest.json        ← per-playbook + per-domain counts, verification state
  tasks/<playbook>.jsonl   ← one task per line (5 files, one per playbook)
  exemplars/<action>.json  ← seed exemplar store (pkg/exemplar.FileStore shape)
```

> **ID convention:** `<playbook>-<domain>-<NNN>`, NNN sequential per (playbook,domain) cell in file order (renumber-on-write keeps it collision-free).

### Task schema (one JSON object per line in `tasks/<playbook>.jsonl`)

```json
{
  "id": "plan-law-001",
  "domain": "law",
  "expected_playbook": "plan",
  "prompt": "…the task as the user would state it…",
  "rationale": "why this operation is the single most-correct playbook",
  "answer_or_check": "expected answer (process/verify_grounded) | expected subtasks+recompose (plan) | expected merged output (compose) | pass/issues (critique)",
  "coherence_checked": true,
  "verification_passes": 1
}
```

### Exemplar schema (`exemplars/<action>.json`, array)

Matches `pkg/exemplar.FileStore` (one file per action):
`{Action, Prompt, Output, Score, SourceCase, Tokens, AddedAt, LastRetrievedAt}`.
These are the labeled neighbors the router's cross-action kNN votes over.

## How it feeds the experiment (§2.12.9.3)

1. Load `exemplars/` into an `exemplar.FileStore` → the router's `RetrieveAcross`
   reads it.
2. Walk the `tasks/` corpus through an **unconstrained Evaluate-per-AP** path
   (`cmd/oscillitron --router`, not the `Tree` arm which hard-pins `process`).
3. Metric: `router.evaluate_overrode_hint` disagreement rate, and whether the
   hint's playbook matches `expected_playbook` more often than Evaluate alone.

## Verification discipline

A mislabeled task silently corrupts the experiment. Every task is checked for
(a) prompt coherence, (b) that `expected_playbook` is genuinely the single
most-correct operation, (c) answer correctness for `process`/`verify_grounded`.
`verification_passes` tracks independent clean passes; target ≥3 before the
corpus is considered verified-complete.

**Target:** ~100 tasks per playbook (~500 total), every (playbook × domain) cell
represented. (Far smaller than an MCQ benchmark — the router needs operation
variety, not volume.)
