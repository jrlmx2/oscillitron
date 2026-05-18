<!-- CLAUDE GENERATED -->
# Oscillatron — Library Plan (Phase 1 + Phase 2 Thin Skeleton)

**Status:** Draft v0.2 (substantive revision of v0.1)
**Scope:** Phase 1 (empirical validation) + the minimum Phase 2 surface needed to absorb Phase 1 findings.
**Sources (v0.1):** `inputs/framework-design.md` (v0.2), `inputs/oscillatron-vocabulary.md`.
**Sources (v0.2):** `../CLAUDE.md`, `design-notes.md`, plus the 2026-05-18 architectural lock decisions.
**Date:** 2026-05-18 (v0.2); 2026-05-17 (v0.1).

---

## v0.2 — Major revision (2026-05-18)

This plan was written before the 2026-05-18 architectural decisions captured in `../CLAUDE.md` and `design-notes.md`. The locked changes:

- **Hermes integration: WRAP, not modify or fork (LOCKED).** Oscillitron now sits *above* Hermes instances as an orchestration layer. Hermes owns per-instance skill creation, persistent memory, multi-channel I/O. Oscillitron owns the action-potential bus, the router specialist, the verifier loop, the playbook curation, and cross-instance topology.
- **No fine-tuning of model weights.** Specialization lives entirely in playbooks, retrieval indexes, prompt templates, and routing topology — all cheap, inspectable, and rollback-safe.
- **Graph topology is the main learnable substrate.** Content specialists are the nodes; processing flow (input → reasoning → output, fast vs. slow paths, inhibition) is graph topology, not separate node types.
- **Action potentials carry summaries.** The handoff payload between specialists is the compressed summary the upstream session produced on exit. The "spike" between oscillators is just a concrete summary handoff.

### What survives from v0.1

- The session envelope schema (§5.1). The `Outcome` struct IS the AP summary shape.
- The vocabulary (Action Potential, Synapse, Oscillation, Neural Ensemble). The vocab doc was prophetic — its definitions already match the new architecture.
- Phase 1 as a kill-or-proceed empirical validation gate.
- The 85% quality / 15% cost target.
- Go as the orchestration language.
- The Adapter / Decomposer / Recomposer interface seams as integration points.

### What changes shape

- **The Adapter abstraction** now adapts at a higher level: a Hermes instance, not a raw model endpoint. Hermes still talks to vLLM/SGLang/NIM under the hood; Oscillitron talks to Hermes.
- **Phase 1's workload validation** now tests whether Hermes-wrapped specialists deliver the 85%/15% target. Code analysis remains the recommended workload.
- **Phase 2's orchestrator** owns routing between Hermes instances. The Decomposer seam stays, but its job becomes "which Hermes instance(s) receive this AP" rather than "decompose this prompt into a session list."
- **The Recomposer** is no longer the only synthesis seam. Most synthesis happens via single-specialist exit summaries (the AP itself). The Recomposer is reserved for ensemble fan-out cases where multiple specialists fire on the same AP and their outputs need to be merged.
- **New components introduced.** Router specialist (decides where APs go), inhibitor (circuit-breaker for drifting reasoning chains), verifier loop (grounded checks + thumbs + implicit + periodic audit).

### What this plan does NOT yet address (open for the user to confirm or cut)

- **Compliance / regulated-industry framing** from the prior design doc (§6.8, §10): audit ledger, classification routing, reproducibility manifest, PII detection, evidence export. None of this came up in the 2026-05-18 architecture discussion. It may still be a downstream phase, but the new core thesis is "production-grade LLM at a fraction of the cost" without an explicit regulated-industry overlay.
- **Business model** (OSS core + consulting for regulated industries) — unchanged in the prior plan but unconfirmed in current direction.
- **Quantization-specific concerns** (AWQ Q4 calibration, mixed-precision policies) — now a Hermes-internal concern, not an Oscillitron concern. Oscillitron does not need to care which model Hermes is running.

### Open conflicts to resolve before code lands

