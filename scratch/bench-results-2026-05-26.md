# Benchmark Results — 2026-05-26

Model: llama3.1:8b-instruct-q6_K (local Ollama)
Config: Vote-1, per-benchmark regex extractors, goal="Describe the intent of the following prompt", SystemPreamble="Be terse and dense.", natural text for process/compose (no JSON enforcement)

See `scratch/archive/bench-findings-2026-05-26.md` for the full session analysis including the JSON→natural text discovery and commit list.

## Final results (20-case each, natural text)

| Benchmark | Frontier (Single) | Vote-1 | Tree |
|-----------|-------------------|--------|------|
| **SB** | **90%** (18/20) | 85% (17/20) | 80% (16/20) |
| **ARC** | **85%** (17/20) | 70% (14/20) | 80% (16/20) |
| **GSM8K** | **55%** (11/20) | **55%** (11/20) | 50% (10/20) |
| **MMLU-Pro** | **10%** (2/20) | **10%** (2/20) | 5% (1/20) |

## Key finding

**JSON structured output was costing 15-55pp on every benchmark.** Removing it produced the single largest quality improvement in the project's history. GSM8K went from 0% to 55%.

## Tree still trails frontier

Tree loses to frontier on every benchmark. Root cause: decomposition rarely fires (model picks process over plan on most cases), and when it does, the recomposition doesn't reliably outperform a single call. The evaluate step + goal-as-outputSchema adds noise without signal on simple MCQ.

## What's still broken

- **MMLU-Pro** dropped from 24% (JSON) to 10% (natural text) — 10-option MCQ (A-J) extraction from natural text less reliable
- **Tree value** — orchestration overhead not yet justified
- **LLM extraction** — built but not wired; needs short-response fast path
- **20-case sample noise** — ~5-10pp variance between runs
