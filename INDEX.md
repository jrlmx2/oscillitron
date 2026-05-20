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

## Skills

(empty)

## Key artifacts

- [Monetization analysis v1 (cost-wedge preservation)](artifacts/monetization-analysis.md) — load when revisiting business-model decisions, pricing-model questions, license choice (Apache vs. AGPL), the §11 thesis in framework-design.md, sequencing of consulting / BYO-keys / vertical packaging / compliance-subscription monetization, self-hosted vs. API economics by hardware tier, the cost-quality tension on quality-matching workloads, or the lift-based value proposition framing. Stress-tests framework-design §11 with unit-economics modeling under stated mid-2026 assumptions; §10 covers bootstrap-and-signal-gated-expansion posture; §11 covers self-hosted capex/hosting economics with break-even analysis; §12 covers the three-tier execution model and workload-mix-dependent positioning; §13 reframes the value claim from "match frontier" to "measurable per-model quality lift" with Phase 1 acceptance criteria; assumption sheets in §8, §11.7, §12.9, and §13.8.