- **Naming.** The folder and `CLAUDE.md` use "Oscillitron"; this plan, the input docs, and the vocabulary doc use "Oscillatron." The 2026-05-18 conversation consistently used Oscillitron. Pick one and rename for consistency.
- **Compliance scope.** Does the regulated-industry framing remain a phase target, or has the thesis narrowed? This affects roughly 30% of the prior plan and design doc.
- **Self-improvement loop position.** v0.1 had Phase 7 (Quality + Calibration Tooling) as the latest quality-improvement work. The new architecture puts the verifier loop and playbook curation closer to the core — probably Phase 3 or 4. Reorder when this is confirmed.

---

## 0. Framing

The framework design doc explicitly treats Phase 1 as a kill-or-proceed gate. Until Phase 1 says the cost-quality math works, every line of Phase 2+ code is at risk of being thrown away. So:

- **Phase 1 builds the *minimum* needed to answer "does the architecture deliver 85% quality at 15% cost?"** for one chosen workflow.
- **Phase 2's thin skeleton is the smallest Go surface that (a) reuses Phase 1's investment without rework and (b) gives a future-self a clear seam to slot in Phase 3–6 components.**

What is deliberately *not* in this plan: the decomposition engine UX, audit ledger crypto, MCP server, recomposition graph, gateway-with-retries, calibration tooling, evidence export. Each of those gets its own plan in later phases.

---

## 1. Naming + folder reconciliation

| Surface | Current | Should be |
|---|---|---|
| Project folder | `Oscillitron` | `Oscillatron` (rename when convenient; non-blocking) |
| Vocab doc | `oscillatron-vocabulary.md` | unchanged |
| Framework design doc | "[Framework Name TBD]" | rewrite header to "Oscillatron — Design Document" |
| Go module path | n/a | `github.com/<owner>/oscillatron` (placeholder; GitHub org is a §13.10 open item) |
| Top-level Go package | n/a | `package oscillatron` |
| CLI binary | n/a | `oscillatron` |

§16 of the design doc still lists "Framework name" as undecided — this plan resolves it to **Oscillatron** based on the vocabulary doc's metaphor having already produced a coherent vocabulary around the name. Confirm or push back before code lands.

---

## 2. Phase 1 — Empirical Validation

### 2.1 Goal

Answer one question with publishable evidence: **on workload W, does Oscillatron's externalized-reasoning architecture deliver target quality at target cost vs a frontier baseline?**

Target from design doc §3: 85% quality at 15% cost. The plan does not assume these numbers are achievable — Phase 1 exists precisely to find out.

### 2.2 Workload choice

§16 nominates **code analysis** as the lead candidate ("recommended for publishable evidence"). The plan proceeds on that assumption. If you pick a different workload, only §2.3.5 (the eval harness) and §2.3.6 (the example workflow) change materially; the rest is workload-agnostic.

### 2.3 Phase 1 deliverables

1. **Session Envelope types in Go** — the canonical schema from design doc §6.1, implemented as plain Go structs with JSON tags. Phase 1 writes these as files (one JSON per session); Phase 2 promotes them to channel payloads. Same struct in both phases.
2. **An `Adapter` interface and two implementations.** [v0.2] `hermes.Adapter` wraps a single Hermes instance with a pre-seeded specialization (probably the code specialist for the code-analysis workload). Hermes handles the underlying vLLM/SGLang/NIM call internally; Oscillitron treats Hermes as the opaque substrate. `claude.Adapter` (frontier baseline) is used for the comparison run only. The interface is the contract; both phases share it. Phase 2 expands to multiple Hermes instances behind the router specialist.
3. **A sequential runner** — no goroutines, no fan-out, no concurrency. Loops over sessions, calls the adapter, writes the envelope. This is *deliberately* simpler than Phase 2's Oscillation runner so the measurement doesn't get confounded with concurrency bugs.
4. **A cost tracker** — records actual cost (local: token counts × marginal energy/depreciation if we want it; frontier: provider pricing). Records counterfactual frontier cost for local runs. Outputs `cost_actual_usd`, `cost_frontier_baseline_usd`, ratio.
5. **An eval harness for workload W** — task-specific, intentionally bespoke. For code analysis: a corpus of code-analysis prompts with held-out reference answers, a grader (LLM-as-judge with calibration spot-checks, or rubric scoring). Outputs `quality_score` per session, aggregated per run.
6. **The code-analysis example workflow** — driver code that decomposes a code-analysis prompt manually (no decomposition engine yet — you write the decomposition by hand), dispatches via the sequential runner, recomposes by hand. The *manual* nature is the point: Phase 1 isn't testing the decomposition/recomposition engine, it's testing whether bounded sessions with manual orchestration beat one big frontier call on the cost-quality frontier.
7. **A comparison runner** — runs the same workload through (a) local quantized Qwen3 via Oscillatron, (b) frontier Claude as a single call, (c) optionally frontier Claude with the same manual decomposition. Produces a results table.

