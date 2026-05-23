<!-- CLAUDE GENERATED -->
# v3 design: inadequacy notice and coping

**Status:** working draft, 2026-05-23. v3 builds on v1 (substrate + orchestration floor) and v2 (per-action exemplar substrate + cold-path curation). Captured from the design conversation 2026-05-23. Sections marked **LOCK CANDIDATE** need user sign-off before migrating to `oscillitron/CLAUDE.md` as architecture locks.

---

## 1. Premise

- **v1 set the floor.** Orchestration on a cheap substrate (Haiku + voting + critique) matched Sonnet-class quality on Phase 1 email drafting. Proves orchestration adds capability even with no learning. Locked.
- **v2 built the substrate.** Per-action exemplar store (`pkg/exemplar`), cold-path curation driver (`pkg/curation`), warm-path retrieval consumer (`pkg/adapter/curated`). The infrastructure for accumulating per-customer knowledge over time.
- **v3 is the active piece — making the system human-shaped about its own inadequacy.** Not "make the LLM smarter." Make the *system* notice when the LLM is struggling and act accordingly.

The target market is small business. SMBs at 1–30M tokens/month don't care about throughput-at-scale; they care about (a) trustworthy outputs on tasks they can't afford to get wrong, (b) privacy/sovereignty for sensitive workloads, (c) predictable pricing, (d) a system that knows their business. v3 is the architecture that delivers (a) and (d). (b) and (c) are deployment/packaging concerns.

## 2. Load-bearing locks

### 2.1 General intelligence enhancement, NOT subject specialization **(LOCK CANDIDATE)**

We do not partition the architecture by subject domain (legal, medical, code). We organize around general intelligence enhancement per customer.

Rationale:
- The QoL layer is per-*customer*, not per-*domain*. "John Acme prefers brief emails" is this customer's knowledge, not the medical or legal industry's.
- Subject specialization pre-commits to a taxonomy we don't trust (same trap the brain-function lock avoided 2026-05-18).
- The base LLM already has subject-matter knowledge built in. Duplicating it in the wrapper is free competence we'd be paying to re-build.
- Subject-shaped flywheel fragments; per-customer flywheels run in parallel without fragmentation.

Commercial packaging can still be vertical ("Oscillitron for legal practices") — same engine, domain-tuned starting set of routing rules — without architectural specialization. This is a sales/positioning surface, not an architectural one.

Consistent with the brain-function lock from 2026-05-18 (one layer up).

### 2.2 Two-stage architecture: NOTICE → ACT **(LOCK CANDIDATE)**

The system does two distinct things, in order:

1. **Notice** — analyze the call (prompt + response) for signals of inadequacy. *What's happening?*
2. **Act** — based on signals, classification level, and accumulated calibration, pick a coping mechanism. *What do I do?*

Crucially, the notice layer analyzes **behavior**, not **correctness**. We can't always know whether the answer is right. We CAN always tell whether:
- The output followed the requested format
- The response length matched the request shape
- Instructed artifacts were present
- Internal consistency was maintained
- Structured output was well-formed
- The response stayed on-task

These signals are verifiable from first principles — by comparing instruction to output. The model itself tells us when it's struggling, in its behavior, even when it can't honestly tell us in its answer.

### 2.3 Behavioral signal extraction comes from BOTH sides **(LOCK CANDIDATE)**

A failure mode is not always the response's fault. The system can fail to set the model up for success. Notice-layer signals come from two sources:

- **Prompt-side signals** (pre-call): "is this prompt likely to crush this model?" The Hermes-overload story (4097-token envelope around a 122-token question) is detectable BEFORE the call by examining input/persona/schema ratios against the substrate's known limits.
- **Response-side signals** (post-call): "did the model behave consistently with instructions?" Format violations, hedging language, internal contradictions, self-corrections.

Both kinds feed the same assessment. Both are cheap. Both are verifiable without ground truth.

### 2.4 Confidence is first-class **(LOCK CANDIDATE)**

