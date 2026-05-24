<!-- CLAUDE GENERATED -->

# Bench findings — 2026-05-23

Post-session record of the v3.5 stack's first full-scale empirical
test. Companion to `scratch/archive/session-2026-05-23-handoff.md`
(this morning's handoff into the session that produced the runs)
and the predecessor `bench-findings-2026-05-22.md` (which framed
the v3 redesign that v3.5 implements).

**The question:** does the v3 chain (notice → cope → escalate)
deliver measurable orchestration lift on cheap substrate at
198-case scale, with v3.5's structured-output schema enforcement
closing the format-leakage gap that dominated 2026-05-22?

**The answer (one line):** the orchestration premise survives —
vote-5 beats single-call by 2–3 pp on both substrates at ~5×
token cost — but the cope dispatcher's escalate path is
functionally dead, because both models are catastrophically
overconfident and never dip below the escalate threshold. v4
calibration-correction is now the load-bearing next step, not
optional.

---

## 1. Run setup

| | |
|---|---|
| Benchmark | GPQA Diamond, 198 cases |
| Stack | `--vote-n 5 --stakes rotate --cope --notice --minimal-output --structured-output` |
| Orchestrator substrate | ollama-direct (`pkg/adapter/ollama`) |
| Orchestrator = Frontier | Same model on both arms (self-escalation pattern) |
| Models tested | `qwen2.5:7b-instruct-q6_K` and `phi4-mini:latest` |
| Cope thresholds | HighConfidence=0.85, LowConfidence=0.5 (DefaultRuleTable) |
| Sliding window | 25 |

Run files:

- qwen: `/tmp/v35-final.log`, `/tmp/v35-final.jsonl`, `/tmp/v35-final-report.json`
- phi:  `/tmp/v35-phi.log`,   `/tmp/v35-phi.jsonl`,   `/tmp/v35-phi-report.json`

---

## 2. Aggregate results

### qwen2.5:7b-instruct-q6_K

| Metric | frontier (single-call) | cope-vote-5 |
|---|---|---|
| pass | 59 / 198 = **29.8%** | 63 / 198 = **31.8%** |
| calls | 198 | 1,056 |
| tokens | 61,129 | 319,339 |
| token cost ratio | 1.0× | 5.2× |
| **Uplift vs frontier** | — | **+2.0 pp (+4 cases)** |

### phi4-mini-latest

| Metric | frontier (single-call) | cope-vote-5 |
|---|---|---|
| pass | 52 / 198 = **26.3%** | 58 / 198 = **29.3%** |
| calls | 198 | 1,056 |
| tokens | 59,165 | 304,641 |
| token cost ratio | 1.0× | 5.1× |
| **Uplift vs frontier** | — | **+3.0 pp (+6 cases)** |

---

## 3. Failure categorization

```
                       qwen-frontier  qwen-cope  phi-frontier  phi-cope
pass                          59          63           52          58
format_no_letter               1           0           12           6
wrong_letter                 138         135          134         132
refusal                        0           0            0           0
empty_response                 0           0            0           2
adapter_error                  0           0            0           0
```

Three things to notice:

1. **`wrong_letter` dominates on every arm** — the model committed to a letter and got it wrong. Not a format issue, not a refusal, not a hedging issue. It just doesn't know the answer.
2. **Structured-output schema enforcement is substrate-dependent.** qwen has 1 `format_no_letter`; phi has 12. The schema constrains the engine but a smaller model is more likely to violate the schema (or produce a JSON-valid response with a non-letter `answer` field that the Multichoice grader can't extract). Voting roughly halves phi's format-broken count (12 → 6).
3. **The `empty_response=2` on phi cope** is an interesting edge case — cases where all 5 vote attempts produced empty extractions, so the vote returned empty. Worth a closer look in a follow-up, but ~1% incidence so not load-bearing.

---

## 4. Confidence calibration

### qwen2.5:7b

```
frontier
  medium (0.50-0.85)   n= 34   pass=35.3%   mean_conf=0.79
  high   (>=0.85)     n=164   pass=28.7%   mean_conf=0.94    ← INVERTED

cope-vote-5
  medium (0.50-0.85)   n= 19   pass=26.3%   mean_conf=0.81
  high   (>=0.85)     n=179   pass=32.4%   mean_conf=0.93
```

### phi4-mini

```
frontier
  low    (<0.50)        n=  1   pass= 0.0%   mean_conf=0.10
  medium (0.50-0.85)   n= 24   pass=33.3%   mean_conf=0.77
  high   (>=0.85)     n=172   pass=25.6%   mean_conf=0.93    ← INVERTED

cope-vote-5
  low    (<0.50)        n=  1   pass= 0.0%   mean_conf=0.10
  medium (0.50-0.85)   n= 29   pass=24.1%   mean_conf=0.79
  high   (>=0.85)     n=167   pass=30.5%   mean_conf=0.92
```

Both substrates exhibit **catastrophic overconfidence** on GPQA
hard science:

| | high-band mean conf | high-band pass rate | overconfidence gap |
|---|---|---|---|
| qwen frontier | 0.94 | 28.7% | **65 pp** |
| qwen cope-vote | 0.93 | 32.4% | 61 pp |
| phi frontier | 0.93 | 25.6% | **67 pp** |
| phi cope-vote | 0.92 | 30.5% | 62 pp |

And both exhibit **calibration inversion** on the frontier path
(medium-band pass > high-band pass). Voting fixes the inversion
on both — vote-5's high-band is higher than its medium-band on
both substrates — but doesn't move the absolute overconfidence
gap meaningfully (only 3-5 pp).

**Voting tightens calibration; it does not fix it.**

---

## 5. Cope action distribution

```
                ship  ship_with_caveat  escalate  refuse
qwen-cope-5     179         19              0        0
phi-cope-5      167         31              0        0
```

The escalate and refuse paths **never fired** on either
198-case run. This is the most important finding of the day.

The cope dispatcher's escalate path is gated by `confidence <
LowConfidence` (default 0.5). Both substrates were so
overconfident that mean confidence essentially never dropped
below 0.5 — so the dispatcher always landed in either Ship
(≥0.85) or ShipWithCaveat (0.5–0.85), never Escalate (<0.5).

The v3.4 escalate path is **functionally dead at this substrate
scale**. The architecture works; the threshold-calibration data
is wrong. We can't trust this substrate's confidence values as
a gating signal, period.

This is the strongest possible empirical motivation for v4
(user-feedback intake → calibrated-confidence learning loop):
without learning that "this model's self-reported confidence is
inflated by ~60 pp on hard science," the cope dispatcher has no
useful signal to act on.

---

## 6. Bug-#2 empirical incidence (next-PR bundle item #2)

The code review's bug #2 — "empty-extraction attempts still
aggregate into Answer.Confidence" in `vote.go:183` — fires
dramatically more on the weaker model:

```
                        attempts*    bug-#2 firings    rate
qwen-cope-vote-5          ~990            1             0.1%
phi-cope-vote-5           ~990           35             3.5%

* 5 attempts × 198 cases, minus errors (which were ~0)
```

phi4-mini's reported `cope-vote-5 = 29.3%` is contaminated by
35 attempts where `extracted="" tokens>0 confidence>0` got
averaged into the per-case mean. Estimating impact: those 35
attempts span fewer than 35 cases (some cases had multiple
empty-extraction attempts). Assume 25 cases were affected;
removing the spurious confidence contributions could shift each
case's coping decision in either direction. Net effect on pass
rate is probably 1–2 cases — small but not zero.

**Recommendation:** re-run phi with bug #2 patched to validate
whether the numbers move. qwen's 0.1% rate is too low to detect
a real change at 198 cases.

---

## 7. Cross-model comparison — the punchline

| | qwen2.5:7b | phi4-mini |
|---|---|---|
| frontier pass | 29.8% | 26.3% |
| cope-vote-5 pass | 31.8% | 29.3% |
| vote uplift | +2.0 pp | +3.0 pp |
| token cost ratio | 5.2× | 5.1× |
| format_no_letter (frontier) | 1 | 12 |
| bug-#2 firings | 1 | 35 |
| escalations triggered | 0 | 0 |
| high-band overconfidence | 65 pp | 67 pp |

Five takeaways:

1. **The orchestration premise survives.** Voting beats single-call on both substrates. Worth the 5× token cost depends on the alternative (e.g., a stronger substrate at 1× cost is probably better — to be measured).
2. **Both models are calibration-broken on GPQA hard science.** The cope dispatcher can't be meaningfully gated by confidence at this substrate scale. Both substrates are ~60 pp overconfident; the absolute gap is essentially invariant to which model you pick.
3. **Voting tightens calibration but doesn't fix it.** The high/medium inversion disappears under voting on both substrates — strong evidence that voting isn't just averaging, it's pulling lower-confidence attempts off the high band. Useful but not load-bearing.
4. **Bug #2 matters more on weaker substrates.** 35× incidence on phi vs qwen quantifies why the next-PR bundle's #2 fix is operationally load-bearing for phi-class substrates.
5. **The escalate path is dead at this scale on both substrates.** The architecture is right; the threshold-calibration substrate is wrong. v4 calibration-correction is the keystone.

---

## 8. What this changes for v4

Before today, v4 was framed as "user-feedback intake — the
keystone for calibrated learning over time" (parent CLAUDE.md
§Status). Today's data sharpens the framing:

- **v4 is not optional infrastructure.** It is the missing
  substrate that makes the v3 chain useful. Without learning
  per-model / per-domain confidence corrections, the escalate
  path is dead weight.
- **The correction signal can come from grader feedback before
  we ever need real user feedback.** GPQA has ground truth.
  We can compute the per-model / per-band calibration error from
  the bench itself, feed it back into the cope dispatcher as a
  confidence-correction layer, and re-measure. This is a much
  cheaper v4 prototype than waiting for human-feedback intake.
- **The correction is likely high-dimensional.** qwen and phi
  both show ~60 pp overconfidence on GPQA hard science, but the
  correction needed on (say) MATH-500 or MMLU-Pro is unknown
  and almost certainly different. v4 needs per-(model, domain)
  buckets, not a single global multiplier.

---

## 9. Open / what's next

In rough priority order:

1. **Next-PR bundle (next session, small):**
   `coping.go:77` (#1), `vote.go:183` (#2), `response.go:167`
   (#3), `minimal.go:63` (#5). Sketches in
   `scratch/archive/session-2026-05-23-handoff.md` §"Code
   review findings". The #2 fix is empirically load-bearing on
   phi-class substrates; the others are correctness floors.

2. **Re-run phi after #2 fix.** Confirms whether the 1–2-case
   estimate is right.

3. **Sanity-check on a competent substrate.** Run the same
   198-case Diamond against Haiku or Sonnet via Anthropic API.
   If calibration is still broken there, v4 is even more
   load-bearing. If it's fine there, we've localized "broken
   calibration" to small open models.

4. **v4 design pass.** Per-(model, domain) confidence
   correction, grader-feedback-driven before user-feedback
   becomes available. Lives in `scratch/v4-design.md` (not yet
   written).

5. **Larger / discussion fixes from the code review:**
   - #4 multi-substrate raw-confidence parity (needs design
     pass: do hermes/vllm/lmstudio always go through structured
     output, or sometimes not?)
   - #6 cost double-counting on Escalate (operator-facing
     decision: actual blended rate, or frontier rate × all
     tokens?)
   - Hermes adapter parity for `response_format` (Hermes uses
     `/v1/runs`, no schema surface — text-only enforcement,
     soul.md tweak, or accept the gap).

---

## 10. Bench artifacts (where to look)

| Artifact | Path | Use |
|---|---|---|
| qwen JSONL | `/tmp/v35-final.jsonl` | Per-case results, 198 lines |
| qwen log | `/tmp/v35-final.log` | Full trace, aggregate at bottom |
| qwen report | `/tmp/v35-final-report.json` | snake_case JSON for replay |
| phi JSONL | `/tmp/v35-phi.jsonl` | Per-case results, 198 lines |
| phi log | `/tmp/v35-phi.log` | Full trace, aggregate at bottom |
| phi report | `/tmp/v35-phi-report.json` | snake_case JSON for replay |
| Watcher log | `/tmp/v35-watcher.log` | Confirms phi auto-launched 19:51 |

These are in `/tmp` and won't survive a reboot. If we want
durable copies for replay or comparison, copy into
`artifacts/bench-runs/2026-05-23/` before they vanish.