### 2.4 Phase 1 → Phase 2 contract

Phase 1 commits to two things Phase 2 cannot break:

- **The Session Envelope schema is stable.** Phase 2 may add fields; it must not rename or restructure existing ones. Phase 1's recorded sessions must be replayable in Phase 2.
- **The `Adapter` interface is stable.** Phase 2's Oscillation runner consumes the same interface.

Phase 1 does not commit to: the sequential runner's shape (Phase 2 replaces it), the file-per-session writer's location/format (Phase 2 keeps the JSON envelope but may move where it lives), the eval harness internals (workload-specific).

### 2.5 Decision gate criteria

Spell out the kill/proceed criteria *before* running Phase 1, not after. Suggested:

| Metric | Proceed if | Kill if |
|---|---|---|
| Quality (Oscillatron vs frontier-single-call) | ≥ 80% | < 65% |
| Cost ratio | ≤ 25% of frontier | > 50% of frontier |
| Both above | both met → proceed | either failed badly → kill |
| Mixed | one met, one in middle zone → diagnose before deciding |

These numbers are derived from §3 goal of 85%/15%; relaxed slightly because Phase 1 is manual orchestration without the eventual quality-preservation tooling (calibration, recomposition prompts, etc.) — if manual hits 80%/25%, the tooling should close the gap.

---

## 3. Phase 2 — Thin Skeleton

### 3.1 Goal

A Go orchestrator that runs the same workload Phase 1 ran, but with: real concurrency (fan-out / fan-in), real backend adapter contracts, real operational traces, and an entry point for Phase 3's decomposition engine. **Nothing more.**

Phase 2 explicitly does not include: classification-aware routing (Phase 4), audit ledger (Phase 4), MCP (Phase 6), recomposition graph (Phase 5), decomposition engine (Phase 3).

### 3.2 Phase 2 deliverables

1. **`session` package** — promoted from Phase 1; adds `parent_session`, `feeds_into`, basic provenance fields. Classification field present but unused by routing (Phase 2 has one backend). [v0.2] The `Outcome` struct is the canonical AP-summary shape; downstream sessions ingest it as input.
2. **`orchestrator` package** — the Oscillation runner: take a slice of sessions, dispatch concurrently, gather results, return ordered envelopes. Uses goroutines + a worker pool. Bounded concurrency configurable. [v0.2] Enforces the session-budget threshold (~70% of context window) at exit: a specialist either finishes within budget or produces a "where I got, what remains" summary at threshold.
3. **`adapter` package** — `Adapter` interface unchanged from Phase 1. [v0.2] `hermes.Adapter` only; frontier `claude.Adapter` is retained for the comparison harness, not for production routing.
4. **`router` package** [v0.2 NEW] — the router specialist. Given an AP, decides which Hermes instance(s) receive it. Phase 2 implementation is a small cheap classifier (rule-based or single-model lookup); a learned router arrives with the self-improvement loop in a later phase. Owned by Oscillitron, not Hermes — this is where most of the system's intelligence lives.
5. **`inhibitor` package** [v0.2 NEW] — the circuit-breaker. Watches a reasoning chain for drift signals (grounded check failures, contradiction with earlier summaries, confidence drop, repetition, parallel-specialist disagreement) and fires an inhibition AP that aborts or restarts the chain. Phase 2 implementation can be a stub that only enforces the hard max-iteration cap; full drift detection grows over time. Open in `design-notes.md`: inhibitor as a dedicated node vs. as a property attached to every chain edge.
6. **`trace` package** — minimal structured tracing. Slog-based. Span per session, span per oscillation. *Not* Langfuse yet — Langfuse arrives when operational observability becomes its own concern in Phase 6+.
7. **`cost` package** — promoted from Phase 1.
8. **CLI entry point `cmd/oscillatron`** — loads a workflow definition (JSON or YAML), runs it, writes envelopes. Placeholder; expands in later phases.