Confidence is the primary signal driving the act layer's decisions. It is:
- **Required on every output**, including in minimal-output mode (where the JSON envelope is dropped but confidence stays)
- **Not just "the model said 0.85"** — that's the model's self-report, which is uncalibrated. The system's *effective* confidence is derived from: model's self-report × behavioral signals × historical accuracy on similar (action, input-pattern, classification) tuples.
- **Surfaced to the user** in the final output, not buried in traces. Honest uncertainty signaling is the SMB-trust mechanism.

Per-pattern calibration (deriving effective confidence from history) requires user feedback. **Deferred to v4.** v3 ships with model-self-report confidence + behavioral-signal adjustment.

### 2.5 User feedback is the ground truth that calibrates everything — DEFERRED TO v4 **(LOCK CANDIDATE)**

Without a pathway for the user to signal "I edited this, here's what I changed" or "I rejected this entirely," the system has no calibration source for its confidence and no anti-pattern data to learn from. This is the v3-to-v4 keystone.

v3 builds the layers that can RECEIVE this signal (notice + act) without requiring it to ship value. v4 is the user-feedback intake design + the consumers that compound on top of it (calibrated confidence, routing oracle, anti-pattern detection).

The intake design is the gating decision for v4: where does the signal land? CLI flag? File? Daemon? HTTP endpoint? Bench-only? This question deserves its own design pass before any v4 code starts.

## 3. The notice layer: signal catalog

### 3.1 Prompt-side signals

| Signal | What it tells us | Detection cost |
|---|---|---|
| Input context size as % of model window | Substrate overload (Hermes story); the model may degrade before reasoning | Free — token count vs. `ModelSpec.ContextSize` |
| Persona/instructions size vs. content size | Wrapper is shipping more tokens than the question (Hermes again) | Free — split at message boundary |
| Input size vs. running median for this action | Anomalous input for this workload | Cheap — keep per-action rolling median |
| Output schema vs. substrate capability table | "asking for 5-key JSON from a 3.8B model" — high-risk combination | Cheap — per-substrate compatibility heuristic |
| Token budget vs. complexity heuristic | High budget on trivial input → wasteful; low budget on complex input → truncation | Cheap |
| Conflicting instructions | "be brief" + "show reasoning step by step" | Hard — defer |

### 3.2 Response-side signals

| Signal | What it tells us | Detection cost |
|---|---|---|
| Format violation | Asked for JSON → got prose; asked for letter → got essay | Cheap — pattern match against request shape |
| Required artifact missing | Closing letter, requested key, mandatory closing line | Cheap |
| Internal consistency violation | Claimed "100% sure" + hedging language ("I think", "maybe") | Cheap — co-occurrence regex |
| Length-vs-request mismatch | Yes/no question answered with 5 paragraphs | Cheap — length ratio |
| Self-correction mid-stream | "Actually, I'm not sure..." | Cheap — pattern set |
| Refusal language | "I can't reliably...", "outside my expertise" | Cheap — pattern set |
| Off-task drift | Response wanders to adjacent topic | Hard — needs semantic check; defer |
| Confidence claim vs. hedging score | Model claims high confidence but text is hedge-heavy | Cheap — derived |

### 3.3 Confidence as a signal

Confidence is consumed three ways:
- **Raw** from the model's self-report (extracted in minimal mode, parsed from JSON in full mode)
- **Adjusted** by response-side signals: format violation → downgrade; hedging detected → downgrade; refusal language → downgrade hard
- **Calibrated** against history (v4 — depends on feedback intake)

The output to the act layer is a single **effective confidence** number plus a structured **assessment** noting which signals fired.

## 4. The act layer: coping families

Three families of coping mechanism. Some we have; some we don't. The act layer routes based on effective confidence and classification level.

### 4.1 Reduce uncertainty before output (have)

What v1 built. Multi-path consensus.

- Vote-N
- Critique playbook
- Plan-then-decompose
- Externalize to tools (when actually invoked)
- Escalate to frontier (delegate — designed, cut in v0; revisit in v3.4)

### 4.2 Communicate uncertainty in the output (don't have)

The biggest gap. Today we optimize for *producing an answer* over *honestly communicating what we know*. The richer output shapes that solve this — `defer`, `question`, `decline` as first-class output categories — require an envelope schema change AND a user UX redesign for "the system can ask you back" / "the system can say I don't know."

