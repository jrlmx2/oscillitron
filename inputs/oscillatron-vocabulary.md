# Oscillatron — Naming Vocabulary

**Project:** Oscillatron
**Purpose:** Codified vocabulary for the compound AI / multi-session orchestration architecture. Drop into project root as `VOCABULARY.md` or fold into the design doc.

---

## Core Metaphor

Oscillatron orchestrates coordinated signals into thought. The architecture borrows its vocabulary from neuroscience because the structure genuinely matches: discrete signals (sessions) combine into rhythmic layers (coordinated calls), which combine into emergent coherence (the final reasoning output).

---

## The Four-Level Hierarchy

```
Neural Ensemble                 ← full session graph for a user task
    │
    ├── Oscillation             ← a coordinated layer of sessions
    │       │
    │       ├── Action Potential    ← a single bounded session
    │       ├── Action Potential
    │       └── Action Potential
    │              │
    │              └── Synapse  ← structured handoff to next session
    │
    └── Oscillation
```

---

## Definitions

### Action Potential
A single bounded model call. One scaffold, one context window, one structured output.

- Discrete, fires once, carries a specific signal.
- Quality-preservation discipline lives here (bounded inputs, schema'd outputs, calibration against quantization drift).
- Failure mode: a noisy or incoherent spike. Caught at this layer, not propagated.

**Examples:** a single file-session over one source file; a single decomposition call; a single synthesis call.

### Synapse
The structured handoff between action potentials.

- Carries the schema'd output of one session as input to the next.
- The user confirmation gate is a special-case synapse — a mandatory quality boundary at the decomposition seam.
- Failure mode: schema mismatch, dropped context, lossy transformation.

**Examples:** the JSON envelope passed from a file-session to synthesis; the user-approved decomposition plan handed back to the orchestrator.

### Oscillation
A coordinated layer of action potentials firing in concert.

- Has a cadence: parallel dispatch, gather, aggregate.
- Layers aren't just batches — they have internal structure (fan-out width, gather strategy, retry policy).
- Failure mode: the layer doesn't converge — too many noisy action potentials, or the aggregation can't reconcile them.

**Examples:** the decomposition layer fanning out N parallel file-sessions; the synthesis layer aggregating bounded outputs into a single reasoning trace.

### Neural Ensemble
The full session graph for one user task.

- The unit of observability — what gets traced end-to-end in Langfuse.
- What the user confirmation gate sits inside of.
- The unit of emergent coherence — produces something no single action potential could.

**Examples:** "the ensemble that answered the user's question about X"; "the ensemble that produced the migration analysis."

---

## Mapping to Architecture

| Vocabulary       | Implementation                                                  |
|------------------|-----------------------------------------------------------------|
| Action Potential | Single vLLM or Claude API call with scaffold + structured output |
| Synapse          | Schema'd envelope between calls; user confirmation gate          |
| Oscillation      | Go orchestrator layer — parallel dispatch + gather              |
| Neural Ensemble  | Full session graph for one task; root span in Langfuse trace    |

---

## Why This Vocabulary Pays Off

1. **Shared team language.** "Which action potential failed?" is more precise than "which LLM call failed?" — it implies the bounded contract.
2. **Observability mirrors the metaphor.** Langfuse traces are hierarchical: ensemble → oscillations → action potentials.
3. **Design opinions encoded in names.** "Action potential" implies *bounded, discrete, all-or-nothing* — the exact discipline needed around quantized Qwen3 calls. "Oscillation" implies *rhythm and coordination*. "Ensemble" implies *emergent coherence*.
4. **Naming the edges (Synapse) names the contract.** Most frameworks hide the handoff; this one elevates it.

---

## Open Naming Questions

- **Routing decision.** What LiteLLM does when it picks Qwen3 vs Claude. Closest neural analog is **neuromodulation** (the chemical context that shifts which circuits fire). May be too cute — flagged for review.
- **Calibration / quality-check spikes.** The sessions whose only job is to validate other sessions' outputs. Possible name: **inhibitory action potentials** (in neuroscience, these suppress firing rather than propagate). Worth considering if the architecture needs a quality-gating session class distinct from work-doing sessions.
- **Persistent state across ensembles.** Memory that survives between tasks. Possible name: **long-term potentiation (LTP)** — the mechanism behind learning. Probably overkill unless the system actually adapts.

---

## Quick Reference Card

| Term              | One-liner                                                |
|-------------------|----------------------------------------------------------|
| Action Potential  | One bounded session — discrete, schema'd, all-or-nothing |
| Synapse           | The structured handoff between sessions                  |
| Oscillation       | A coordinated layer of sessions                          |
| Neural Ensemble   | The full session graph for one user task                 |