### 3.3 The seams that matter

These are the integration points later phases need. Get them right in Phase 2 and Phase 3–6 slot in cleanly:

- **`Decomposer` interface.** Phase 2 has one implementation: `manual.Decomposer` (reads a pre-written workflow file, hands the orchestrator a slice of sessions). Phase 3 replaces it with `llm.Decomposer` (calls a Hermes instance with a decomposition scaffold). The orchestrator never knows which it's using. [v0.2] In the wrap-Hermes architecture, the Decomposer's job overlaps with the router's. Likely resolution: Decomposer breaks a user prompt into a *plan* (a sequence of APs to fire); the Router decides which Hermes instance handles each AP. Different concerns, same compositional flow.
- **`Recomposer` interface.** Phase 2 has one implementation: `concat.Recomposer` (concatenates outcomes in order). Phase 5 replaces it with `tree.Recomposer` (pairwise merge). Same contract. [v0.2] Most synthesis no longer routes through this — a single specialist's exit summary IS the answer in the common case. The Recomposer is reserved for ensemble fan-out (same AP to multiple specialists with consensus or merge).
- **`Router` interface** [v0.2 NEW]. The router specialist's contract: take an AP, return an ordered list of `(instance_id, threshold)` pairs. Multiple destinations means parallel ensemble; single destination is the cheap fast path. Phase 2 implementation is rule-based or single-model lookup.
- **`Inhibitor` interface** [v0.2 NEW]. Take a chain of APs, return either `Continue`, `Restart(from_checkpoint)`, or `Abort`. Phase 2 implementation enforces the hard cap and the most basic drift signals (grounded check failure, confidence threshold); learned drift detection grows over time.

These seams are why Phase 2 is a *skeleton* and not a *Phase 3 implementation*. The interfaces exist; the implementations are placeholders.

---

## 4. Repo layout

Standard Go layout. Single module to start; promote to multi-module only if Phase 6+ compliance components need independent versioning.

```
oscillatron/
├── go.mod
├── go.sum
├── README.md
├── LICENSE                    # Apache 2.0 per §11.1 leading recommendation
├── cmd/
│   ├── phase1/                # Phase 1 validation runner (temporary; removed after gate)
│   │   └── main.go
│   └── oscillatron/           # Phase 2+ CLI entry point
│       └── main.go
├── pkg/                       # Public API surface
│   ├── session/               # Session Envelope types (the Synapse contract)
│   ├── classification/        # Classification enum + propagation rules
│   ├── adapter/               # Adapter interface
│   │   ├── vllm/              # vLLM adapter (Phase 1 + Phase 2)
│   │   └── claude/            # Frontier adapter (Phase 1 only; revisits Phase 4)
│   ├── orchestrator/          # Oscillation runner (Phase 2)
│   ├── decomposer/            # Decomposer interface + manual.Decomposer (Phase 2)
│   ├── recomposer/            # Recomposer interface + concat.Recomposer (Phase 2)
│   ├── cost/                  # Cost tracking + frontier baseline comparison
│   ├── trace/                 # Operational traces (slog-based, Phase 2)
│   └── eval/                  # Quality eval harness (Phase 1; reusable in regression suite later)
├── internal/                  # Implementation details not part of the public API
│   └── jsonl/                 # JSONL writer for envelope persistence
├── examples/
│   └── code-analysis/         # The Phase 1 workflow as a worked, runnable example
│       ├── workflow.yaml
│       ├── prompts/
│       └── corpus/
├── docs/
│   ├── adr/                   # Architecture Decision Records
│   ├── design.md              # Symlink or copy from inputs/framework-design.md once stable
│   └── vocabulary.md          # Symlink or copy from inputs/oscillatron-vocabulary.md
└── scripts/
    └── run-phase1.sh
```

**Notes**
- `cmd/phase1` is intentionally throwaway. After the kill/proceed gate, it either gets deleted (kill) or absorbed into the example workflow + CI regression harness (proceed).
- `pkg/` vs `internal/` split: anything a future consultee or contributor might import is in `pkg/`. Implementation details with no stability promise live in `internal/`.
- No `api/` directory until there's a gRPC/REST surface to host. The CLI is the only entry point through Phase 2.

---

## 5. Core type sketches

These are signatures, not full implementations — meant to pressure-test the shapes. Final field naming should match design doc §6.1 exactly.

