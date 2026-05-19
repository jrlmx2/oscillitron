<!-- CLAUDE GENERATED -->
# Oscillitron

## Provenance rule (project-wide, enforced)

Every document Claude authors in this project carries the literal marker `CLAUDE GENERATED` at the top, in the comment syntax appropriate for the format (`<!-- CLAUDE GENERATED -->` for markdown/HTML, `# CLAUDE GENERATED` for Python/shell/YAML, `// CLAUDE GENERATED` for JS/TS/C-family, `/* CLAUDE GENERATED */` for CSS, `-- CLAUDE GENERATED` for SQL; for office formats use the document Author/Creator metadata; for strict-schema formats like JSON/CSV use a sibling `.claude-generated` manifest). Documents without this marker are treated as user-authored. Claude does not add the marker when merely editing a user-authored file — only when Claude is the original author or has substantially rewritten it. This rule resolves authorship ambiguity for restructuring, classification, and any provenance judgment; check for the marker first before falling back to heuristics.

## Folder conventions

- `inputs/` — human-curated source material. The user puts files here; Claude never modifies, renames, moves, or deletes anything inside. Classification ignores this folder entirely.
- `references/` — Claude's organized knowledge for this project. Prefer many small, focused files over few large ones. Indexed from the root `INDEX.md`.
- `artifacts/` — finished, shareable outputs. What the user takes away.
- `scratch/` — staged work: drafts, intermediate files, exploration. Safe to clear.
- `skills/` — reusable skill definitions developed in this project.

`INDEX.md` (root) — single entry point. One line per loadable resource (reference, skill, key artifact) with a hook describing when to load it.

## Default behaviors

- Consult `INDEX.md` before scanning the tree; load only what's needed.
- Deliverables → `artifacts/`. Drafts and intermediates → `scratch/`.
- Durable project conventions belong in this file. One-off facts go in memory, not here.
- Never write into `inputs/`. If a file looks like input material, surface it as a candidate for the user to move themselves.

---

<!--
Project memory for Claude. Fill in the sections below as the project takes shape.
Keep it concise — link out to deeper docs rather than inlining them.
-->

## What this is

<!-- One- or two-sentence description: language/stack and what the project does. -->
Production-grade LLM handling at a fraction of the cost. A neural-ensemble runtime where weak/cheap base models are wrapped as "oscillators" coordinated through "action potentials" (spike-like events), with specialization growing organically over time inside scaffolded seed niches. No fine-tuning of model weights — specialization lives entirely in playbooks, retrieval indexes, prompt templates, and routing topology, all of which are cheap, inspectable, and rollback-safe.

## Status

- **Stage:** scaffolding — code subproject seeded with the AP/router skeleton; no Hermes integration yet.
- **Owner:** Jim (jrlmx2@gmail.com)
- **Created:** 2026-05-17
- **Code subproject:** [`oscillitron/`](oscillitron/) — Go runtime. See [`oscillitron/CLAUDE.md`](oscillitron/CLAUDE.md) for code-mode conventions and current implementation status.

## Stack & tooling

- **Language: Go (LOCKED 2026-05-18).** Rationale: goroutines + channels map cleanly onto N oscillators passing action potentials; static binary makes ops simple; the `anthropic-skills:go-agent-company` skill aligns with the multi-agent message-envelope pattern this project needs.
- **Package manager:** Go modules (stdlib only for v0; external deps introduced one at a time with justification).
- **Min Go version:** 1.21 (uses `log/slog`).
- **Code location:** `oscillitron/` subdirectory (library-plan §8 Option B). If this is later promoted to a sibling repo per library-plan §8 Option A, the cross-link to `oscillitron/CLAUDE.md` becomes a README pointer to the new repo.

## Architecture

**Hermes integration: WRAP, not modify or fork (LOCKED 2026-05-18).**

Oscillitron sits *above* Hermes (github.com/NousResearch/hermes-agent) as an orchestration layer that runs N pre-seeded Hermes instances, each acting as a specialist oscillator. Hermes source is never modified. Rationale: preserve upstream feature flow from a fast-moving project (~99K GitHub stars in 8 weeks), isolate specialization drift between instances, and keep ops simple — a bad instance can be killed without a global rollback.

What lives in Oscillitron (the wrapper) vs. Hermes (the substrate):