**Deferred to v4** as a structural change. v3 surfaces confidence in the output text itself (in-band hedging based on effective confidence) without changing the envelope.

### 4.3 Constrain the problem to where you're competent (have but unused)

- `env.Classification` exists on the envelope; nothing reads it at runtime.
- Effort proportional to stakes: cheap path for low-stakes, expensive (vote-N + critique + judge) for high-stakes. This is v3.0.

### 4.4 Signal → action rule table (v3 scope)

| Effective confidence | Classification | Behavioral signals | Action |
|---|---|---|---|
| ≥ 0.85 | low | none | Ship as-is (single call, cheap) |
| ≥ 0.85 | medium | none | Ship as-is (single call) |
| ≥ 0.85 | high | none | Vote-3 + critique (high-stakes default) |
| 0.5–0.85 | any | format violation | Re-run with stricter prompt |
| 0.5–0.85 | low/medium | hedging detected | Ship with confidence surfaced |
| 0.5–0.85 | high | any | Vote-5 + critique + judge audit |
| < 0.5 | low/medium | any | Ship with explicit uncertainty caveat |
| < 0.5 | high | any | Escalate to frontier (v3.4) OR refuse with explanation |
| any | any | refusal language detected | Refuse with explanation; do not ship the refusal as content |
| any | any | prompt-side overload detected | Warn config; downgrade effective confidence by 0.3 |

This table is the v3 contract. Implementation reads signals + classification + confidence and picks the action. It is deliberately the WHOLE behavioral surface of v3 — adding new actions requires extending this table, not adding ad-hoc paths.

## 5. v2 + v3 wiring (bidirectional) **(LOCK CANDIDATE)**

v2 and v3 are not sequential. They're complementary:

- **v3 feeds v2:** every notice-layer assessment becomes structured data. Per (action, input-pattern, classification), accumulate: which signals fired, what effective confidence we landed on, what coping action we picked. Written to a behavioral-profile partition of the v2 store.
- **v2 feeds v3:** v3's confidence calibration (v4-deferred) reads from v2's accumulated behavioral profiles. "How often have we seen this input shape produce a format violation?" → adjust the confidence floor for that shape.
- **The same per-action substrate** that holds exemplars (v2 original use) holds behavioral profiles (v3 addition). Different partitions of the same store.
- **The same cold-path driver** can mine behavioral profiles for patterns. Future: cold path notices "JSON-envelope inputs to phi4-mini have 60% format-violation rate" and proposes a routing rule.

This wiring is where the measurable change compounds. The bench's per-case output gets richer (behavioral profile per attempt). The sliding window can track confidence calibration drift. The store accumulates a learning corpus that's safe (dispatch-shaped, not content-shaped) and actionable.

## 6. Safety / gating discipline

From the cache-poisoning discussion 2026-05-23: anything that introduces *learned LLM-generated content* into the inference path carries a poisoning risk. Routing/dispatch decisions are safe; injected exemplars / cached answers / distilled recipes are not.

v3 stays on the safe side of this line:
- Signals are extracted from the call, not the LLM's self-report
- Coping actions are dispatch decisions (vote-N, escalate, refuse) not content injection
- Behavioral profiles in v2 store are *metadata* about past calls, not content that gets injected into future prompts

When v4 starts wiring content-shaped learning (recipes, calibrated routing oracle outputs, per-recipient style snippets), the gating discipline applies: multi-path-agreement + frontier-judge + A/B-before-promotion + version-and-rollback. v3 does not need this discipline because it doesn't write LLM content back to inference.

## 7. v3 phases — what ships

Each phase is a separate PR off main, bench-measurable, no dependencies on user feedback or envelope schema changes.

### v3.0 — Classification-driven effort routing
- Runtime reads `env.Classification`; routes to cheap-path or expensive-path based on the §4.4 table's classification column.
- ~200 LoC. No new packages.
- **Acceptance:** bench output shows different cost profiles for low/medium/high-stakes inputs.