### 5.1 `pkg/session`

```go
package session

type ID string

type Type string
const (
    TypeDecompose  Type = "decompose"
    TypeAnalyze    Type = "analyze"
    TypeMerge      Type = "merge"
    TypeSynthesize Type = "synthesize"
)

type Envelope struct {
    ID             ID                          `json:"session_id"`
    Type           Type                        `json:"session_type"`
    Objective      string                      `json:"objective"`
    Classification classification.Level        `json:"classification"`
    Notes          Notes                       `json:"notes"`
    Input          Input                       `json:"input"`
    Outcome        *Outcome                    `json:"outcome,omitempty"`
    Routing        Routing                     `json:"routing"`
    Trace          Trace                       `json:"trace"`
    Audit          *Audit                      `json:"audit,omitempty"` // nil through Phase 3; populated Phase 4
}

type Notes struct {
    Constraints  []string `json:"constraints"`
    PriorSignals []string `json:"prior_signals"`
    ContextTags  []string `json:"context_tags"`
}

type Input struct {
    Type        string `json:"type"`
    Content     string `json:"content"`
    ContentHash string `json:"content_hash"` // sha256:hex
}

type Outcome struct {
    Verdict        string   `json:"verdict"`
    Signals        []string `json:"signals"`
    Confidence     float64  `json:"confidence"`
    OpenQuestions  []string `json:"open_questions"`
    Contradictions []string `json:"contradictions"`
    FeedsInto      []ID     `json:"feeds_into"`
}

type Routing struct {
    Model                   string `json:"model"`
    ModelHash               string `json:"model_hash"`
    Reason                  string `json:"reason"`
    ClassificationConstraint string `json:"classification_constraint"`
}

type Trace struct {
    TokensInput              int     `json:"tokens_input"`
    TokensOutput             int     `json:"tokens_output"`
    DurationMs               int64   `json:"duration_ms"`
    ParentSession            *ID     `json:"parent_session,omitempty"`
    CostUSD                  float64 `json:"cost_usd"`
    CostVsFrontierBaselineUSD float64 `json:"cost_vs_frontier_baseline_usd"`
}

type Audit struct {
    LedgerID  string `json:"ledger_id"`
    SignedAt  string `json:"signed_at"`
    Signature string `json:"signature"`
}
```

### 5.2 `pkg/classification`

```go
package classification

type Level string
const (
    Public       Level = "public"
    Internal     Level = "internal"
    Confidential Level = "confidential"
    Regulated    Level = "regulated"
)

// Propagate returns the more-restrictive of two levels.
func Propagate(a, b Level) Level { /* ... */ }
```

### 5.3 `pkg/adapter`

```go
package adapter

type Request struct {
    Model       string
    Prompt      string
    MaxTokens   int
    Temperature float64
    // ... minimum shared surface
}

type Response struct {
    Text         string
    TokensInput  int
    TokensOutput int
    DurationMs   int64
    CostUSD      float64
    ModelHash    string
}

type Adapter interface {
    Name() string
    Call(ctx context.Context, req Request) (Response, error)
}
```

### 5.4 `pkg/orchestrator`

```go
package orchestrator

type Oscillation struct {
    Sessions    []*session.Envelope
    Concurrency int
}

type Runner struct {
    Adapter adapter.Adapter
    Tracer  trace.Tracer
    Cost    *cost.Tracker
}

// Run dispatches the sessions concurrently and returns updated envelopes
// in input order, with Outcome populated.
func (r *Runner) Run(ctx context.Context, osc Oscillation) ([]*session.Envelope, error) {
    /* fan-out / fan-in with bounded concurrency */
}
```

### 5.5 `pkg/decomposer` + `pkg/recomposer`

```go
package decomposer

type Decomposer interface {
    Decompose(ctx context.Context, prompt string) ([]*session.Envelope, error)
}

// manual.Decomposer reads a workflow file and returns pre-defined sessions.
// llm.Decomposer (Phase 3) calls a model with a decomposition scaffold.
```

```go
package recomposer

type Recomposer interface {
    Recompose(ctx context.Context, outcomes []*session.Envelope) (*session.Envelope, error)
}

// concat.Recomposer concatenates outcomes in order.
// tree.Recomposer (Phase 5) pairwise-merges with conflict resolution.
```