- **Wrapper owns:** the action-potential bus between instances, the router specialist that decides which instance receives an AP, the verifier loop (grounded checks + thumbs + implicit signals + periodic audits), the playbook curation/garbage-collection layer, and cross-instance topology updates (which oscillator listens to which).
- **Hermes owns:** per-instance skill creation and growth, persistent memory, multi-channel I/O (Slack/Telegram/Discord/voice/etc.), per-peer agent isolation. Used as-is.

Specialization seeds are **predetermined** (the scaffolding) and **grow organically within their niche** (the plasticity). Brain analogy: anatomical priors plus cortical plasticity, not pure emergence.

**Specialists are nodes; processing flow is graph topology.** The router, and optionally a cheap intent classifier upstream of it, are also nodes. Everything else — input → reasoning → output sequencing, fast vs. slow paths — is a property of the graph (edges, weights, thresholds, path length, playbook richness), not separate node types. **The graph is the main learnable substrate.** Specialists grow within their niches via Hermes' skill creation; the bulk of self-improvement happens at the topology layer — strengthening edges, shifting thresholds, adding or pruning routes. Brain analog: cortical microcircuits are nearly identical across regions; specialization comes from what feeds in, not from a different circuit.

**Specialists are brain-function roles, NOT subject domains (LOCKED 2026-05-18).** Seed nodes are typed by cognitive function — perception/parsing, retrieval, planning/decomposition, reasoning/transformation, critic/verification, composition/output — analogous to functional cortical roles (sensory cortex, hippocampus, PFC, ACC, Broca). They are NOT typed by subject ("code specialist", "math specialist", "legal specialist"). Subject competence is an *emergent* property of which playbooks, exemplars, and retrieval shards a brain-function node accumulates over time, plus the topology that routes subject-shaped inputs toward it. Rationale: cortical microcircuits are uniform; specialization comes from afferents and learned weights, not from a different circuit per topic. Subject-based seeding pre-commits the system to a taxonomy we don't yet trust and duplicates effort across topics that share cognitive structure. **Enforcement:** if a draft, demo, doc, or chat suggestion reintroduces subject-based seed names (code/math/legal/writer/fact-check/etc.), rename to the brain-function role it actually plays. The demo in `oscillitron/cmd/oscillitron` uses `reasoner → critic → composer` as canonical placeholders.

**Inhibitor is an edge property, not a node (LOCKED 2026-05-18).** Drift detection and circuit-breaking attach to graph edges (and to chains via aggregate edge state), not to a dedicated inhibitor specialist. Rationale: inhibition is a *modulation* of signal flow between regions, not a separate cortical region; node-shaped inhibition would have to re-receive and re-judge everything the edge already carries. The composite inhibitor in `pkg/inhibitor` is the implementation surface; the runner invokes `Inhibitor.Check(Edge)` at each parent→child edge after the child resolves. Root has no incoming edge and is never checked. Restart-depth and hard-cap semantics are unchanged. Path-stateful detectors (confidence drop windows, cumulative contradictions, repetition) read `Edge.Path`.

**Per-instance vs. shared resources follow the brain (LOCKED 2026-05-18).**

- **Base model weights — shared (like cortical microcircuitry).** All Hermes instances point at the same underlying weak/cheap base model(s). Cortical microcircuits are nearly identical across regions; specialization isn't in the substrate.
- **Playbooks and prompt templates — per-instance (like region-specific learned skills).** Each brain-function node accumulates its own. This is where specialization actually lives.
- **Retrieval — per-instance episodic, shared semantic (like hippocampal vs. semantic memory).** Each node owns its episodic store (its own past sessions, its own exemplars). A small shared semantic pool — stable general knowledge, cross-node facts, agreed terminology — is readable by all. Writes to the shared pool are gated by the curation layer; nodes don't pollute the commons unilaterally.
- **Working/session memory — per-instance, ephemeral (like local working memory in PFC).** Lives only for the session's lifetime; the AP summary is what survives the handoff.
- **Verifier signal and topology state — shared (like global neuromodulation).** Feedback that shapes edge weights, firing thresholds, and curation decisions is system-wide so the graph can learn coherently.

**Action potentials carry summaries.** The handoff payload between specialists is the compressed summary the upstream session produced on exit. The "spike" is just a concrete summary handoff sized to be cheap for the downstream specialist to ingest. See `scratch/design-notes.md` for the working session-lifecycle, summary, and inhibition design.