### v3.1 — Notice layer (prompt-side signals)
- `pkg/notice` (or similar) package implementing the §3.1 catalog as cheap detectors.
- Pre-call invocation in the runner; result attached to envelope.Trace.
- ~400 LoC + tests.
- **Acceptance:** bench output includes a per-call behavioral assessment (prompt-side signals fired).

### v3.2 — Notice layer (response-side signals + confidence extraction)
- Extends `pkg/notice` with §3.2 detectors.
- Confidence extraction from minimal-output responses (parse `confidence: 0.X` from raw text).
- Effective confidence computed (raw × behavioral signal adjustments).
- ~500 LoC + tests.
- **Acceptance:** bench output includes effective confidence per call + which response-side signals fired.

### v3.3 — Confidence surfaced to user output
- Final orchestrator output includes a confidence + an in-band hedge if effective confidence < threshold.
- Bench captures confidence as a column for analysis.
- Minor envelope amend: confidence becomes a first-class field on `Execute.ReturnResult` (it already is — just used).
- ~150 LoC.
- **Acceptance:** bench shows confidence column; calibration table (confidence band vs. pass rate) becomes a report section.

### v3.4 — Coping rule table dispatcher + escalate-to-frontier
- The §4.4 table becomes runnable code.
- `delegate` mechanism (cut in v0) revived as the "high-stakes + low-confidence" branch.
- Per-case coping decision logged in trace.
- ~300 LoC + a frontier endpoint config (reuses anthropic adapter).
- **Acceptance:** bench output shows per-case coping action chosen; high-stakes cases that fail are correctly escalated.

**Total v3 scope: ~1550 LoC across 5 PRs. Every PR ships independently. Every PR is bench-measurable.**

## 8. v4 deferred

Sections deliberately moved out of v3 to keep the shipping target tight:

- **User feedback intake** (the gating design decision — where signals land)
- **New output categories** (`defer` / `question` / `decline`) — envelope schema change, UX redesign
- **Calibrated confidence per (action, input-pattern, recipient)** — depends on feedback intake
- **Routing oracle** — consequence of calibrated confidence
- **Anti-pattern detector / critique spec from corrections** — depends on feedback
- **Per-recipient style memory** — depends on feedback + an entity-extraction layer
- **Recipe distillation** — content-shaped learning; needs the gating discipline above
- **Vocabulary substitution** — context rewriting; orthogonal but lower priority

## 9. What we keep from v2

- `pkg/exemplar.Store` — substrate, **keep**. v3 writes behavioral profiles to a new partition; v4 writes calibration data.
- `pkg/curation.Run` — cold-path driver, **keep**. Currently mines pass/fail; v4 extends to mine corrections.
- `pkg/adapter/curated` — warm-path exemplar prepend, **keep but rename in mental model** to "experimental measurement tool." Not the production path. Stays available for bench experiments that explicitly want to measure exemplar-injection effects.
- v2 substrate's per-action partitioning is exactly the right shape for v3's behavioral profiles — different rows in the same store.

## 10. Bench harness implications

GPQA Diamond and Phase 1 email drafting both measure single-call generic quality. Neither has persistent context to learn from. Neither can demonstrate the v4 QoL thesis.

**v3 can be measured on the existing bench harness** — the signals fire, the coping actions fire, the per-case behavioral assessment is recordable. The bench output gets richer; no new harness needed for v3.

**v4 will need a new bench shape** — a simulated SMB workflow (consistent fictional company, 100 sequential tasks, accumulated context). That's deferred with the rest of v4.

---

## Open questions

- §2.1 lock — does "general intelligence enhancement" hold up against any commercial case for vertical packaging? If so, name it explicitly here.
- §4.4 confidence thresholds (0.85, 0.5) — these are starting points; needs empirical tuning. Where does that calibration happen if not user feedback?
- §5 — should the behavioral-profile partition be a separate package or a partition of `exemplar.Store`? Probably the latter for v3 simplicity; could split later.
- §7.4 — escalate-to-frontier as the high-stakes-low-confidence default makes cost spiky. SMBs may prefer "refuse with explanation" as the default. This should be a per-customer config knob, not a hard default.