---

## 6. Explicitly deferred (and the seams they'll plug into)

| Component | Phase | Seam it slots into |
|---|---|---|
| LLM-driven decomposition engine | 3 | `decomposer.Decomposer` interface |
| Notes enrichment between sessions | 3 | Hook in `orchestrator.Runner` between dispatches |
| Multi-model routing (local vs frontier) | 4 | New `router` package wrapping `adapter.Adapter` |
| Gateway (LiteLLM-style or custom Go) | 4 | Sits between orchestrator and adapters |
| Audit ledger (append-only, signed) | 4 | Hook in orchestrator before/after each `Adapter.Call` |
| Reproducibility manifest | 4 | New `manifest` package; collects state from all components |
| Tree-merge recomposition | 5 | `recomposer.Recomposer` interface |
| Confirmation gate UX (CLI) | 5 (or earlier) | Wraps the decomposer output before orchestrator dispatch |
| MCP server | 6 | New `mcp` package; exposes orchestrator state |
| PII detection | 6 | Hook in adapter pre-call |
| Approval gates (general primitives) | 6 | Hook at orchestrator state transitions |
| Evidence export | 6 | Reads ledger + manifest |
| Workload-specific AWQ calibration | 7 | Standalone Python tooling; produces model weights consumed by vLLM |
| Langfuse integration | 7 | `trace` package gets a Langfuse backend alongside slog |

---

## 7. Open questions to resolve before code lands

### Resolved in v0.2

- ~~Framework substrate~~ — Hermes-wrap, LOCKED 2026-05-18.
- ~~Whether to fine-tune~~ — No weight updates, LOCKED 2026-05-18.

### Still open (v0.1 carryovers)

1. **GitHub owner / org** — `github.com/<owner>/oscillitron` (or oscillatron — see naming below). Personal account or new org? (§13.10 of framework-design.md)
2. **License confirmation** — Apache 2.0 leading per §11.1 of framework-design.md; confirm before adding LICENSE.
3. **Workflow definition format** — YAML vs JSON for the manual decomposer's input. YAML reads better for humans; JSON aligns with the envelope schema. Recommendation: YAML in `examples/`, JSON for the envelope on the wire.
4. **Eval grader** — LLM-as-judge or rubric scoring or both? Calibration approach for judge prompts. [v0.2] This needs to interoperate with the verifier loop (grounded + thumbs + implicit + audit) — Phase 1's eval grader is essentially the verifier in miniature.
5. **Cost model** — pure token throughput vs. amortized hardware depreciation + energy. Recommendation: track tokens primarily; treat hardware cost as a separately reported denominator. [v0.2] In the wrap-Hermes architecture, also need to account for the cost of running multiple Hermes instances vs. one.
6. **Where do the design docs live in the code repo?** Symlink from `inputs/` into `docs/`, copy with a freshness check, or just link in README. Recommendation: README link to the canonical version in this knowledge-work project; do not duplicate.

### New in v0.2

7. **Naming conflict.** Folder + `CLAUDE.md` say "Oscillitron"; this plan + input docs say "Oscillatron." Pick one before any code uses the name. Recommendation: confirm "Oscillitron" since that's what the 2026-05-18 architectural conversation consistently used, and rename the input docs and this plan's references on next pass.
8. **Compliance scope.** Does the regulated-industry framing from the prior design doc (audit ledger, classification routing, reproducibility manifest, PII detection, evidence export) remain in scope as a downstream phase, or is the thesis now strictly "production-grade LLM at a fraction of cost" without that overlay? Affects ~30% of the prior plan and the business model.
9. **Seed specialization list.** Per `CLAUDE.md`, ~3–6 content nodes to start: candidates are code, math/structured reasoning, retrieval/research, writing. Phase 1 uses just one (the code specialist for code-analysis workload). Need to confirm the full Phase 2 seed list.
10. **AP / summary shape.** Structured fields (state, attempts, remaining, confidence) vs. freeform prose vs. structured envelope wrapping freeform body. Decision blocks Phase 1 if the envelope structure changes. Working hypothesis from `design-notes.md`: hybrid.
11. **Inhibitor: node or edge property?** Affects the `inhibitor` package shape in Phase 2. Open in `design-notes.md`.
12. **Session-budget threshold value.** Fixed at 70% of context window, or per-specialist / per-model? Affects `orchestrator` package configuration.
13. **Router design.** Phase 2 starts with a rule-based or small-classifier router. The contract is fixed (`Router` interface); the implementation grows over time. Worth deciding now whether the initial classifier is its own model call or just a Go-side decision tree.
14. **Hermes instance lifecycle.** Long-lived (one Hermes instance per specialist seed, persistent) vs. ephemeral (spin up per task)? Long-lived matches Hermes' "the agent that grows with you" design and lets each instance accumulate playbooks; ephemeral is cheaper for bursty workloads. Probably long-lived, but confirm.