**Runtime structure is a call tree of AP invocations, not a routed peer graph (LOCKED 2026-05-18).** A complex problem is solved by recursively invoking brain functions. The "graph" we care about is the call tree that emerges for *this specific problem* and dissolves when it returns. It is not a fixed routed network with persistent edges. Brain analog: writing an essay isn't one cortical region routing to another along fixed wires — it's a recursive cascade (retrieve → plan → generate → critique → revise), each step itself a smaller cascade. The structure exists for the duration of the thought.

**APs are invocations, siloed to one brain function each (LOCKED 2026-05-18).** An AP is not a handoff payload between peers — it is a *call* to a specific brain function on a specific input. Each AP invokes exactly one brain function. Complex work happens by the invoked function emitting sub-APs (further invocations of other brain functions on sub-problems), recursively, until leaves return concrete results that recompose back up the tree. The siloed-to-one-function rule keeps each invocation single-purpose and the call tree readable.

**Specialist vs. invocation (LOCKED 2026-05-18).** Two distinct things, easy to conflate:

- **Specialist** = the brain-function type. One per type. Owns the *persistent* memory substrate for that function — exemplars, recipes, retrieval shards, accumulated playbooks. This is the per-instance side of the brain-mirrored resource-sharing model above. Long-lived; accumulates good knowledge over time via the curation layer.
- **Invocation** = one AP being processed. Gets a fresh ephemeral Hermes process seeded from the specialist's persistent store at start, contributes back through curation on exit. Session-bounded, clean isolation, no cross-invocation contamination at the working-memory level.

Brain analog: working memory is per-task and dissolves; long-term memory in the cortical region accumulates across tasks via consolidation. Same pattern, same separation.

**Sub-AP emission is synchronous in v0 (LOCKED 2026-05-18; configurable later).** When an invocation emits sub-APs, the parent blocks on the whole subtree before recomposing and returning. Recursive function-call semantics. Async sub-AP emission (parent returns; sub-APs continue independently) is a real axis but stays deferred — it requires inhibition that can reason across in-flight asynchronous subtrees, which is its own design problem. v0 is fully synchronous, single-threaded sibling dispatch.

**Hardware-level parallelism (multi-GPU, inference-server sharing, sibling-concurrent dispatch) is deferred past v0.** Not designed around. The dispatcher interface should still return a future-shaped result rather than a direct value — cheap insurance so this can be reintroduced without an interface break — but no concurrency, queueing, or backpressure logic ships in v0.

## Self-improvement loop

No weight updates. Specialization substrate is layered:

- Playbooks — exemplar libraries and recipes a specialist applies.
- Retrieval indexes — growing per-instance stores of past good outputs, past failures, domain context.
- Routing topology — graph edges between oscillators, including firing thresholds.
- Prompt templates — DSPy-style compiled prompts that improve from verifier signal.

Verifier is layered, not single-source: grounded checks (code ran, math verified, retrieval matched) form the floor; thumbs and implicit engagement signals cover taste-and-helpfulness; periodic strong-model audit on a sample is the tripwire against sycophancy drift. Implicit signals (kept reading, copied output, asked follow-up) are expected to carry more weight than explicit thumbs because they're denser.

Artifact rot is a first-class concern. The curation layer prunes underused playbooks, merges redundant ones, flags contradictions, and tracks which exemplars are still earning their keep. Playbook store is treated as a living artifact, never append-only.

## Layout

- `inputs/` — user-curated source material (framework design, vocabulary). Never modified by Claude.
- `scratch/` — working notes (design-notes, library-plan, restructure-log).
- `references/` — Claude's organized knowledge (empty so far; populated as needs arise).
- `artifacts/` — finished deliverables (empty so far).
- `skills/` — project-specific skill definitions (empty so far).
- `oscillitron/` — the Go runtime subproject. Its own `CLAUDE.md` governs code-mode behavior.
- `INDEX.md` — single entry point listing every loadable resource.

## How to run / build / test

All code-side commands run inside `oscillitron/`:

```
cd oscillitron
go build ./...
go test ./...
go run ./cmd/oscillitron
```

Knowledge-work side (this folder) has no build step — it's docs and design notes.

## Conventions

