<!-- CLAUDE GENERATED -->

# Oscillitron

An LLM orchestration runtime in Go, built to answer one question: **how much can orchestration elevate the floor on open-weight model reliability?**

The honest answer so far, measured on this repo's own benchmark harness: less than the optimistic pitch. Small edge models (2.7B–8B, quantized, on consumer hardware) struggled badly on frontier benchmarks — qwen2.5:7b scored 29.8% single-call on GPQA Diamond, barely above the 25% four-option chance floor — and forcing terse, dense responses improved output only marginally. What *did* move the needle, and by how much, is documented in [Findings](#findings) below. Negative results are results; they're all here.

The second research goal is **self-learning without weight updates**: can an AI system's specialization grow over time when the only mutable substrate is playbooks, retrieval indexes, prompt templates, and routing topology — never fine-tuning? Sliding-window pass-rate snapshots are the learning signal; calibration tables test whether a model's self-reported confidence is informative enough to act on.

**Status: personal research project.** Solo-built, concentrated build (v1 → v3.5, May 2026). Not production software, no users, no support promises. The next milestone (v4) is a calibration-correction layer driven by grader/user feedback — the keystone that the v3.5 measurements showed is load-bearing, not optional.

## Architecture

A task enters as a root **action potential (AP)** — a lean JSON envelope. Every AP runs the same two-step contract:

1. **Evaluate** — a cheap LLM call picks one of five playbooks (`plan`, `process`, `critique`, `verify_grounded`, `compose`) for this input.
2. **Execute** — runs the chosen playbook. It either returns a result, emits a verifier signal to the runtime, or emits sub-APs — recursive invocations that form a call tree which dissolves when it returns.

Around that core:

- **Substrate adapters** (`pkg/adapter/*`) — five interchangeable backends behind one Evaluate→Execute interface: [Hermes-agent](https://github.com/NousResearch/hermes-agent) (wrapped, never modified), Ollama-direct, vLLM-direct, LM-Studio-direct, and the Anthropic API. Swapping substrates is a config change, which is what makes cross-substrate measurement honest.
- **VRAM governor** (`pkg/vram`) — multi-platform GPU/unified-memory probe (nvidia-smi, rocm-smi, Apple Silicon, Linux DRM/meminfo) plus a per-call KV-cache estimator computed from real transformer architecture, not magic numbers. Every component doing inference acquires leases from one shared governor; concurrency is library-managed by default and falls back to serial when the probe fails.
- **Cope dispatcher** (`pkg/cope`) — a rule table that reads calibrated confidence plus stakes and picks one of `Ship` / `ShipWithCaveat` / `Escalate` (re-run on a frontier model) / `Refuse`. Pure decision function, no I/O.
- **Inhibitors** (`pkg/inhibitor`) — circuit-breakers attached to call-tree edges (depth caps, confidence drops, repetition, contradictions), with strict cancellation across concurrent siblings.
- **Verifier + judge layers** (`pkg/verifier`, `pkg/judge`) — sampled critique injection whose rate adapts to a Wilson-bound happiness signal from frontier-judge audits.
- **Curation loop** (`pkg/curation`, `pkg/exemplar`, `pkg/adapter/curated`) — cold-path mining of benchmark streams into per-action exemplar stores; warm-path BM25 retrieval prepends the best exemplars as few-shot context. This is the no-weight-update learning substrate.

The naming is borrowed from neuroscience — cheap models wrapped as "oscillators," handoffs as "action potentials," uniform nodes like uniform cortical microcircuits with specialization coming from what feeds in rather than different circuitry. The metaphor guided the design; the artifacts above are just Go interfaces and stand on their own.

## What's here and testable

- **5 substrate adapters** under one contract, each with httptest-backed unit tests.
- **Benchmark harness** (`pkg/benchmark`, `cmd/bench`) — GPQA Diamond (198 cases), MMLU-Pro, and MATH-500 loaders; Single / Vote-N / Tree / Coping orchestrators; failure categorization; confidence-calibration tables; sliding-window pass-rate curves; cost tracking with frontier-counterfactual ledgers. Dataset snapshots are operator-downloaded (see `oscillitron/cmd/bench/cases/README.md`).
- **Observability** — structured `slog` tracing with context-propagated correlation IDs, optional OpenTelemetry (OTLP) export, and crash-safe JSONL streaming of per-case bench results.
- **Test discipline** — `go test -race ./...` green across all ~40 packages; gofmt + `go vet` pre-commit hooks in `scripts/git-hooks/`.

## Quickstart

Requires Go 1.26+. The Go module lives in the `oscillitron/` subdirectory:

```bash
git clone https://github.com/jrlmx2/oscillitron
cd oscillitron/oscillitron
go build ./...
go test ./...

# Offline demo — stub adapter, full call tree (plan → process×3 + critique → recompose):
go run ./cmd/oscillitron --task "outline a migration plan"

# Benchmark against a real local substrate (e.g. Ollama serving qwen2.5:7b-instruct-q6_K).
# Dataset snapshots are operator-downloaded — cmd/bench/cases/README.md has the recipe:
go run ./cmd/bench --benchmark gpqa --cases cmd/bench/cases/gpqa_diamond.json \
  --orchestrator-substrate ollama --orchestrator-model qwen2.5:7b-instruct-q6_K --limit 20
```

The demo runs with no API keys and no local model. The bench needs a substrate — an OpenAI-compatible local server (Ollama / vLLM / LM Studio), a Hermes gateway, or `ANTHROPIC_API_KEY` for the Anthropic adapter — plus a dataset snapshot (`gpqa`, `mmlu-pro`, or `math-500`). `cmd/bench/bench.properties.example` documents the full configuration surface.

## Findings

Everything below has benchmark evidence in this repo (run logs and analysis under `scratch/archive/bench-findings-*.md` and `references/`).

**Small open-weight models are far below frontier on hard-science reasoning.** GPQA Diamond, 198 cases, single-call: qwen2.5:7b-instruct-q6_K 29.8%, phi4-mini 26.3% — against a 25% chance floor. Claude Haiku 4.5 scored 37.9% on the same harness. The gap between an edge model and even a small frontier model (+25–35pp, depending on benchmark) dwarfs anything orchestration recovered.

**Structured-output enforcement can cost more than it buys.** Forcing JSON schema output cost 15–55pp on reasoning benchmarks for an 8B model — GSM8K went from 0% to 55% when the contract switched to natural text plus a trailing `confidence:` line. The model could do the arithmetic or emit valid JSON, not both. The exception: many-option MCQ (MMLU-Pro) got *worse* under natural text (24% → 10%) because answer extraction got harder. Format constraints and reasoning compete for small-model capacity; choose per task shape.

**Majority voting helps weak substrates and hurts strong ones.** Vote-5 vs single-call: +2.0pp on qwen2.5:7b, +3.0pp on phi4-mini — at ~5.1–5.2× token cost. On Haiku 4.5 it *lost* 1–3pp. Voting is variance reduction; when first-attempt errors dominate it pays, and when the substrate is already mostly right it averages gains away.

**Self-reported confidence is catastrophically miscalibrated — and worse, non-monotone on small models.** High-confidence-band (≥0.85) answers passed at 28.7% (qwen) and 25.6% (phi) against mean stated confidence of ~0.93: a 65–67pp overconfidence gap. Haiku's gap was ~38pp but *monotone* (higher confidence did mean higher pass rate), so a fixed offset would roughly fix it; the small models' calibration was inverted, which an offset cannot fix. Voting un-inverted the shape but barely moved the gap.

**Confidence-gated escalation is dead on arrival without calibration correction.** The cope dispatcher escalates to a frontier model below 0.5 confidence. Across 396 small-model cases, zero escalations and zero refusals fired — the models never doubted themselves enough. (Haiku escalated twice in 198 cases.) This is the empirical motivation for v4: corrected confidence, learned per (model, domain, confidence-band) from grader feedback, is the missing substrate that makes escalation routing useful.

**A lot of "too weak for the benchmark" is actually format breakage.** phi4-mini's early GPQA runs scored 13.6%, but 71% of failures were answers with no parseable letter; conditional accuracy when it did commit was 47% — Haiku-comparable. A format-recipe prompt suffix was worth more than any ensemble trick. Ordering of observed impact: format recipes > substrate choice > voting.

**Agentic envelopes crush small models.** Hermes injects ~14k tokens of tool/skill boilerplate per call; on a ≤7B model the same probe prompt produced 33 characters of gibberish through the agent envelope vs 1,487 characters of coherent reasoning direct to Ollama. Hence the direct adapters, and a documented capability floor (≥7B instruct, ≥Q5_K_M, reliably honors `response_format`): the library coordinates substrates above the floor rather than compensating for ones below it.

**Input tokens are the cost; the KV cache is the economics.** Orchestration-shaped calls are ~10–20k tokens in, tens of tokens out — 95%+ of spend is input. Measured on Apple Silicon: 206s cold call vs 7.6s warm on the same 14k-token prompt, and 38.2s when the instruction prefix changed. Stable prompt prefixes aren't a nicety; they're a 5–27× wall-clock lever.

**Misc, measured:** terse/dense response forcing ("Be terse and dense." system preamble) cut per-call output tokens ~72% but improved pass rates only marginally; Q4_K_M vs Q6_K quantization is worth ~1–3pp on these benchmarks; deterministic alphabetical vote tie-breaking silently biased weak-substrate results toward "A" (two-thirds of wrong votes) until fixed; LM Studio autonomously reverts model settings mid-run and shares context across parallel sessions, making Ollama the stable choice for unattended multi-hour benches.

## Repository layout

```
oscillitron/        Go module (the runtime) — start at oscillitron/CLAUDE.md for the package inventory
references/         operator/evaluator guides (performance sizing, VRAM coverage, substrate routing, capability floor)
scratch/            working design notes and dated benchmark findings (archive/ holds the empirical record)
docs/, inputs/      design plans and original source material
```

This repo doubles as the project's lab notebook — design decisions are locked with dates in `CLAUDE.md`, and the benchmark findings that drove each architectural turn are preserved as dated documents rather than rewritten history.

## Roadmap

v4: calibration-correction layer — per-(model, domain, confidence-band) correction of self-reported confidence, learned first from benchmark grader feedback and later from user-feedback intake, so the escalate path routes on signal instead of noise. Design draft in `scratch/v4-design.md`.

## License

MIT — see [LICENSE](LICENSE).
