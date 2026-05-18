<!-- CLAUDE GENERATED -->
# Oscillitron — Go runtime

This subproject is the **code** for Oscillitron. The design lives in the parent project:

- Parent `CLAUDE.md`: `../CLAUDE.md` — project-wide conventions (provenance rule, folder layout) and the locked architectural decisions.
- Parent `INDEX.md`: `../INDEX.md` — entry point for design docs.
- Library plan v0.2: `../scratch/library-plan.md` — the spec this code implements. Read §5 for type signatures and §9 for the first-week task order.
- Working design notes: `../scratch/design-notes.md` — open architectural questions still in flight (AP shape, inhibitor placement, summary format).

The parent project is the source of truth for **what** to build and **why**; this subproject is the source of truth for **how it's currently built**.

## Status

Stage: **scaffolding — Phase 2 thin skeleton only** (per library-plan §3).

What's here:

- Session envelope types (`pkg/session`) — the AP/handoff shape, per library-plan §5.1.
- Classification levels (`pkg/classification`).
- Adapter interface (`pkg/adapter`) with a stub implementation for tests and the demo.
- Oscillator type (`pkg/oscillator`) — wraps an adapter behind a goroutine + input channel.
- Topology (`pkg/topology`) — directed graph of oscillator edges with weights.
- Router interface (`pkg/router`) with a rule-based v0 (`pkg/router/rule`).
- Inhibitor interface (`pkg/inhibitor`) with implementations:
  - `pkg/inhibitor/hardcap` — chain-length cap (belt-and-suspenders).
  - `pkg/inhibitor/confidence` — low-floor abort + window-drop restart on `Outcome.Confidence`.
  - `pkg/inhibitor/repetition` — exact-verdict cycling detector over a sliding window.
  - `pkg/inhibitor/contradictions` — single-session spike or cumulative `Outcome.Contradictions` cap.
  - `pkg/inhibitor/composite` — combines members with Abort > Restart > Continue precedence; concatenates reasons.
- Demo runner (`cmd/oscillitron`) that fires an AP through a 3-oscillator topology with a composite inhibitor and logs each hop.
- Cost tracker (`pkg/cost`) — `Pricing` + `Tracker` with parallel actual + frontier-counterfactual ledgers. Goroutine-safe. Not yet wired into the runner; that lands with the real Hermes adapter.
- Eval harness (`pkg/eval`) — `Grader` interface, substring-match stub grader, and a `Run` function that drives a workload through a caller-supplied `Runner` (so the three Phase 1 comparison arms in library-plan §9 step 11 are substitutable).
- Decomposer (`pkg/decomposer`) — `Decomposer` interface + `Passthrough` no-op impl. Manual-workflow and LLM-driven impls deferred to subpackages.
- Recomposer (`pkg/recomposer`) — `Recomposer` interface + `Concat` impl. Tree-merge impl deferred (library-plan Phase 5).
- Trace (`pkg/trace`) — `Tracer` interface with an slog-backed default. Scaffold-only: existing slog callsites in oscillator/runner/cmd are not yet migrated. New code should reach for `trace.Tracer`.

Runner currently downgrades inhibitor `Restart` to `Abort` (logged with an annotated reason) because checkpointing isn't built yet. Replace with a real restart path when checkpointing lands.

What's deliberately NOT here yet:

- Real Hermes adapter — see library-plan §9 step 4.
- Cost tracker wired into the runner — package is ready (`pkg/cost`); wiring lands with the real adapter so token counts come from somewhere real.
- Real grader implementations beyond substring — LLM-as-judge and rules-DSL graders are seam-reserved but not built.
- Manual / LLM-driven decomposer impls — `Passthrough` is the only impl today.
- Tree-merge recomposer — `Concat` is the only impl today.
- Trace migration — `pkg/trace` exists but oscillator/runner/cmd still call slog directly.
- Anything compliance-shaped (audit ledger, manifest, classification routing) — Phase 4.

## Build, run, test

```
go build ./...
go test ./...
go run ./cmd/oscillitron
```

Requires Go 1.26+ (current toolchain on dev machine; bumped from 1.21 on 2026-05-18).

## Conventions

- **Stdlib only** for the skeleton. Reach for external deps only when a real need lands (Hermes client, persistence, observability backend). Each new dep should be justified in `../scratch/design-notes.md` or its own ADR.
- **Interfaces in the package they're consumed in.** `Adapter` is in `pkg/adapter` because everyone consumes it. Concrete impls live in subpackages (`pkg/adapter/stub`).
- **Tests next to code** (`foo_test.go` alongside `foo.go`). Integration tests under `internal/test/`.
- **No `main` logic in `cmd/`.** `cmd/oscillitron/main.go` wires components together and runs them; all logic lives in `pkg/`.
- **Provenance marker.** Every file Claude authors has `// CLAUDE GENERATED` at the top.

## Repo facts (locked 2026-05-18)

- **Module path:** `github.com/jrlmx2/oscillitron`. Matches the live private repo at https://github.com/jrlmx2/oscillitron.
- **Layout:** Option B — Go module lives at `oscillitron/` inside the parent knowledge-work repo. Not a sibling repo. `../scratch/...` links from this file are stable.

## Open placeholders to resolve before publishing

- **License.** No LICENSE file yet. Apache 2.0 is the leading recommendation per the framework design doc §11.1; confirm and add before the repo goes public.

## When to ask vs. proceed

Proceed without asking on:

- Mechanical code that implements an already-locked design (anything in library-plan §5 type signatures).
- Stdlib-only additions to existing packages.
- Test additions.
- Refactoring within a package that doesn't change its public surface.

Ask before:

- Adding a new external dependency.
- Changing the Session Envelope schema in a way that breaks JSON compatibility (library-plan §2.4 commits to schema stability).
- Picking implementations for items listed as "Open" in `../scratch/library-plan.md` §7 or `../scratch/design-notes.md`.
- Anything that touches `../inputs/` — that's user-owned per the parent provenance rule.

## Pointers for Claude Code's first session

1. Read this file, then `../CLAUDE.md`, then `../scratch/library-plan.md` (especially §5 and §9), then `../scratch/design-notes.md`.
2. Run `go test ./...` to confirm the skeleton is green on your machine. If it isn't, fix before adding new code.
3. The natural next move per library-plan §9 step 4 is standing up a real Hermes instance and implementing `pkg/adapter/hermes`. That's a much bigger task than the skeleton — likely warrants its own session.
4. The other natural next move is filling out the inhibitor's drift signals (currently only hard-cap). See `design-notes.md` "Inhibition as circuit-breaker" for the list of signals to detect.
