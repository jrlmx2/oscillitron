# Oscillitron

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
- **Always record scores & findings to a durable file — never leave them only in chat/session history.** Whenever a run scores model outputs (a benchmark via `cmd/bench`, the kill-or-proceed gate via `cmd/phase1`, a grader/judge pass, or any eval), the scores and the findings drawn from them must land in a file, not just the conversation. The user should never have to scroll session history to recover a result. Convention:
  - **Raw machine scores:** use the bench's own persistence — `cmd/bench --report-out <path>.json` (full Report) and/or `--stream-out <path>.jsonl` (per-case, crash-safe). Write these alongside the run.
  - **Distilled findings:** summarize scores + what they mean into a dated file `scratch/bench-results-<YYYY-MM-DD>.md` (results tables, key findings, what's still broken), matching the existing format. Older deep-dive session analyses are archived under `scratch/archive/bench-findings-<YYYY-MM-DD>.md`.
  - When a finding is durable enough to reuse, promote it to a focused `references/` file and index it in `INDEX.md`.
  - Subagents that run scoring must write their scores+findings to such a file and report the path back — returning numbers only in their final message is not sufficient.

---

<!--
Project memory for Claude. Fill in the sections below as the project takes shape.
Keep it concise — link out to deeper docs rather than inlining them.
-->

## What this is

<!-- One- or two-sentence description: language/stack and what the project does. -->
Production-grade LLM handling at a fraction of the cost. A neural-ensemble runtime where weak/cheap base models are wrapped as "oscillators" coordinated through "action potentials" (spike-like events), with specialization growing organically over time inside scaffolded seed niches. No fine-tuning of model weights — specialization lives entirely in playbooks, retrieval indexes, prompt templates, and routing topology, all of which are cheap, inspectable, and rollback-safe.

## Status

- **Stage:** **v3 complete (2026-05-23).** v1 set the orchestration floor (vote+critique on cheap substrate ≈ frontier quality, locked at v1.0.0). v2 built the curation substrate (per-action exemplar store, cold-path mining, warm-path retrieval — v2.0.0 in dev). v3 added the inadequacy-coping layer: stakes-driven attempt scaling, prompt + response notice signals, calibrated confidence, cope rule-table dispatcher with frontier escalation. All 5 substrate adapters working (hermes, ollama, vllm, lmstudio, anthropic). Full test suite green under `-race`. Next: v4 — user-feedback intake (the keystone for calibrated learning over time).
- **Owner:** Jim (jrlmx2@gmail.com)
- **Created:** 2026-05-17
- **Code subproject:** [`oscillitron/`](oscillitron/) — Go runtime. See [`oscillitron/CLAUDE.md`](oscillitron/CLAUDE.md) for current package inventory + code-mode conventions.

## Stack & tooling

- **Language: Go (LOCKED 2026-05-18).** Rationale: goroutines + channels map cleanly onto N oscillators passing action potentials; static binary makes ops simple; the `anthropic-skills:go-agent-company` skill aligns with the multi-agent message-envelope pattern this project needs.
- **Package manager:** Go modules (stdlib only for v0; external deps introduced one at a time with justification).
- **Min Go version:** 1.26 (per `oscillitron/go.mod`; bumped from 1.21 on 2026-05-18).
- **Code location:** `oscillitron/` subdirectory (library-plan §8 Option B). If this is later promoted to a sibling repo per library-plan §8 Option A, the cross-link to `oscillitron/CLAUDE.md` becomes a README pointer to the new repo.

## Architecture

**Hermes integration: WRAP, not modify or fork (LOCKED 2026-05-18).**

Oscillitron sits *above* Hermes (github.com/NousResearch/hermes-agent) as an orchestration layer that runs N pre-seeded Hermes instances, each acting as a specialist oscillator. Hermes source is never modified. Rationale: preserve upstream feature flow from a fast-moving project (~99K GitHub stars in 8 weeks), isolate specialization drift between instances, and keep ops simple — a bad instance can be killed without a global rollback.

What lives in Oscillitron (the wrapper) vs. Hermes (the substrate):

- **Wrapper owns:** the action-potential bus between instances, the router specialist that decides which instance receives an AP, the verifier loop (grounded checks + thumbs + implicit signals + periodic audits), the playbook curation/garbage-collection layer, and cross-instance topology updates (which oscillator listens to which).
- **Hermes owns:** per-instance skill creation and growth, persistent memory, multi-channel I/O (Slack/Telegram/Discord/voice/etc.), per-peer agent isolation. Used as-is.

Specialization seeds are **predetermined** (the scaffolding) and **grow organically within their niche** (the plasticity). Brain analogy: anatomical priors plus cortical plasticity, not pure emergence.

**Specialists are nodes; processing flow is graph topology.** The router, and optionally a cheap intent classifier upstream of it, are also nodes. Everything else — input → reasoning → output sequencing, fast vs. slow paths — is a property of the graph (edges, weights, thresholds, path length, playbook richness), not separate node types. **The graph is the main learnable substrate.** Specialists grow within their niches via Hermes' skill creation; the bulk of self-improvement happens at the topology layer — strengthening edges, shifting thresholds, adding or pruning routes. Brain analog: cortical microcircuits are nearly identical across regions; specialization comes from what feeds in, not from a different circuit.

**Specialists are brain-function roles, NOT subject domains (LOCKED 2026-05-18).** Seed nodes are typed by cognitive function — perception/parsing, retrieval, planning/decomposition, reasoning/transformation, critic/verification, composition/output — analogous to functional cortical roles (sensory cortex, hippocampus, PFC, ACC, Broca). They are NOT typed by subject ("code specialist", "math specialist", "legal specialist"). Subject competence is an *emergent* property of which playbooks, exemplars, and retrieval shards a brain-function node accumulates over time, plus the topology that routes subject-shaped inputs toward it. Rationale: cortical microcircuits are uniform; specialization comes from afferents and learned weights, not from a different circuit per topic. Subject-based seeding pre-commits the system to a taxonomy we don't yet trust and duplicates effort across topics that share cognitive structure. **Enforcement:** if a draft, demo, doc, or chat suggestion reintroduces subject-based seed names (code/math/legal/writer/fact-check/etc.), rename to the brain-function role it actually plays. Note that under the uniform-node lock (2026-05-19, below), brain functions are *playbook tags* on a single workflow, not distinct node types. The current demo's `reasoner → critic → composer` framing is superseded by the evaluate/execute + 5-playbook shape; the refactor is queued behind the JSON envelope sketch.

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

**Parent blocks on subtree (LOCKED 2026-05-18; sibling-concurrent dispatch UNLOCKED 2026-05-21).** When an invocation emits sub-APs, the parent still blocks on the whole subtree before recomposing and returning — that semantic stays. What changed 2026-05-21: siblings *within* a single subtree can now dispatch concurrently (via `runner.Config.MaxConcurrency`), bounded by a static cap and optionally by a dynamic VRAM-derived cap (`Config.VRAMProbe + VRAMEstimator`). Inhibition under concurrent siblings is **strict cancellation**: the first sibling to fire `inhibitor.Abort` cancels in-flight siblings via context, matching the locked "one inhibited child inhibits the parent" rule. Async sub-AP emission (parent returns; sub-APs continue independently across subtrees) stays deferred — it requires cross-subtree inhibition reasoning that is its own design problem.

**Sibling concurrency in v0 (UNLOCKED 2026-05-21); multi-GPU and inference-server sharing still deferred.** The graph-walking layer can fan out sibling APs concurrently. The dispatcher interface stays future-shaped per the original lock — that paid off. What's still deferred: multi-GPU placement, inference-server sharing (one Hermes serving multiple tenants), backpressure/queueing across trees. The VRAM-aware dynamic concurrency cap (`pkg/vram`) is the v0 throttling mechanism — it bounds in-flight sessions per the operator's GPU headroom and a sliding-window per-session estimate capped by the model's context window. See `references/vram-platform-coverage.md` for the probe coverage matrix and `oscillitron/pkg/vram` for the estimator math.

**Library auto-manages concurrency by default (LOCKED 2026-05-21).** `runner.Config.MaxConcurrency = 0` (the zero value) means "library-managed": the runner constructs a VRAM probe, sliding-window estimator, and conservative `DefaultVRAMModel` automatically, and derives the per-wave concurrency cap from detected headroom each dispatch. A hard safety ceiling (`MaxConcurrencyCeiling`, default 8) prevents pathological goroutine counts even with abundant VRAM. Probe failure under the auto path falls back to **serial** (the safe choice when we can't measure), not to "unbounded." Operators opt out via `MaxConcurrency = 1` (strict serial) or set `N > 1` (static cap, still tightened by VRAM). Forcing operators to set these at runtime is dangerous — the design choice is "safe by default, override deliberately."

**Phase 1 workload: mundane office tasks with a nuance twist; verify before acting (LOCKED 2026-05-21).** The kill-or-proceed gate from `scratch/library-plan.md` §2.5 uses email drafting as the v0 workload — common-enough that everyone has opinions about what good looks like, nuanced enough that single-call cheap models reliably miss something (tone-mismatch, missing acknowledgment, generic close, scope assumptions). Architectural constraint that survives into Phase 2+: **tools and connectors execute *after* the orchestrator has produced a verified output, never inside the call tree.** Side effects are a separate post-task layer that reads a verified result and decides whether to act; the orchestrator only produces proposals. This matches the existing inhibitor + verifier-policy + judge philosophy (gating happens before output is consumed) and keeps Phase 1's measurement honest: we measure the orchestrator's quality at producing verifiable proposals, not its ability to execute. Phase 1 driver: `oscillitron/cmd/phase1`. Operator guide: `references/phase1-measurement-guide.md`.

**Uniform node model (LOCKED 2026-05-19).** No structurally distinct seed nodes per brain function. *One* AP-handling workflow runs at every recursion level. Specialization lives in the **playbook substrate keyed by action**, not in node types. This sharpens the brain-function lock-in (cortical microcircuits are uniform; specialization comes from learned weights, not different circuits) rather than breaking it. The "specialist" abstraction survives but moves out of the structural/code layer into the data layer — see "Specialists are substrate" below.

**Two-step AP: evaluate → execute (LOCKED 2026-05-19).** Every AP runs the same two-step workflow:

- **Evaluate** — an LLM call on Hermes-on-base that picks the right playbook for this AP from the v0 playbook set.
- **Execute** — runs the chosen playbook; produces a result and/or emits sub-APs.

Every AP evaluates — no trivial-skip path in v0. Evaluate is cheap-local-first; the frontier is *not* freely selectable by evaluate. Frontier model use is restricted to (a) the `delegate` runtime escalation gate (critic failed past retry budget) and (b) sampled `verify_judge` audits.

**v0 playbook set (LOCKED 2026-05-19).** Five playbooks the evaluate step can pick from:

| Playbook | Envelope-input | Execute-pulls | Output | Output category |
|---|---|---|---|---|
| `plan` | task | — | `{subtasks, recompose}` | emit_subtree |
| `process` | task | — | result | return_result |
| `critique` | prior result + context | — | pass / issues | verifier_signal |
| `verify_grounded` | result + check spec (from envelope) | — | pass / fail | verifier_signal |
| `compose` | `{scope_handle, expected_count}` | 2 results from scope channel | combined result | return_result |

Cut from earlier proposals: `parse` (premature differentiation of `process`), `terminate` (envelope flag, not a playbook), `delegate` (runtime escalation mechanism, not evaluate-visible).

**Three output categories (LOCKED 2026-05-19; non-uniform — envelope must encode).**

- **`emit_subtree`** — produces sub-APs into the parent's scope; doesn't return up.
- **`return_result`** — value flows up the tree to the parent.
- **`verifier_signal`** — pass/fail/issues flows to **the runtime**, not the next AP. Runtime owns retry / proceed / escalate policy.

**Plan bundles recompose spec (LOCKED 2026-05-19).** Plan's output is `{subtasks: [...], recompose: pairwise | sequential | none}`. Decomposition without a recompose spec is incomplete — can't decompose meaningfully without saying how it composes back.

**Compose input is scope-channel-based (LOCKED 2026-05-19).** A compose AP doesn't *receive* sibling results in its input. It receives `{scope_handle, expected_count}` and pulls results from a parent-scoped channel at execute time. Sibling-triggered semantics.

**Sibling dispatch is randomized (LOCKED 2026-05-19).** Runner pops ready sibling APs in random order, not emission order. Keeps v0 baseline honest about not relying on emission order; future parallel runtime won't change observable behavior.

**Specialists are substrate, not nodes (LOCKED 2026-05-19).** Per-instance playbook stores keyed by action tag. The "specialist" abstraction (per-brain-function persistent memory of exemplars, recipes, retrieval shards) survives the uniform-node lock — it just moves from the code layer (no distinct node types) to the data layer (per-action playbook stores).

**Verifier policy: phase ramp, not binary lock (LOCKED 2026-05-20).** Critique-emission is a *sampling policy* with a rate that ramps with substrate maturity, not a hard "(a) parent / (b) auto" pick. Bootstrap: 100% critique until N invocations. Steady-state: `sample_rate = max(floor, 1 - happiness_wilson_lower_bound)` over a sliding window of judge-sampling agreement. `happiness_scope ∈ {global, per_action}` is a runtime config; v0 records telemetry for both regardless of which drives the rate. Parent override (`needs_verification: true` on a child AP) forces critique on top of the baseline. v0 defaults: `bootstrap_threshold=10_000`, `floor=0.15`, `sliding_window=2_000`, `confidence_level=0.95`. All defaults are starting points; revisit on wild swings or saturation. Full design and rationale in [`scratch/design-notes.md`](scratch/design-notes.md) "Verifier policy."

**Judge sampling policy (LOCKED 2026-05-19).** 100% judge on un-grounded outputs, 10% sample on grounded. Revisit if cost gets ugly. This is the *audit* layer that feeds the verifier-policy happiness signal — distinct from the critique-sampling rate above.

**Pairwise compose: sequential self-chaining (LOCKED 2026-05-20).** One compose AP per scope, pulling pairs off the scope channel as siblings complete, reducing sequentially, re-entering its own result into the channel until `expected_count` reductions are done. *Not* pre-emission of N-1 compose APs. Rationale: sibling dispatch is already randomized, so pre-emission doesn't buy step-level determinism — just more APs. Trace stays faithful at the reduction level via `pkg/trace`. Full rationale in `scratch/design-notes.md` "Pairwise compose."

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
- **No edits to `inputs/`** — ever.
- **Open decisions** stay in this file (the list below) until they're locked, at which point they migrate into the relevant section (Architecture, Stack, etc.) with a `LOCKED YYYY-MM-DD` tag.

## Versioning & release tagging (LOCKED 2026-05-22)

Semantic versioning on annotated git tags + GitHub releases. **Claude manages tagging proactively** — when a release point is reached, create the annotated tag, push it, and `gh release create` with notes describing what landed.

| Tag shape | When |
|---|---|
| `vMAJOR.0.0` | A new architectural layer or substrate component lands (e.g., v1.0.0 = bench-ready foundation; v2.0.0 = curation/self-improvement loop). |
| `vMAJOR.MINOR.0` | Additive feature within the same architectural layer (new orchestrator, new grader, new benchmark loader). |
| `vMAJOR.MINOR.PATCH` | Bugfix or non-functional change on a released major. |

Between major-version development cycles, **no intermediate tags** — work accumulates in main until the next major is feature-complete, then tag once. Don't tag work-in-progress.

Tag the merge commit on `main`, not feature branches. Annotated tags only (`git tag -a`, never lightweight). Always push tags with `git push origin <tag>`. Always pair with a GitHub release (`gh release create`) for visibility.

Current state: **v1.0.0** at `main@d52edeb` (2026-05-22). v2.0.0 in development.

## PR workflow (LOCKED 2026-05-22)

**Branch from `main`, one PR at a time. Wait for merge between PRs.** Don't stack.

Stacked PRs caused real pain on the v1.0.0 cycle: each PR only merges into its *immediate base*, so 6 stacked PRs left main behind even after all were "merged." Recovery required opening a separate "land it all" PR.

Going forward, the loop is:

1. Branch from `origin/main` (fresh fetch first).
2. Build the change. Test. Commit. Push.
3. Open PR against `main`. Report URL to the user.
4. **Stop.** Wait for the user to merge (or to direct continuation).
5. After merge confirmation, fetch `origin/main`, branch again, start the next step.

If a follow-up step is *blocked* on the prior PR (e.g., uses types defined there), say so explicitly and wait. Don't preemptively stack to "save time" — the time saved is eaten by the recovery PR.

If the user explicitly asks to stack (e.g., for a tight series of related changes), honor that — but the default is one PR at a time off main.

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
- ~~**Uniform node model.**~~ One AP-handling workflow at every recursion level; specialization in the playbook substrate keyed by action, not in distinct node types. LOCKED 2026-05-19. See Architecture above.
- ~~**Two-step AP: evaluate → execute.**~~ Every AP evaluates (picks a playbook) then executes (runs it). Evaluate is cheap-local-first; frontier reserved for `delegate` escalation and `verify_judge` audits. LOCKED 2026-05-19. See Architecture above.
- ~~**v0 playbook set.**~~ Five playbooks: `plan`, `process`, `critique`, `verify_grounded`, `compose`. Cut: `parse`, `terminate`, `delegate`. LOCKED 2026-05-19. See Architecture above.
- ~~**Three output categories.**~~ `emit_subtree`, `return_result`, `verifier_signal`. Verifier signals go to the runtime, not the next AP. LOCKED 2026-05-19. See Architecture above.
- ~~**Plan bundles recompose spec.**~~ Plan output carries `{subtasks, recompose: pairwise|sequential|none}`. LOCKED 2026-05-19. See Architecture above.
- ~~**Compose input is scope-channel-based.**~~ Compose receives `{scope_handle, expected_count}`, pulls results from parent-scoped channel at execute time. LOCKED 2026-05-19. See Architecture above.
- ~~**Sibling dispatch is randomized.**~~ Runner pops ready sibling APs in random order. LOCKED 2026-05-19. See Architecture above.
- ~~**Specialists are substrate, not nodes.**~~ Per-instance playbook stores keyed by action tag. LOCKED 2026-05-19. See Architecture above.
- ~~**Verifier policy.**~~ Phase ramp with `sample_rate = max(floor, 1 - happiness_wilson_lower_bound)` over a sliding window of judge-sampling agreement; bootstrap at 100% for 10k invocations; `happiness_scope` configurable; defaults revisitable. LOCKED 2026-05-20. See Architecture above and `scratch/design-notes.md`.
- ~~**Judge sampling policy.**~~ 100% on un-grounded, 10% sample on grounded. LOCKED 2026-05-19. See Architecture above.
- ~~**Pairwise compose mechanism.**~~ Sequential self-chaining off the scope channel, not pre-emission of N-1 compose APs. LOCKED 2026-05-20. See Architecture above and `scratch/design-notes.md`.
- ~~**Sibling-concurrent dispatch.**~~ Unlocks the original "sync sub-AP emission" lock for siblings *within* a single subtree. Runner gains `Config.MaxConcurrency` (static cap) and optional `Config.VRAMProbe + VRAMEstimator` (dynamic VRAM-aware cap from `pkg/vram`). Strict cancellation on inhibitor.Abort. Parent still blocks on its subtree; cross-subtree async emission still deferred. UNLOCKED 2026-05-21. See Architecture above and `references/vram-platform-coverage.md`.
- ~~**Shared semantic pool implementation.**~~ Option 1 (locked 2026-05-21): whole pool injected as a stable preamble on every adapter call, operator-curated JSON file backing, mtime-based reload. Soft cap ~2000 tokens; over-budget emits a warning trace, doesn't fail the run. Programmatic writes deferred to a future curation layer. See `oscillitron/pkg/semanticpool` and the per-instance-vs-shared resource lock above.
- ~~**Library auto-manages concurrency + VRAM by default.**~~ Operator-forced runtime configuration of `MaxConcurrency` and VRAM probing was dangerous (forgetting → unmanaged concurrency or unmanaged memory). `MaxConcurrency = 0` (the zero value) now means "library-managed": the runner constructs the probe/estimator/model automatically and throttles per VRAM headroom each dispatch wave. Hard safety ceiling `MaxConcurrencyCeiling` (default 8). Probe failure falls back to serial. Operators opt out via `MaxConcurrency = 1` (strict serial) or set `N > 1` (static cap). LOCKED 2026-05-21. See `oscillitron/pkg/runner` and the Architecture section.
- ~~**Phase 1 workload: mundane office tasks with a nuance twist.**~~ Email drafting is the v0 workload — corpus in `oscillitron/cmd/phase1/cases.json` covers decline / clarify / firm-with-late-vendor / redirect / acknowledge-and-defer / ambiguous-urgency / scope-creep-pushback / graceful-no. Tools and connectors execute *after* the orchestrator has produced a verified output, never inside the call tree — Phase 1 only measures draft quality. LOCKED 2026-05-21. See Architecture above and `references/phase1-measurement-guide.md`.

### Still open

(none currently blocking v0; envelope sketch landed both in `scratch/design-notes.md` "JSON envelope sketch" and in code at `oscillitron/pkg/session`. Phase ramp wiring for the verifier policy is the next concrete code task — design locked 2026-05-20, runner integration in progress.)

### Deferred (not blocking v0)

- **License.** Apache 2.0 leading per framework-design.md §11.1; not yet added. Repo is private, no urgency. Decide before going public.
- **Hardware parallelism beyond sibling-concurrent dispatch.** Multi-GPU placement, inference-server sharing (one Hermes serving multiple tenants), cross-tree backpressure/queueing. Sibling-concurrent dispatch landed 2026-05-21 with VRAM-aware throttling (`runner.Config.MaxConcurrency`, `pkg/vram`); the rest is still out of scope for v0.
- **Async sub-AP emission across subtrees.** v0 still blocks the parent on its subtree; async emission (parent returns; sub-APs continue independently across subtrees) requires inhibition that reasons across in-flight asynchronous subtrees. Revisit when async workloads motivate it.

## Notes for Claude

- This file is design-mode guidance. For code-mode work, `oscillitron/CLAUDE.md` is more specific — read it before touching `oscillitron/`.
- Open decisions in the list above are real blockers. Don't pick implementations for them silently; ask first.
- When updating this file, keep it concise — link out to `scratch/library-plan.md` and `scratch/design-notes.md` rather than inlining their content.
