<!-- CLAUDE GENERATED -->
# Oscillitron — Go runtime

This subproject is the **code** for Oscillitron. The design lives in the parent project:

- Parent `CLAUDE.md`: `../CLAUDE.md` — project-wide conventions (provenance rule, folder layout) and the locked architectural decisions.
- Parent `INDEX.md`: `../INDEX.md` — entry point for design docs.
- Library plan v0.2: `../scratch/library-plan.md` — the spec this code implements. Read §5 for type signatures and §9 for the first-week task order.
- Working design notes: `../scratch/design-notes.md` — open architectural questions still in flight (AP shape, inhibitor placement, summary format).

The parent project is the source of truth for **what** to build and **why**; this subproject is the source of truth for **how it's currently built**.

## Status

Stage: **uniform-node refactor in flight — Stages 1+2+3 of 5 landed.** Envelope, adapter, and runner are rewritten onto the locked uniform-node + evaluate/execute shape (parent CLAUDE.md, scratch/design-notes.md "JSON envelope sketch"). Recomposer, Hermes adapter, and demo are stubbed or build-tagged off and land in Stages 4–6. Build is green; full test suite passes on the rewritten packages.

What's here:

- **Session envelope** (`pkg/session`) — uniform-node + evaluate/execute shape. Fields: `SchemaVersion`, `ID`, `ParentID`, `RootID`, `Path` (root→this), `ScopeHandle`, `Input` (`Payload{Kind, Content, ContentHash}`), `OutputSchema`, `Classification`, `NeedsVerification`, `VerifySpec`, `Budget`, `Evaluate{Playbook, Rationale, Confidence, TokensUsed}`, `Execute{Category, EmitSubtree|ReturnResult|VerifierSignal, TokensUsed}`, `ExitReason`, `Trace`, `Audit`. No `BrainFunction` field — playbook is *picked by evaluate*, not declared. `NewRoot()` and `NewChild()` helpers stamp the call-tree plumbing. Three execute categories: `emit_subtree` (plan), `return_result` (process, compose), `verifier_signal` (critique, verify_grounded). Predicates: `IsComplete`, `IsLeaf`, `IsInhibited`, `Depth`.
- **Classification levels** (`pkg/classification`).
- **Adapter** (`pkg/adapter`) — interface has two methods: `Evaluate(ctx, env) (env, err)` (picks the playbook) and `Execute(ctx, env) (env, err)` (runs the chosen playbook). Per-invocation session lifecycle is the adapter's responsibility.
- **Stub adapter** (`pkg/adapter/stub`) — configurable per-playbook. `WithDefaultPlaybook` / `WithEvaluator` shape Evaluate; `WithEmitSubtree` / `WithReturnResult` / `WithVerifierSignal` shape Execute by playbook. Counters: `Calls`, `EvalCalls`, `ExecuteCalls`, `CallsForPlaybook`. Used by tests.
- **Hermes adapter** (`pkg/adapter/hermes`) — **build-tagged off** (`//go:build hermes_stage5`). Old `(session.Envelope) -> session.Output` shape doesn't compose with the new evaluate/execute split; Stage 5 rewrites the structured-output contract to the new envelope and lifts the tag.
- **Inhibitor** (`pkg/inhibitor`) — edge-property interface unchanged. Implementations updated to read the new envelope:
  - `pkg/inhibitor/hardcap` — path-depth cap (unchanged; reads `Edge.Path` length).
  - `pkg/inhibitor/confidence` — reads `Execute.ReturnResult.Confidence`. APs that didn't emit a return_result are skipped (missing signal, not a negative signal).
  - `pkg/inhibitor/repetition` — reads `Execute.ReturnResult.Result.Content`.
  - `pkg/inhibitor/contradictions` — reads `Execute.ReturnResult.Signals.Contradictions`.
  - `pkg/inhibitor/composite` — combines members; Abort > Restart > Continue precedence (unchanged).