---

## 8. Where the code physically lives

The Oscillatron knowledge-work project (this folder) holds **design, research, and consulting artifacts** — not source code. Two reasonable arrangements for the Go module:

**Option A (recommended): separate sibling repo.**
- Knowledge-work project: `/Users/james/Documents/Claude/Projects/Oscillitron/` (or renamed `Oscillatron/`).
- Go module repo: `/Users/james/Documents/code/oscillatron/` (or wherever you keep code), published to `github.com/<owner>/oscillatron`.
- Connection: README in each links to the other; INDEX.md in this project includes a top-level pointer.

**Option B: code as a subdirectory of this project.**
- `/Users/james/Documents/Claude/Projects/Oscillitron/code/` contains the Go module.
- Simpler discoverability for solo work; awkward once the code repo has its own contributors and CI.

Recommend A. The OSS framework and the knowledge-work project have different audiences (contributors vs Jim) and different lifecycles (versioned releases vs perpetual research notebook).

---

## 9. Suggested first-week tasks

In execution order, no commitment on calendar pace. [v0.2] Steps 4–5 rewritten for the wrap-Hermes architecture; the surrounding steps are essentially unchanged.

1. Confirm the name (Oscillitron vs. Oscillatron), license, GitHub owner. (5 minutes once you decide; blocks everything else.)
2. Create the Go module repo with `go.mod`, README, LICENSE, `cmd/`, `pkg/` skeleton dirs. No code yet.
3. Implement `pkg/session` types from the schema. JSON round-trip tests.
4. [v0.2] Stand up one Hermes instance locally per `github.com/NousResearch/hermes-agent`. Pre-seed it with a code-analysis specialization (skills, retrieval index, prompt scaffolds). This is the Phase 1 specialist.
5. [v0.2] Implement `pkg/adapter` interface + `hermes.Adapter`. The adapter sends an AP envelope to the Hermes instance and receives a structured `Outcome` back. Smoke-test against the running Hermes instance.
6. Implement `pkg/adapter/claude.Adapter` for the frontier baseline (used only in the comparison harness).
7. Implement `pkg/cost` tracker; verify it produces sensible actual + counterfactual numbers.
8. Pick the Phase 1 workload (code analysis assumed). Build a small corpus (10–20 prompts).
9. Hand-write the manual decomposition for the workload — this is the artifact you'll learn the most from.
10. Implement `pkg/eval` with the chosen grader. Calibrate the grader on a held-out set.
11. Run the comparison: local-via-Hermes-instance vs frontier-single-call vs frontier-with-same-decomposition. Tabulate. Read the decision gate criteria honestly.

If step 11's numbers don't clear the gate, the design doc's own §15 says kill or pivot to consulting-on-existing-frameworks. That's the entire purpose of doing Phase 1 before Phase 2.

---

## 10. What's missing from this plan and why

- **Concurrency strategy details for the Oscillation runner.** Worker pool sizing, error-aggregation semantics, partial-failure policy. Deferred to a Phase 2 design note — these need their own ADR.
- **Adapter retry/timeout policy.** Phase 1 can be naive (single attempt, hard timeout); Phase 2 needs a real policy that doesn't bleed into the gateway's job.
- **The notes enrichment function.** §6.4 of the design doc. Skipped because Phase 2 has no enrichment (manual workflow files specify notes directly); arrives with Phase 3.
- **Anything compliance-shaped.** Audit ledger, manifest, classification routing, PII detection, evidence export. The classification field is in the schema but inert through Phase 3. This is deliberate scope discipline per §1.3 of the design doc.

---

*End of plan v0.1. Open for revision. Tracked in INDEX.md under References.*
