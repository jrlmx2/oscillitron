<!-- CLAUDE GENERATED -->

# Session handoff — 2026-05-22

Picks up from `session-2026-05-19-handoff.md`. Read that first if you need pre-2026-05-21 context; the rest of this assumes it.

## You are here

Phase 1 kill-or-proceed gate **has been run and PROCEED'd on quality**. The orchestration architecture is empirically validated on email drafting (Haiku-orchestrated 0.96 of Sonnet quality, beating Haiku-solo 0.92 for the first time). The wedge measurement was done against hosted Haiku — a deliberate v0 shortcut — and the user has redirected toward (a) testing against established LLM benchmarks (GPQA Diamond, MATH-500, AIME, HLE, MMLU-Pro, SWE-bench), and (b) running the orchestrator on local Hermes substrate so the cost-wedge claim is testable against the actual project thesis (local cheap model vs. hosted frontier). Substrate-pluggability + local-grader (`AdapterGrader`) just landed; the immediate next step is benchmark scaffolding starting with GPQA Diamond.

## What's open

**Active branch:** `claude/phase1-email-drafts` (stacked on the merged concurrency/VRAM chain).

**Open PR:** [#24](https://github.com/jrlmx2/oscillitron/pull/24) "Phase 1 kill-or-proceed gate" — extended via subsequent commits on the same branch since the original PR description was written. The commit log on the branch is the source of truth for what's actually in.

**Commits on the active branch beyond what PR #24 originally described:**

| Commit | What it added |
|---|---|
| `58d5a66` | Original Phase 1 driver + 8-case email corpus + AnthropicAdapter + AnthropicGrader |
| `13040f9` | Verdict ordering fix (quality primacy) |
| `24721d0` | Three orchestration fixes: tightened synthesizer prompt, plan-driven N, critique→revise loop |
| `6d714f7` | Critique calibration (default-pass) |
| `02b6a42` | Substrate-pluggable phase1: per-role config; default orchestrator=local Hermes, frontier+grader=Anthropic API |
| `270eec6` | `AdapterGrader` (any substrate); wires `--grader-substrate=hermes` for pure-local Phase 1 |

The PR description is stale. When the new context returns, either update the PR body or land the branch and start fresh.

## Measurement state (last live run, before rate limits)

Email-drafting corpus, 8 cases:

| Configuration | Quality vs Sonnet | Cost ratio | Verdict |
|---|---|---|---|
| Haiku solo (no orchestration) | 0.92 | 0.24 | PROCEED |
| Haiku orch v1 (fixed-3 ensemble) | 0.90 | 1.36 | net-negative orchestration |
| Haiku orch v2 (plan + tightened synth + revise) | **0.96** | 1.68 | PROCEED ON QUALITY |
| Haiku orch v3 (v2 + calibrated critique) | 0.95 | 1.83 | PROCEED ON QUALITY |
| Sonnet 1-shot | 1.00 | baseline | — |

Three observations the next context should keep in mind:

1. **Solo Haiku already passes Phase 1 thresholds**. Substrate substitution alone delivers the wedge; the architectural orchestration is *a different thesis* (quality lift at substrate parity, or wedge at much-cheaper substrate).
2. **Orchestration adds ~4 quality points (0.92 → 0.96) at ~7× the call count.** The cost-wedge claim against hosted Haiku is structurally not testable — per-token gap to Sonnet is only ~4×, less than the orchestration call multiplier.
3. **The case-006 v1 catastrophe** (orchestration synthesizer concatenated three different time slots into a contradictory answer) was a real failure mode caught by single-run measurement. v2's plan-driven N + tighter synth fixed it.

Per-case detail from the v2/v3 runs is in `/tmp/phase1-full.json`, `/tmp/phase1-v2.json`, `/tmp/phase1-v3.json`, `/tmp/phase1-haiku-solo.json` (ephemeral — likely gone by next session; the JSON-shaped per-case data isn't committed anywhere durable yet).

## Architectural locks added this session

In parent `CLAUDE.md` Architecture section:

1. **Phase 1 workload (locked 2026-05-21)**: mundane office tasks with a nuance twist; email drafting as v0; tools/connectors execute *after* the orchestrator has produced a verified output, never inside the call tree. Action-vs-execution separation is architectural.
2. **Library auto-manages concurrency by default (locked 2026-05-21)**: `runner.Config.MaxConcurrency = 0` (zero value) means library-managed via VRAM probe + sliding-window estimator. `MaxConcurrency = 1` is strict serial; `N > 1` is static cap. Probe failure → serial fallback. Hard ceiling `MaxConcurrencyCeiling` default 8.

In subproject `oscillitron/CLAUDE.md`:

- `pkg/adapter/anthropic` — adapter.Adapter backed by `pkg/anthropic`, one model per instance
- `pkg/grader` — LLM-as-judge with rubric; both `AnthropicGrader` and `AdapterGrader` (substrate-agnostic)
- `pkg/recomposer.AdapterSynth` — generic Synthesizer wrapping any adapter.Adapter
- `cmd/phase1` — Phase 1 measurement driver, substrate-pluggable per role

## Things that are settled — DO NOT re-litigate

These come up repeatedly. They're decided:

| Decision | Why settled |
|---|---|
| Hermes is wrapped, not modified | Parent CLAUDE.md lock 2026-05-18 |
| Specialization lives in playbooks/retrieval/topology, never in weights | Parent CLAUDE.md lock |
| Per-AP per-phase session_id (`<envID>:<phase>`) — NOT tree-scoped | We reverted tree-scoping in PR #22; it violated the locked invocation-isolation rule. KV-cache hits come from engine-level prefix caching, not session sharing. See `cmd/probe-prefix-cache` if you need to re-verify on a different substrate. |
| Tools/connectors execute *after* orchestrator output, never inside | Lock 2026-05-21 |
| Grading is a hook, not a workflow step | User stated explicitly this session; the architecture already matches (pkg/grader is separate from pkg/runner) |
| Library auto-manages concurrency + VRAM (zero-config-is-safe) | Lock 2026-05-21 |
| Phase 1 workload is email drafting (for the current corpus; expanding to LLM benchmarks is *next*) | Lock 2026-05-21 |

## Where the conversation left off

The user just confirmed they want:

1. **Benchmark against multiple established LLM benchmarks**: GPQA Diamond, MATH-500, AIME, HLE, MMLU-Pro, SWE-bench Verified.
2. **Dual-grading in the short term**: run both Haiku-grader and local-Hermes-grader on each candidate as a sanity check on the local judge. Eventually local-only once trusted. Treated as a "benchmark hack," not architectural.
3. **Judging via hook, not workflow step**: confirmed the existing `pkg/grader` shape already matches this. Critique (inside the workflow) stays in-workflow because the runner acts on its verdict; grading (outside the workflow) is for measurement.

The user just asked an unanswered design question:

> "But how does that grader manage limited vram?"

My response (in the chat, not yet acted on): the grader currently bypasses `pkg/runner`'s VRAM management. For sequential benchmark execution, this works out fine (engine-level serialization, orchestrator finishes before grader fires within a case). The proper fix when we need parallel case execution or want explicit cross-component coordination is a process-wide `vram.Governor` that both the runner and the grader can opt into. I recommended **not** building the governor up front — sequential execution is fine for the immediate benchmark work — but having `cmd/bench` share a single VRAM probe/estimator across components so the governor is a small future-proof drop-in.

**Awaiting answer**: should we build the governor up front or sequential-first?

## The concrete next piece of work

Once VRAM-governor question is answered:

### Phase 1: `pkg/benchmark` scaffolding

```
pkg/benchmark/
  benchmark.go        # Case, Loader, Grader interfaces + Runner orchestration
  exactmatch.go       # Grader for MCQ (A/B/C/D) — no LLM judge needed
  numericmatch.go     # Grader for math problems — exact numeric match
  dualgrader.go       # Wraps N graders, runs all, reports each + agreement
```

### Phase 2: GPQA Diamond loader + first benchmark run

```
pkg/benchmark/loader/gpqa/   # GPQA Diamond loader
cmd/bench/
  main.go                    # --benchmark gpqa|math500|aime|mmlupro|hle|swebench
  cases/                     # downloaded benchmark JSON snapshots
```

GPQA Diamond was chosen as the first because: graduate-level reasoning, MCQ format (zero LLM-judge needed if grading exact-match), frontier scores in 60-75% range (real room for orchestration to claim), publicly available (HuggingFace gated but free auth), 198 cases (manageable).

### Phase 3+: MATH-500, AIME, MMLU-Pro, HLE, SWE-bench

Each ~one focused PR following the GPQA pattern. SWE-bench needs Docker + repo execution environment — separate, later.

## Dataset access

HuggingFace gates the datasets. Pragmatic path: `huggingface-cli login` once, `huggingface-cli download <dataset>` to fetch, commit the JSON snapshot under `cmd/bench/cases/`. Makes runs reproducible without runtime network dependency. **Do not** make the bench driver pull from HuggingFace at runtime — keeps test infra simple and offline-capable.

HLE specifically requires gated access (Idavidrein org). If access fails, skip HLE for now and proceed with GPQA + MATH-500 + AIME (all freely accessible).

## API rate limits

The user is currently rate-limited on Anthropic API. Phase 1 ran 4 full corpus passes today (solo, v1, v2, v3 + the smoke runs) which burned through whatever quota was available. **Do not run live phase1 against the Anthropic API in the next session without confirming rate limits have reset.** Pure-local mode (everything through Hermes) is now possible — use that for any immediate measurement work.

## Operator's local setup

Per `references/performance-operator-guide.md` and the 2026-05-20 perf doc:

- Apple Silicon laptop
- gemma-4-e4b downloaded, was running via LM Studio
- `hermes gateway` was running on `127.0.0.1:8642`
- Latest measurement: 14k input tokens, ~7s warm-call, ~206s cold-call

If the new session needs to verify the local setup is alive, recipe in `references/performance-operator-guide.md` under "Make the smoke test fast for the next session." Hermes persona slim-down recipe in `references/hermes-persona-slim-down.md` (critical — default Hermes injects 14k tokens of toolset boilerplate that should be cut).

## What NOT to do next session

- Don't re-run the email-drafting phase1 against Anthropic API (rate limits + the measurement is done)
- Don't keep iterating on email-drafting orchestration tuning — the user has moved on to "do real benchmarks"
- Don't propose tree-scoped sessions or any cross-AP state sharing (lock violation, reverted)
- Don't build the VRAM governor up front unless the user explicitly asks for it
- Don't make grading a workflow step — it stays a hook

## Reference docs (full project context)

In order of "load when":

- `oscillitron/CLAUDE.md` — code-mode conventions, package inventory
- `CLAUDE.md` (parent) — architecture, locks, project conventions
- `INDEX.md` (parent) — catalog of loadable resources
- `references/phase1-measurement-guide.md` — how to run phase1, verdict interpretation, substrate examples, Haiku-as-hosted-cheap-proxy caveat
- `references/cost-dynamics-narrative.md` — the cost wedge thesis (evaluator-facing)
- `references/performance-operator-guide.md` — hardware sizing, local Hermes setup
- `references/hermes-persona-slim-down.md` — cutting the 14k Hermes default overhead
- `references/vram-platform-coverage.md` — pkg/vram coverage matrix
- `scratch/library-plan.md` §2 — original Phase 1 plan and §2.5 proceed/kill criteria
- `scratch/design-notes.md` — design questions still in flight (none currently blocking)
- `artifacts/monetization-analysis.md` — business model context

## Last thing

The user is engaged, sharp, and catches architectural ambiguities (the tree-scoped sessions revert, the "what API actually" check, the VRAM-governor gap question). When in doubt, surface the gap honestly rather than glossing — they spot the glossing every time.
