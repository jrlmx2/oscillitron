<!-- CLAUDE GENERATED -->

# Phase 1 measurement guide

The Phase 1 **kill-or-proceed gate** answers one question: *does cheap-model + orchestration actually deliver comparable quality to a frontier-single-call at meaningfully lower cost?*

Until this answers "yes" with real measurements, the entire orchestration project is at risk of being thrown away. Everything else in the codebase exists to make this measurement runnable.

This doc tells an operator how to run the gate, interpret the verdict, and know when the result is meaningful vs. when it just measured the wrong thing.

## The workload (locked 2026-05-21)

**Mundane office tasks with a nuance twist.** Phase 1 v0 uses email draft generation — common enough that everyone has opinions about what good looks like, nuanced enough that single-call cheap models reliably miss something (tone-mismatch, missing acknowledgment, generic close).

The corpus (`oscillitron/cmd/phase1/cases.json`) carries 8 hand-curated cases covering decline, schedule, clarify, redirect, acknowledge-and-defer, ambiguous urgency, scope-creep pushback, and graceful no.

**What we deliberately don't measure yet:**
- Tool / connector execution. Per the design lock, side effects happen *after* the orchestrator has produced a verified output — they are a separate layer, not part of the call tree. Phase 1 only measures the orchestrator's draft quality.
- Real-user judgment. The grader is an LLM-as-judge; eventual human-eval rounds will calibrate it.

## How to run

```bash
# Required: ANTHROPIC_API_KEY in your environment.
export ANTHROPIC_API_KEY=sk-ant-...

# Default run — 8 cases, Haiku orchestrator vs Sonnet frontier, Sonnet grader.
go run ./cmd/phase1

# Verbose — per-case detail to stderr.
go run ./cmd/phase1 -v

# Limit to the first 2 cases for a quick smoke.
go run ./cmd/phase1 -v --limit 2

# Save per-case results for review.
go run ./cmd/phase1 --out results.json

# Disable ensembling (1 draft per case) — measures whether even
# a single Haiku call closes any quality gap on its own.
go run ./cmd/phase1 --drafts-per-case 1

# Stronger orchestrator — use Sonnet on both sides to isolate
# the orchestration wedge from the model wedge.
go run ./cmd/phase1 --orchestrator-model claude-sonnet-4-6
```

**Approximate cost per full run:** $0.10 – $0.50 depending on how chatty the model is per draft. Phase 1 is cheap.

## What the verdict means

The driver prints two ratios:

- **Quality ratio** = (avg orchestrator score) / (avg frontier score), each on a 1.00 scale (rubric: relevance + tone + completeness + professionalism, 1–5 each, summed and normalized to 20).
- **Cost ratio** = (orchestrator USD spent) / (frontier USD spent), inclusive of all calls in the orchestration path.

Plus a one-line **VERDICT** mapping the pair onto the §2.5 proceed/kill thresholds:

| Verdict | Meaning |
|---|---|
| PROCEED | Both quality ≥ 0.80 and cost ≤ 0.25. The wedge is real. |
| KILL | Quality < 0.65 — orchestration is producing worse output. |
| DIAGNOSE (cost above kill) | Quality looks OK but cost overhead is destroying the wedge. Look at where the tokens went. |
| PROCEED ON QUALITY | Quality threshold met, cost mid-zone. *Expected outcome when the cheap proxy is hosted* (see below). |
| INCONCLUSIVE / DIAGNOSE (mid) | One signal in the middle zone. Review per-case detail before deciding. |

## The Haiku-as-cheap-proxy caveat (important)

The §2.5 cost threshold (≤ 25% of frontier) **assumes a local cheap substrate that is 10–100× cheaper than hosted frontier**. Phase 1 v0 measures with hosted Haiku as the cheap proxy. Haiku is roughly 3.75× cheaper than Sonnet — not nearly enough headroom for orchestration overhead to fit under 25%.

So **expect** the cost ratio to land between 0.5 and 1.5 in this configuration. Hitting cost ≤ 0.25 would actually be suspicious — it'd mean the orchestrator made fewer calls than expected. What we're really measuring here is:

- **Quality ratio.** Does orchestration close the gap from Haiku-alone toward Sonnet-alone? If yes (≥ 0.80), the orchestration logic is doing real work — the wedge thesis is intact, and we move to measuring it against a real local cheap substrate.
- **Cost magnitude.** Even with Haiku, the absolute cost per task should be small (cents). If it spirals, something's wrong (runaway critique loop, recomposition bloat).

The proper §2.5 cost-wedge measurement waits for the real cheap substrate (a local 4B model behind Hermes, or a much-cheaper hosted model). When that lands, re-run Phase 1 with `--orchestrator-model <that-model>` and the cost ratio becomes meaningful against the threshold.

## What a "PROCEED ON QUALITY" verdict tells you

This is the most likely Phase 1 v0 outcome if the architecture works. It means:

- The cheap model (Haiku), under orchestration, is producing output as good as the frontier model on this workload.
- The cost ratio is in the "expected for hosted cheap proxy" zone, not a kill signal.
- The Phase 2 work (real local substrate) is justified — the orchestration is *capable* of preserving quality, so deploying it on a model that's actually 10× cheaper would deliver the wedge.

**It does not mean "ship to production."** It means "the orchestration thesis is alive; the next investment (local-substrate infrastructure, or a richer workload, or human-eval calibration) is justified."

## What "KILL" means at this stage

Either:

1. The orchestration is genuinely worse than a single frontier call on this workload. Plausible if the synthesizer collapses the drafts incorrectly, or the per-draft prompt isn't producing meaningfully different attempts. **Diagnostic:** check the per-case `--out` JSON; look at whether the orchestrator drafts are nearly identical (no ensemble diversity), or whether the synthesizer produced an inferior merge.

2. The grader is poorly calibrated and is favoring single-frontier style. **Diagnostic:** look at a handful of cases by eye. Does the human reader actually prefer the frontier draft? If yes, the grader is doing its job. If no, the rubric or model needs revision.

3. The workload is too easy for orchestration to add value. **Diagnostic:** rare in this corpus — every case has a nuance the cheap model often misses. But worth a look.

## Adding cases

`cases.json` is operator-curated. Each case is `{id, email, intent}`. Keep cases:

- **Mundane** — common-enough that the desired output isn't ambiguous in the abstract.
- **With nuance** — some constraint (tone, what to omit, who to address) that a cheap model can fail.
- **Self-contained** — the email + intent together must define a good answer.

Avoid:

- Cases that require external knowledge (current events, specific company policy).
- Cases where the "right" answer is opinion-bound (style preferences without a stated intent).
- Cases longer than ~200 lines (grader cost and latency scale with this).

## Cross-references

- `scratch/library-plan.md` §2 — the original Phase 1 plan and decision criteria.
- `references/cost-dynamics-narrative.md` — the cost wedge thesis Phase 1 is testing.
- `references/performance-operator-guide.md` — hardware sizing for the eventual local-substrate measurement.
- `oscillitron/pkg/grader` — the grader implementation, including the rubric prompt.
- `oscillitron/pkg/adapter/anthropic` — the adapter that drives both paths.
