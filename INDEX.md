<!-- CLAUDE GENERATED -->
# Project Index

Single entry point for everything Claude might load in this project. One line per resource:
`- [Title](path) — when to load this.`

## Code subproject

- [oscillitron/ — Go runtime](oscillitron/CLAUDE.md) — load when working on Go code, the AP/router skeleton, the runner loop, or any of the pkg/* packages. Subproject CLAUDE.md is more specific than the root for anything code-side.
- [oscillitron/ — quick start](oscillitron/README.md) — load when you just need build/run/test commands.

## References

- [Library Plan v0.2 (Phase 1 + Phase 2 thin skeleton, wrap-Hermes architecture)](scratch/library-plan.md) — load when designing the Go module layout, deciding what to build before/after the Phase 1 kill-or-proceed gate, sequencing the first week of code work, or reconciling v0.1's vLLM-direct/compliance framing with v0.2's wrap-Hermes architecture. Contains an explicit v0.1 → v0.2 reconciliation section at the top.
- [Working design notes](scratch/design-notes.md) — load when iterating on specialist lifecycle, action-potential shape, inhibition design, summarizer/handoff conventions, or any architectural decision still in flight.
- [Performance & sizing guide (operator-facing)](references/performance-operator-guide.md) — load when someone asks what hardware to buy, which model to point at, why their first call is slow, why a smoke test takes 7 minutes, how to size for Phase 1 lift measurement, or whether a discrete GPU is worth it vs. a frontier hosted API. Concrete numbers from the 2026-05-20 live Hermes smoke; decision tree by use case (iteration vs. lift measurement vs. self-host past Phase 1 vs. production at scale); recipes for `~/.hermes/config.yaml` and `~/.hermes/.env`.
- [Cost dynamics narrative (evaluator-facing)](references/cost-dynamics-narrative.md) — load when explaining the cost-wedge thesis to evaluators, investors, or prospective customers; when answering "doesn't a faster GPU collapse your advantage?" or "why not just use the frontier API?"; when framing the relationship between substrate choice (small local vs. frontier) and orchestration value. Cross-references monetization-analysis §11/§12/§13. Same dynamics as the operator guide but pitched as "is the wedge real" rather than "what do I buy."
- [Hermes persona slim-down (operator-facing)](references/hermes-persona-slim-down.md) — load when someone is running Oscillitron against a local Hermes and needs to cut the 14k per-call boilerplate, reduce VRAM pressure, or understand which parts of Hermes' default persona Oscillitron actually needs. Deeper than the perf-operator-guide's top-line "trim toolsets" lever: token-share breakdown per category, keep-or-cut decisions grounded in the wrap-Hermes architecture, KV-cache VRAM math, verification probe, and an honest list of what you give up.
- [VRAM probe platform coverage & limitations](references/vram-platform-coverage.md) — load when sizing concurrency for a deployment, debugging why `pkg/vram` returned a particular number, or deciding whether `--vram-budget` should be set explicitly. Coverage matrix per platform/GPU vendor (NVIDIA, AMD/ROCm, Apple Silicon, Intel Arc, generic Linux, Windows+non-NVIDIA), probe priority order, what the probe doesn't answer (per-process usage, fragmentation, multi-GPU placement), and the operator override escape hatch.
- [Phase 1 measurement guide](references/phase1-measurement-guide.md) — load when running the kill-or-proceed gate or interpreting its verdict. Covers the workload choice (email drafts; mundane + nuance; verification-only; tools-and-connectors after), how to run `cmd/phase1`, what the quality/cost ratios mean against the §2.5 thresholds, the Haiku-as-hosted-cheap-proxy caveat (real cost-wedge measurement waits for a local cheap substrate), and how to interpret each verdict.
- [Substrate routing (small models bypass Hermes)](references/substrate-routing.md) — load when configuring `cmd/bench`'s `--orchestrator-substrate`, deciding whether to point a small model through Hermes or Ollama-direct, or extending the small-model allowlist. Covers why small substrates need Ollama-direct (Hermes's agentic envelope crushes ≤7B models per Hermes's own docs), the `auto` heuristic in `cmd/bench/main.go:resolveSubstrate`, and the empirical evidence (33-char gibberish via Hermes vs. 1487-char coherent reasoning via Ollama-direct on the same probe prompt).

## Skills

(empty)

## Key artifacts

- [Monetization analysis v1 (cost-wedge preservation)](artifacts/monetization-analysis.md) — load when revisiting business-model decisions, pricing-model questions, license choice (Apache vs. AGPL), the §11 thesis in framework-design.md, sequencing of consulting / BYO-keys / vertical packaging / compliance-subscription monetization, self-hosted vs. API economics by hardware tier, the cost-quality tension on quality-matching workloads, or the lift-based value proposition framing. Stress-tests framework-design §11 with unit-economics modeling under stated mid-2026 assumptions; §10 covers bootstrap-and-signal-gated-expansion posture; §11 covers self-hosted capex/hosting economics with break-even analysis; §12 covers the three-tier execution model and workload-mix-dependent positioning; §13 reframes the value claim from "match frontier" to "measurable per-model quality lift" with Phase 1 acceptance criteria; assumption sheets in §8, §11.7, §12.9, and §13.8.
