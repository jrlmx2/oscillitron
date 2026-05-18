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

**Specialists are nodes; processing flow is graph topology.** The system has fewer node types than first sketched. Content specialists (code, math, writing, retrieval, etc.) are real nodes because they carry distinct competence. The router, and optionally a cheap intent classifier upstream of it, are also nodes. Everything else — input → reasoning → output sequencing, fast vs. slow paths, inhibition — is a property of the graph (edges, weights, thresholds, path length, playbook richness), not separate node types. **The graph is the main learnable substrate.** Content specialists grow within their niches via Hermes' skill creation; the bulk of self-improvement happens at the topology layer — strengthening edges, shifting thresholds, adding or pruning routes. Brain analog: cortical microcircuits are nearly identical across regions; specialization comes from what feeds in, not from a different circuit.

**Action potentials carry summaries.** The handoff payload between specialists is the compressed summary the upstream session produced on exit. The "spike" is just a concrete summary handoff sized to be cheap for the downstream specialist to ingest. See `scratch/design-notes.md` for the working session-lifecycle, summary, and inhibition design.

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

### Still open

- **Content-specialist seed list.** Now narrower than first hypothesized because processing-role specialists collapsed into graph topology. Need ~3–6 content nodes to start: candidates are code, math/structured reasoning, retrieval/research, writing. Domain layers (legal, medical, finance) ride on top later. The skeleton's demo uses `code-analyst`, `fact-check`, `writer` as placeholders.
- **Router design.** v0 is rule-based (`oscillitron/pkg/router/rule`); picks the highest-weight outgoing edge. Open: when does content-aware routing land, and whether an upstream cheap intent classifier is a separate node or folded into the router.
- **Per-instance vs. shared resources.** Working hypothesis: base models shared (cost), playbook stores isolated, retrieval mostly isolated with a small shared general-knowledge pool, session memory per-instance.
- **Unit of persistence in the playbook store.** Exemplars (input → good-output pairs), playbooks (recipes), or both with exemplars feeding playbooks?
- **AP/summary shape.** Skeleton ships with a structured envelope (`pkg/session.Envelope`) wrapping a freeform body (`Outcome.Verdict`) — the hybrid hypothesis from `scratch/design-notes.md`. Final shape stable once a real Hermes adapter exercises it.
- **Inhibitor as node vs. edge property.** Open. Skeleton's v0 is a runner-called process (`pkg/inhibitor`), per `scratch/design-notes.md`.
- **GitHub owner.** Module path is `github.com/jrlmx2/oscillitron` placeholder — change once a real org is chosen. Library-plan §7.1.
- **License.** Apache 2.0 leading per framework-design.md §11.1; not yet added. Library-plan §7.2.
- Solo project or collaborative?

## Notes for Claude

- This file is design-mode guidance. For code-mode work, `oscillitron/CLAUDE.md` is more specific — read it before touching `oscillitron/`.
- Open decisions in the list above are real blockers. Don't pick implementations for them silently; ask first.
- When updating this file, keep it concise — link out to `scratch/library-plan.md` and `scratch/design-notes.md` rather than inlining their content.
