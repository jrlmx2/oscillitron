<!-- CLAUDE GENERATED -->

# v4 design — calibration-correction layer

**Status:** *draft, 2026-05-23.* Reframed from "user-feedback intake" to "calibration-correction substrate" after the 2026-05-23 bench findings (`scratch/archive/bench-findings-2026-05-23.md`) showed the v3.4 escalate path was dead on every substrate tested. v4 is no longer a speculative future layer — it is the missing piece that makes the v3 chain useful.

## What changed today

Three empirical findings reframe v4:

1. **Overconfidence is universal.** qwen2.5:7b (65 pp gap), phi4-mini (67 pp), Haiku 4.5 (~40 pp). Smaller substrates have larger gaps, but every substrate tested is significantly miscalibrated on GPQA hard science.
2. **The escalate path is functionally dead.** Across 446 cases on three substrates (qwen 198 + phi 198 + Haiku 50), zero escalations triggered, zero refusals. Because mean confidence never dropped below the 0.5 floor, the cope dispatcher never reached for the frontier. The architecture works; the substrate's *raw* confidence is unusable as a gating signal.
3. **Voting effects are substrate-dependent.** vote-5 beats single-call on qwen (+2.0 pp) and phi (+3.0 pp); on Haiku 4.5 vote-5 **loses** 3.0 pp. The v3 voting premise is not universal; it's a variance-reduction trick that pays off on noisy substrates and costs on already-confident ones.

The implication for v4: **the cope dispatcher needs to read *corrected* confidence, not raw confidence.** With a learned correction, the escalate path comes back to life — qwen's reported 0.93 maps to a true ~0.32, which would correctly route high-stakes cases to escalate.

## Goal

Add a calibration-correction layer between the substrate's raw confidence and the cope dispatcher's threshold check. The correction is:

- **per-(model, domain, raw-conf-band)** — Haiku-on-MMLU-Pro behaves differently from Haiku-on-hard-science.
- **grader-feedback-driven first** — we have benchmark ground truth; we don't need real user feedback to prototype this. Real-user-feedback intake stays scoped as a *future v5* signal source.
- **online-updatable but checkpoint-loadable** — operators run a bench, the resulting (raw_conf, pass) pairs update the correction store; v4 emits checkpoints the cope dispatcher loads on next run.

## Non-goals

- Real user feedback intake (was the old v4 scope) — deferred to v5.
- Per-question calibration ("for this *specific* question type the model is overconfident") — too fine-grained for the data volume we'll have early. Per-(model, domain, band) is the v4 granularity.
- Replacing the v3.3 confidence extraction or the v3.2 effective-confidence multiplicative downgrades — v4 is a *post-processing layer*, not a replacement.

## Architecture

```
Raw substrate response
  ↓
v3.3 NormalizeConfidence (percent → decimal)            [already exists]
  ↓
v3.2 EffectiveConfidence (multiplicative signal adjusts)  [already exists]
  ↓
v4 CalibrationCorrection.Apply(model, domain, eff_conf)   [NEW]
  ↓
cope.RuleTable.Decide(corrected_conf, stakes)             [already exists]
```

v4 plugs in as a final-step transformation on the confidence value before it reaches the cope dispatcher. The dispatcher doesn't need to know — it still reads "a confidence number" and routes on it. The correction layer is *transparent to downstream*.

### Correction store

```go
package calibration

// Key uniquely identifies a calibration bucket. Three-dimensional:
// the substrate's model identity, the question domain (sourced from
// case metadata; for GPQA Diamond this is the subdomain field), and
// a raw-confidence band that buckets continuous values into discrete
// regions where calibration is roughly linear.
type Key struct {
    Model        string // e.g., "qwen2.5:7b-instruct-q6_K"
    Domain       string // e.g., "physics", "chemistry", "biology"
    RawConfBand  string // e.g., "high" (≥0.85), "medium" (0.50–0.85), "low" (<0.50)
}

// Observation is one (raw_confidence, pass_or_fail) data point from
// a bench run. Feeds the correction store.
type Observation struct {
    Key         Key
    RawConf     float64
    Pass        bool
    Timestamp   time.Time
}

// Bucket aggregates observations for one Key into the running
// calibration statistics needed to produce a correction.
type Bucket struct {
    Key            Key
    SampleCount    int
    SumRawConf     float64   // for mean
    PassCount      int       // for pass rate
    LastUpdated    time.Time
}

// MeanRawConf returns SumRawConf / SampleCount.
// PassRate returns PassCount / SampleCount.
// Correction returns MeanRawConf − PassRate (the offset to subtract).

// Store is the persistence interface. JSON-file-backed in v0; future
// SQLite or remote KV when data volume motivates it.
type Store interface {
    Add(Observation) error
    Get(Key) (Bucket, bool, error)
    Snapshot() ([]Bucket, error)  // for offline analysis / debugging
}
```

### Read path (apply correction at inference time)

```go
// Corrector reads from a Store and applies the per-bucket offset.
type Corrector struct {
    Store              Store
    MinSampleCount     int     // below this, return raw conf (don't trust the correction)
    MaxCorrection      float64 // clamp; defaults to 0.95 (correction can't reduce conf below 0.05)
}

// Apply returns the corrected confidence. Pass-through (returns raw)
// when the bucket has insufficient samples — caller can't tell the
// difference shape-wise.
func (c Corrector) Apply(model, domain string, rawConf float64) float64 {
    band := bandFor(rawConf)
    bucket, ok, err := c.Store.Get(Key{Model: model, Domain: domain, RawConfBand: band})
    if err != nil || !ok || bucket.SampleCount < c.MinSampleCount {
        return rawConf // bootstrap / no data path
    }
    correction := bucket.MeanRawConf - bucket.PassRate()
    corrected := rawConf - correction
    if corrected < 0 {
        return 0
    }
    if corrected > 1 {
        return 1
    }
    return corrected
}
```

