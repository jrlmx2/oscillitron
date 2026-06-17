# Benchmark Results — 2026-06-16

Experiment: **H0-SE** (Thread B keep/cut — semantic-entropy vs self-reported confidence) + **H0-cost local arm**, on the best comfortable-fit local model for this laptop (Apple M3 Pro, 18 GB unified).

Model: **qwen3:14b** (Qwen3-14B, Q4_K_M, via Ollama) — 11 GB @ 24k ctx, 100% GPU, ~7 GB headroom.
Config: GPQA Diamond, `--limit 40`, Vote-5, `--thinking off` (non-thinking), `--structured-output` on, SystemPreamble="Be terse and dense.", `--governor-ceiling 1` (serial), frontier arm pointed at local qwen3:14b (single-call baseline; no API key available so no hosted/frontier arm).
Raw machine scores: `scratch/h0-se-qwen3-14b-2026-06-16.json` (Report) + `scratch/h0-se-qwen3-14b-2026-06-16.jsonl` (per-case stream).

> Status: **COMPLETE** — 40/40 cases (~16h wall, ~24.6 min/case; qwen3's reasoning verbosity is the bottleneck). Final numbers below.

## Vendor-reported baseline (Qwen3 Technical Report, Qwen Team, arXiv:2505.09388)

GPQA-Diamond, Qwen3-14B (full-precision BF16, pass@1 single-sample):

| Mode | Vendor GPQA-Diamond | Source |
|---|---|---|
| **Non-thinking** | **54.8** | Table 16 |
| Thinking (reasoning) | 64.0 | Table 15 |

**Our run is thinking-OFF, so 54.8 is the comparable figure.** (Thinking-budget scaling — Fig. 2 — shows GPQA Diamond rising 64→72 as the thinking budget grows to 32k tokens; that's the reasoning-mode ceiling, not our regime.)

### Comparability caveats (why our number isn't a like-for-like of 54.8)
- **Quant:** we run Q4_K_M; vendor is full precision (~1–3 pp typical loss).
- **Protocol:** we run **Vote-5** (5 samples, majority vote) — a variance-reduction boost over vendor **pass@1** single-sample. Our number should sit *above* 54.8 from voting alone.
- **Sample size:** interim n is tiny; full run is n=40 vs vendor's full 198-question GPQA Diamond.
- Net: treat 54.8 as a **reference floor for non-thinking single-call**, not a target our Vote-5/Q4 run must match exactly.

## Final results (40 cases)

### Pass rates
| Arm | Pass | vs vendor non-thinking (54.8, pass@1 BF16) |
|---|---|---|
| **Vote-5** | 26/40 = **65.0%** | +10.2 pp (voting lift + Q4) |
| **Frontier (single qwen3)** | 23/40 = **57.5%** | +2.7 pp — ≈ vendor, as expected for single-call |

Vote-5 beats single-call by **+7.5 pp** — voting helps this substrate on GPQA (consistent with the README finding that voting helps weak/mid substrates). Single-call 57.5% lands right on the vendor non-thinking 54.8 (small-sample + Q4 noise), a good sanity check that our harness reproduces the vendor regime.

### H0-SE — semantic entropy vs self-reported confidence (THE decision)
Calibration from `--report-out` (lower ECE/Brier = better):

| Confidence column | n | ECE | Brier | slope |
|---|---|---|---|---|
| self-reported (`confidence`) | 39 | 0.2387 | 0.2629 | +1.574 |
| **semantic-entropy (`se_confidence`)** | 40 | **0.1470** | **0.1727** | +1.342 |
| (frontier single, self) | 27 | 0.1630 | 0.1928 | +0.282 |

- **ECE-delta (self − SE) = +0.0917** — 3× the `≥ 0.03` build threshold.
- **Brier-delta = +0.0902** (SE better).
- **False-confident WRONG ships** (conf ≥ 0.85 on a case Vote-5 got *wrong*): self-reported = **9**, semantic-entropy = **2**. SE *cuts* false-confident ships 9→2 — satisfies the gate's "no extra false-confident high-stakes ships" condition (strictly fewer).
- self-report's slope is marginally steeper (+1.57 vs +1.34) — its *ranking* is a hair more informative — but ECE/Brier (the primary metrics) and the false-confident count all favor SE decisively.

**VERDICT: H0-SE → BUILD / KEEP semantic entropy.** Delta clears the threshold 3×, and SE roughly quarters false-confident ships. Thread B (#76) is already merged, so this **validates keeping it** and supports making `--cope-confidence-source semantic-entropy` the default for the cope dispatcher (currently defaults to `self`).

> Caveat: n=40 (vendor noise floor is n≈198); discrete exact-match SE on MCQ is the easy case (SE = 1 − H/ln(N) over the vote histogram). The free-form/NLI-clusterer arm (design §2.12.6) is where SE's meaning-clustering would be properly stress-tested — not run here.

## Still pending
- **H0-cost (hosted-Haiku arm):** BLOCKED — no `ANTHROPIC_API_KEY`. Local Vote-5-vs-single arm rides this run; `--frontier-price 4.50` gives the counterfactual savings column.
- **H0-router (Thread A):** needs #78 code + a hand-seeded multi-action store + an unconstrained-Evaluate walk (`cmd/oscillitron --router`). GPQA is inert-by-construction for it (design §2.12.9). Not yet run; execution must wait for the GPU (avoid contending with this run).