- **Code lives in `oscillitron/`; design lives at the project root.** When in code-mode, the subproject CLAUDE.md (`oscillitron/CLAUDE.md`) takes precedence and is more specific. This root file is the canonical source of truth for architecture and open decisions.
- **Provenance marker** required on every Claude-authored file (see top of this file).
- **No edits to `inputs/`** — ever.
- **Open decisions** stay in this file (the list below) until they're locked, at which point they migrate into the relevant section (Architecture, Stack, etc.) with a `LOCKED YYYY-MM-DD` tag.

## Open questions / decisions to make

### Recently locked

- ~~**Language and stack.**~~ Go, LOCKED 2026-05-18. See Stack & tooling above.
- ~~**Hermes integration.**~~ Wrap, not modify or fork. LOCKED 2026-05-18. See Architecture above.
- ~~**GitHub owner.**~~ `jrlmx2`. Module path `github.com/jrlmx2/oscillitron` matches the live repo at https://github.com/jrlmx2/oscillitron (private). LOCKED 2026-05-18.
- ~~**Subproject vs. sibling repo.**~~ Option B — single repo with code at `oscillitron/` subdir. LOCKED 2026-05-18. (Reverses library-plan §8's "open" status.)
- ~~**Specialist seed list — subject-based or function-based?**~~ Brain-function roles, NOT subject domains. LOCKED 2026-05-18. See Architecture above. Working seed set: perception/parsing, retrieval, planning/decomposition, reasoning/transformation, critic/verification, composition/output.
- ~~**Inhibitor as node vs. edge property.**~~ Edge property. LOCKED 2026-05-18. See Architecture above.
- ~~**Per-instance vs. shared resources.**~~ Brain-mirrored: shared base weights, per-instance playbooks, per-instance episodic + shared semantic retrieval, per-instance session memory, shared verifier/topology signal. LOCKED 2026-05-18. See Architecture above.
- ~~**Solo or collaborative?**~~ Solo for now. LOCKED 2026-05-18.
- ~~**Router design.**~~ Under the call-tree model, "router" collapses to a brain-function *dispatcher* — map `BrainFunction → specialist instance`. LOCKED 2026-05-18. The interesting decisions move to decomposition (owned by each brain function), termination (owned by the runner walking the tree), and recomposition (owned by `pkg/recomposer` and the parent invocation). The static-edge-weight question is moot: there are no static routing weights. The existing `router.Decision.Destinations []Destination` slice becomes "sub-APs this invocation emits" and carries forward as the dispatch shape.
- ~~**Playbook persistence unit.**~~ Both — exemplars feeding recipes, with consolidation as a background "sleep" job. LOCKED 2026-05-18 (principle); schemas to be designed when implementation lands. Exemplars are ground truth, per-specialist; recipes are consolidated distillations; vetted cross-specialist recipes can promote to the shared semantic pool (gated by curation).
- ~~**AP shape — AP vs. trace split.**~~ Separate, lean AP vs. fat trace. LOCKED 2026-05-18. AP (envelope) is what the next invocation ingests — must stay lean (token cost compounds over every hop). Trace is what the learning loop reads — verifier feedback, retrieval refs, full tree topology, cost ledger, calibration metadata — kept off the inference path, can be as fat as needed. Brain analog: axonal action potential vs. hippocampal episodic index that feeds cortical consolidation; different timescales, different consumers. Final field-level schema stable once a real Hermes adapter exercises it; sketch in `scratch/design-notes.md`.

### Still open

(none currently blocking v0)

### Deferred (not blocking v0)

- **License.** Apache 2.0 leading per framework-design.md §11.1; not yet added. Repo is private, no urgency. Decide before going public.
- **Hardware parallelism.** Multi-GPU, inference-server sharing, sibling-concurrent dispatch, backpressure/queueing. Out of scope for v0. Dispatcher interface should still be future-shaped (cheap insurance against an interface break later) but no concurrency logic ships.
- **Async sub-AP emission.** v0 is synchronous; configurable async is a real axis but requires inhibition that reasons across in-flight asynchronous subtrees. Revisit when async workloads motivate it.

## Notes for Claude

- This file is design-mode guidance. For code-mode work, `oscillitron/CLAUDE.md` is more specific — read it before touching `oscillitron/`.
- Open decisions in the list above are real blockers. Don't pick implementations for them silently; ask first.
- When updating this file, keep it concise — link out to `scratch/library-plan.md` and `scratch/design-notes.md` rather than inlining their content.