### Write path (feedback from bench / grader)

The bench's existing `RunnerConfig.OnCase` hook already streams per-case results. v4 adds an `OnCase` consumer that converts each CaseResult into an Observation and pushes it to the Store:

```go
// In cmd/bench, after grading:
calibration.RecordObservation(store, calibration.Observation{
    Key: calibration.Key{
        Model:       orchestrator.ModelName(),
        Domain:      caseResult.Case.Metadata["subdomain"],
        RawConfBand: bandFor(caseResult.Answer.RawConfidence), // pre-correction band
    },
    RawConf:   caseResult.Answer.RawConfidence,
    Pass:      caseResult.Verdict.Pass,
    Timestamp: time.Now(),
})
```

This means **every bench run is automatically a calibration-update run**. The first run produces empty correction; subsequent runs benefit from prior runs' data. Cold start → warm state transition is just "run the bench enough times for samples to accumulate."

### Bootstrap behavior

`MinSampleCount` gates whether the correction applies. v0 default: 20 observations per bucket. Below the floor, `Apply` returns raw confidence unchanged — cope dispatcher uses today's logic, escalate path stays dead until calibration data accumulates.

For substrates / domains the operator wants to "ship in" pre-calibrated, the Store accepts hand-crafted buckets (operator-curated calibration is a valid input — same shape as bench-derived data).

## Integration points

| Layer | Change |
|---|---|
| `pkg/calibration` (new) | The Store + Corrector + Observation types above. JSON-file-backed v0. |
| `pkg/benchmark/orchestrator.Coping` | Optional `Corrector` field. When set, `Answer()` applies correction to `inner.Confidence` before passing to `rules.Decide`. |
| `cmd/bench` | New `--calibration-store DIR` flag. When set, every per-case result feeds the store. When the same dir is passed to a subsequent run, the Corrector wraps the orchestrator's confidence on the read path. |
| `pkg/benchmark/calibration` | Existing table-rendering code stays; gains a "corrected confidence band" column when a Corrector is wired, so operators can eyeball calibration before/after side-by-side. |

The cope dispatcher's `RuleTable.Decide` does not change. The dispatcher still reads a confidence number and routes on bands. The correction layer is upstream.

## Open questions

- **Domain key sourcing.** GPQA Diamond's subdomain field is already in case metadata. MMLU-Pro has a category field. MATH-500 doesn't have an obvious domain — math is math. Either coarse-grained (one bucket per benchmark) or skip domain dimension when the source doesn't supply it.
- **Sample-count threshold.** 20 is a guess. At 198 cases × 3 stakes × 3 bands × N subdomains, the floor matters for whether early benches contribute. Revisit empirically.
- **Band edges.** Currently re-use cope's 0.85 / 0.5 thresholds. Could mean buckets are too coarse — high-band conf 0.86 and 0.99 may calibrate very differently. Test with 5-band breakdown first; expand if data justifies.
- **Stakes dimension.** Today's data shows stakes doesn't shift calibration much (the model doesn't *know* the stakes). Keep it out of the Key for now; revisit if v4.1 finds a stakes×calibration interaction.

## What this changes for the existing v3 chain

Nothing breaks. The new layer is opt-in via `--calibration-store`:
- No flag → bench runs identically to today; cope dispatcher reads raw confidence; escalate path stays dead on overconfident substrates (current behavior).
- Flag set on a new bench → correction applies where there's data; first run accumulates observations.
- Flag set on a re-run with existing data → calibration data drives the dispatcher; escalate path becomes live as soon as buckets cross the sample threshold.

## Cut from v4 scope

- **Real user feedback ingestion.** Was the old v4. Now scoped to v5. The architecture above generalizes: a `UserFeedbackObservation` becomes another writer to the same Store, with a slightly different signal interpretation (user "this was wrong" → Pass=false). v5 will look like "add another writer."
- **Curating calibration data across benchmarks.** Cross-benchmark transfer ("Haiku on MMLU-Pro biology informs Haiku on GPQA biology") is appealing but unproven and not load-bearing for v0. Keep buckets benchmark-tight in v4.
- **Online learning of band edges.** v4 uses fixed bands; v5 could learn band edges from the data itself.
- **Stake-aware calibration.** See open question above.

## Implementation plan (after v3.5 bug bundle merges)

1. **pkg/calibration** — Store interface, FileStore (JSON-backed), Corrector. ~250 lines + tests. (One PR.)
2. **Wire into pkg/benchmark/orchestrator.Coping** — optional Corrector field. ~50 lines + tests. (Same PR as #1 if small; otherwise follow-up.)
3. **cmd/bench --calibration-store** — flag + integration. ~50 lines. (Same PR as #2.)
4. **Bench re-runs** — qwen / phi / Haiku × GPQA Diamond / MMLU-Pro / MATH-500 with calibration store enabled. The matrix run after the loaders land. First pass produces ~9 sets of bucket observations; v0 ships with that.
5. **Documentation update** — root CLAUDE.md "Status" line updates to "v4 calibration-correction wired"; new `references/calibration-correction.md` operator guide.
