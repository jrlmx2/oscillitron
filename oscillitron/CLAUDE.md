<!-- CLAUDE GENERATED -->
# Oscillitron — Go runtime

This subproject is the **code** for Oscillitron. The design lives in the parent project:

- Parent `CLAUDE.md`: `../CLAUDE.md` — project-wide conventions (provenance rule, folder layout) and the locked architectural decisions.
- Parent `INDEX.md`: `../INDEX.md` — entry point for design docs.
- Library plan v0.2: `../scratch/library-plan.md` — the spec this code implements. Read §5 for type signatures and §9 for the first-week task order.
- Working design notes: `../scratch/design-notes.md` — open architectural questions still in flight (AP shape, inhibitor placement, summary format).

The parent project is the source of truth for **what** to build and **why**; this subproject is the source of truth for **how it's currently built**.

## Status

Stage: **call-tree skeleton** — the call-tree model from parent CLAUDE.md ("Architecture", locked 2026-05-18) is implemented end-to-end against stub adapters. Build is green; demo runs and recomposes a 4-node tree.

What's here:

- **Session envelope** (`pkg/session`) — AP-as-invocation shape: `SchemaVersion`, `BrainFunction`, `Input`, `OutputSchema`, `ParentRef`, `Budget`, and on completion `Output{Content, Classification, Confidence, Signals, Contradictions, OpenQuestions, SubAPs, ExitReason}`. `NewRoot()` helper builds an entry-point envelope.
- **Classification levels** (`pkg/classification`).
- **Adapter** (`pkg/adapter`) — interface returns `(session.Output, error)`. Per-invocation session lifecycle is the adapter's responsibility.
- **Stub adapter** (`pkg/adapter/stub`) — configurable mode/confidence/classification/signals/SubAPs. Used by tests and demo.
- **Oscillator** (`pkg/oscillator`) — thin brain-function-typed wrapper around an adapter. Synchronous `Invoke(ctx, env) Envelope`. No goroutine, no channels.
- **Registry** (`pkg/registry`) — `BrainFunction → *Oscillator` dispatch table. Replaces the deleted `pkg/topology`. No edges, no weights.
- **Runner** (`pkg/runner`) — synchronous recursive **tree-walker**. Dispatches via registry, checks inhibitor on the root→current path, descends into `Output.SubAPs`, recomposes children, propagates inhibited children up. Sync; no sibling parallelism. Belt-and-suspenders `MaxDepth` cap independent of per-AP `Budget.DepthRemaining`. Restart→Abort downgrade still in effect (no checkpointing yet).
- **Inhibitor** (`pkg/inhibitor`) interface with implementations:
  - `pkg/inhibitor/hardcap` — path-depth cap.
  - `pkg/inhibitor/confidence` — floor abort + window-drop restart on `Output.Confidence`.
  - `pkg/inhibitor/repetition` — exact-content cycling detector over a sliding window.
  - `pkg/inhibitor/contradictions` — single-invocation spike or cumulative `Output.Contradictions` cap.
  - `pkg/inhibitor/composite` — combines members; Abort > Restart > Continue precedence.
  - Argument is the root→current path through the call tree (slice shape unchanged; semantic interpretation differs).
- **Recomposer** (`pkg/recomposer`) — load-bearing now. `Recompose(ctx, parentOutput, children []Envelope) (Output, error)`. `Concat` impl ships: joins content, takes weakest-link confidence min, deduplicates signals / contradictions / open questions.
- **Demo** (`cmd/oscillitron`) — fires a `planning` root that emits `reasoning` + `critic` SubAPs; `reasoning` further emits a `retrieval` SubAP; tree resolves and recomposes back up.
- **Cost tracker** (`pkg/cost`) — `Pricing` + `Tracker` with actual + frontier-counterfactual ledgers. Not yet wired into the runner; lands with the real Hermes adapter.
- **Eval harness** (`pkg/eval`) — decoupled from the orchestrator (Runner is `func(ctx, Task) (string, error)`); no changes needed for the call-tree refactor.
- **Trace** (`pkg/trace`) — slog-backed `Tracer` with `Info` / `Error` sugar helpers and a `Discard` no-op. Oscillator, runner, and the demo now emit through `trace.Tracer` rather than `*slog.Logger` directly. The fat learning-loop trace record (verifier feedback, retrieval refs, etc.) lives here per the lean-AP-vs-fat-trace split.

**Deleted (no longer relevant under the call-tree model):**
- `pkg/topology` — replaced by `pkg/registry`. No edges, no weights.
- `pkg/router` and `pkg/router/rule` — collapsed into runner dispatch via the registry.
- `pkg/decomposer` — decomposition is what a brain function *does* when it emits SubAPs; the standalone interface no longer earns its keep. Root-envelope construction is now `session.NewRoot()`.

What's deliberately NOT here yet:

- Real Hermes adapter — see library-plan §9 step 4. The call-tree skeleton is the spec it will code against.
- Cost tracker wired into the runner — wiring lands with the real adapter so token counts come from somewhere real.
- Real grader implementations beyond substring — LLM-as-judge and rules-DSL graders are seam-reserved but not built.
- Real recomposer variants beyond Concat — LLM-driven recompose (re-invoke parent brain function with children outputs), tree-merge with conflict resolution. Plug in via the `Recomposer` interface.
- Sibling parallelism, async sub-AP emission, hardware parallelism — deferred (see parent CLAUDE.md).
- Checkpointing for inhibitor Restart — runner still downgrades Restart to Abort with annotated reason.
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
