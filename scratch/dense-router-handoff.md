# Handoff — dense-router / Thread A+B work (as of 2026-06-16)

Read this first after a `/clear`. It captures the state of the "dense model
calls focused models as tools" research → build effort and what's left.

## TL;DR

A multi-iteration design loop turned the "dense orchestrator" idea into a
falsifiable build plan (`scratch/dense-router-design.md`), then we **built and
shipped/queued the three concrete outcomes**:

| Deliverable | What | Status |
|---|---|---|
| **Thread B** — semantic-entropy confidence | `pkg/semanticentropy` + `Answer.SEConfidence` + calibration ECE/Brier + `--cope-confidence-source` | ✅ **MERGED** (PR #76) to `main` |
| **Router-test corpus** | `datasets/router-workload/` — 500 verified playbook-heterogeneous tasks | 🔵 **PR #77 OPEN** (data-only) |
| **Thread A** — kNN playbook-hint router | `pkg/exemplar.RetrieveAcross` + `pkg/router` + runner hook + `Tree.Router`/`--router*` | 🔵 **PR #78 OPEN** (code) |

The big-picture verdict (settled): **"dense" is not a cost win** — it must mean
*mid-tier not frontier*, and even then loses to the locked cheap-local path on
cost; the surviving value is two small lock-compatible grafts (Thread A + B),
both built. See `scratch/dense-router-design.md` Executive Summary.

## PR status (verify with `gh pr list` / `gh pr view <n>`)

- **#76 Thread B — MERGED.** Already in `main`. Done.
- **#77 corpus — OPEN, mergeable.** Branch `feat/router-workload-corpus`. Data only
  (`datasets/router-workload/**`); touches no Go code.
- **#78 Thread A — OPEN, mergeable.** Branch `feat/thread-a-router`. Code only;
  branched from `main` *after* Thread B merged.
- **#77 and #78 are independent and non-stacked** (disjoint files). Either merges
  in any order. Not a dependent stack, so the one-PR-at-a-time lock isn't violated.

## ⚠️ Uncommitted / untracked working-tree state (do NOT lose)

`git status` shows these floating (they predate or sit outside the PR branches):

- **`scratch/dense-router-design.md` — UNTRACKED.** This is the load-bearing
  ~1600-line design doc (9 iterations + the §4b/§4c/§4d PR-ready specs). It exists
  ONLY in the working tree, on no branch. PRs #77/#78 reference it. **If you need
  it preserved in git, commit it** (it's referenced but never committed). On disk
  it persists across `/clear`.
- **`CLAUDE.md`, `oscillitron/CLAUDE.md` — modified, uncommitted.** This is a real,
  intentional convention edit from this session: *"record scores & findings to a
  durable file, never leave them only in session history"* (root Default behaviors +
  a pointer in the code CLAUDE.md). Worth committing.
- **`README.md` — modified.** Pre-existing change from session start (not from this
  work); leave unless you know what it is.
- **`graphify-out/` — untracked.** Knowledge-graph output from a `/graphify` run.
  Local artifact; likely gitignore it.

## What's left (the actual experiments — need an operator + resources)

All three build pieces are *instrumentation*; none ran the experiment. Per
`scratch/dense-router-design.md` §2.12 / §4a:

1. **H0-SE (Thread B keep/cut).** Run `cmd/bench` GPQA with both confidence
   columns; score self-reported vs semantic-entropy via `calibration.Score`
   (ECE/Brier). Build SE in if ECE-delta ≥ 0.03. Needs GPQA data + a local model.
2. **H0-cost.** Two `cmd/bench` GPQA arms differing only in
   `--orchestrator-substrate` (local Vote-5 vs hosted-Haiku Vote-5). No new code
   except the pricing fix below. §4c has the copy-paste run checklist.
3. **H0-router (Thread A keep/cut).** NOT GPQA (§2.12.9: GPQA forces ~0 disagreement
   by construction — single-action store + Tree's `process` pin). The fair test is
   the **#77 corpus** walked via an unconstrained Evaluate path (`cmd/oscillitron
   --router` or a thin driver), reading `RouterHintOverrides/RouterHintsProduced`.
   Requires warming an `exemplar.FileStore` from the corpus first.

All gated on operator-downloaded GPQA cases (gitignored), a local Ollama model,
and an Anthropic API key.

## Known bug still open (small, standalone)

`oscillitron/cmd/phase1/main.go` `defaultPricing` (≈ lines 58–62) has stale rates:
`claude-haiku-4-5-20251001` is `{0.80, 4.00}` → should be `{1.00, 5.00}`;
`claude-opus-4-7` is `{15.00, 75.00}` → should be `{5.00, 25.00}`. (Sonnet 4.6
`{3.00, 15.00}` is correct.) 2-line fix; standalone v1.0.0 patch; **does NOT block
the bench experiments** (`cmd/bench` takes blended `--price`/`--frontier-price`,
not this map).

## Loops

Both `/loop` cron jobs from this session are **RETIRED** (Thread B build loop and
the corpus curation loop). No active crons of ours. The corpus reached
verified-complete (500 tasks, all ≥3 verification passes) before retirement.

## Key locks / orientation for a fresh session

- Architecture truth: root `CLAUDE.md` (design) + `oscillitron/CLAUDE.md` (code).
- Design/verdicts/specs: `scratch/dense-router-design.md` (Exec Summary up top;
  §4b/§4c/§4d are the PR-ready specs that produced #76/#78).
- Relevant locks: uniform-node; evaluate→execute every AP; cheap-local-first with
  frontier only at `delegate`/`verify_judge`; specialists-are-substrate;
  no-weight-updates; brain-function-not-subject; PR workflow (branch from main,
  one PR, TDD); record-scores-to-a-file convention (this session).
- Build conventions: `cd oscillitron && go build ./... && go test -race ./...`;
  stdlib-first; tests-next-to-code.

## Suggested next moves

1. Merge #77 and #78 (review first). 
2. Commit or discard the floating doc changes (esp. decide whether to track
   `scratch/dense-router-design.md`).
3. Open the 2-line `cmd/phase1` pricing-fix PR.
4. When an operator has GPQA + a local model + API key: run H0-SE / H0-cost, then
   build the §2.12.9.3 heterogeneous-walk harness for H0-router and run it.