- **Runner** (`pkg/runner`) — synchronous recursive tree-walker on the new envelope. `Run(ctx, cfg, root) (Result, error)`. Calls `adapter.Evaluate` then `adapter.Execute` on every AP; branches on `Execute.Category`: `return_result` bubbles, `verifier_signal` records in `RunState` (per the locked "verifier signals go to the runtime, not the next AP" rule), `emit_subtree` constructs child envelopes, dispatches in **randomized sibling order** (`math/rand/v2` PCG; seedable via `Config.Rand`), recurses synchronously into each, collects child return_result payloads, and calls `Recomposer.Recompose(ctx, spec, payloads)` with the plan's `RecomposeSpec`. Inhibitor fires on every parent→child edge after the child resolves (root not checked). Strict semantics: one inhibited child inhibits the parent (no partial recomposition). Restart→Abort downgrade still in effect. Belt-and-suspenders `MaxDepth` cap alongside per-AP `Budget.DepthRemaining`. `Result` carries `Root`, `ResolvedPayload`, `Subtree` (parent ID → resolved children), `State`. Recomposer is required for any tree with emit_subtree APs; while Stage 4 is in flight, tests use a local fake recomposer.
- **Recomposer** (`pkg/recomposer`) — **placeholder.** Interface is reshaped to `Recompose(ctx, RecomposeSpec, []ReturnResultPayload) (ReturnResultPayload, error)`. Concat returns `ErrStagePending` until Stage 4.
- **Demo** (`cmd/oscillitron`) — **placeholder** that prints a stage-pending status line. Stage 6 rewrites onto the new shape (plan → process sub-APs → compose pulled from scope channel; critique fired per verifier policy).
- **Properties config** (`pkg/config`) — tiny stdlib-only `.properties` loader (Java-style `key=value`, `#` / `!` comments, dotted keys for hierarchy, typed accessors). Used by the demo for both single-endpoint (`hermes.url`) and multi-endpoint (`hermes.endpoints.<bf>.url`) Hermes setups. Deliberately a fraction of Spring Boot's surface — no profiles, no relaxed binding, no SpEL.
- **Cost tracker** (`pkg/cost`) — `Pricing` + `Tracker` with actual + frontier-counterfactual ledgers. Not yet wired into the runner; lands with the real Hermes adapter.
- **Eval harness** (`pkg/eval`) — decoupled from the orchestrator (Runner is `func(ctx, Task) (string, error)`); no changes needed for the call-tree refactor.
- **Trace** (`pkg/trace`) — slog-backed `Tracer` with `Info` / `Error` sugar helpers and a `Discard` no-op. Oscillator, runner, and the demo now emit through `trace.Tracer` rather than `*slog.Logger` directly. The fat learning-loop trace record (verifier feedback, retrieval refs, etc.) lives here per the lean-AP-vs-fat-trace split.

**Deleted (no longer relevant under the uniform-node model):**
- `pkg/oscillator` — uniform-node lock kills the brain-function-typed wrapper. One adapter handles every AP; the playbook is *picked* by evaluate, not declared.
- `pkg/registry` — no `BrainFunction → instance` mapping; one adapter does it all.
- `pkg/topology`, `pkg/router`, `pkg/router/rule`, `pkg/decomposer` — already gone under the earlier call-tree refactor.

What's deliberately NOT here yet:

- Runner, recomposer, Hermes adapter, demo — Stages 3–6 of the uniform-node refactor.
- Multi-instance Hermes exercised end-to-end — locked design, not in v0 dev path. Stage 5 reinstates the Hermes adapter on the new envelope; multi-instance wiring comes after.
- Approval handling — `/v1/runs/{id}/approval`. Returns to the adapter in Stage 5.
- Cost tracker wired into the runner.
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

### Smoke-testing the Hermes adapter against a real local Hermes

**Build-tagged off during Stage 1–4 of the uniform-node refactor.** To compile and exercise the existing Hermes code, pass `-tags hermes_stage5`. Stage 5 rewrites the structured-output contract onto the new envelope and lifts the tag. The block below is preserved for reference and will return to relevance in Stage 5.

```
# In a separate shell, with hermes-agent installed and configured
# (model provider keys in ~/.hermes/.env, etc.):
hermes gateway start
# By default api_server listens on 127.0.0.1:8642. Confirm with:
curl -s http://127.0.0.1:8642/v1/models

# Then run the demo through Hermes:
go run ./cmd/oscillitron --hermes http://127.0.0.1:8642
# Or pick the model explicitly:
go run ./cmd/oscillitron --hermes http://127.0.0.1:8642 --hermes-model openrouter:openai/gpt-4o-mini

# Or commit settings to a file and reuse:
cp cmd/oscillitron/oscillitron.properties.example oscillitron.properties
# (uncomment and edit the hermes.url / hermes.model lines, then:)
go run ./cmd/oscillitron --config oscillitron.properties
# CLI flags still win; --hermes "" forces back to stub mode.
```

`--hermes` (when Stage 5 reinstates it) will use `hermes.SingleEndpoint` — one Hermes per brain function via a per-action endpoint map remains the locked design.

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
