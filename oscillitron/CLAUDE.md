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

Runner currently downgrades inhibitor `Restart` to `Abort` (logged with an annotated reason) because checkpointing isn't built yet. Replace with a real restart path when checkpointing lands.

What's deliberately NOT here yet:

- Real Hermes adapter — see library-plan §9 step 4.
- Cost tracker — Phase 1 deliverable (`pkg/cost`).
- Eval harness — Phase 1 deliverable (`pkg/eval`).
- Decomposer / Recomposer implementations — Phase 2 deliverables not yet on the skeleton.
- Trace package — slog calls are inlined in `cmd/oscillitron` for now.
- Anything compliance-shaped (audit ledger, manifest, classification routing) — Phase 4.

## Build, run, test

```
go build ./...
go test ./...
go run ./cmd/oscillitron
```

Requires Go 1.21+ (uses `log/slog`).

## Conventions

- **Stdlib only** for the skeleton. Reach for external deps only when a real need lands (Hermes client, persistence, observability backend). Each new dep should be justified in `../scratch/design-notes.md` or its own ADR.
- **Interfaces in the package they're consumed in.** `Adapter` is in `pkg/adapter` because everyone consumes it. Concrete impls live in subpackages (`pkg/adapter/stub`).
- **Tests next to code** (`foo_test.go` alongside `foo.go`). Integration tests under `internal/test/`.
- **No `main` logic in `cmd/`.** `cmd/oscillitron/main.go` wires components together and runs them; all logic lives in `pkg/`.
- **Provenance marker.** Every file Claude authors has `// CLAUDE GENERATED` at the top.

## Open placeholders to resolve before publishing

- **Module path.** Currently `github.com/jrlmx2/oscillitron` (derived from email prefix). Change to the real GitHub owner once chosen (per library-plan §7.1). `sed -i '' 's|github.com/jrlmx2/oscillitron|github.com/<owner>/oscillitron|g'` across the tree.
- **License.** No LICENSE file yet. Apache 2.0 is the leading recommendation per the framework design doc §11.1; confirm and add.
- **Subproject vs. sibling repo.** This is currently a subdirectory (library-plan §8 Option B). If you later split to Option A (sibling repo), the `../scratch/...` links above break — they'd become README references back to the parent knowledge-work project.

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
