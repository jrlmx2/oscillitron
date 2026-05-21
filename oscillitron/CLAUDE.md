<!-- CLAUDE GENERATED -->
# Oscillitron — Go runtime

This subproject is the **code** for Oscillitron. The design lives in the parent project:

- Parent `CLAUDE.md`: `../CLAUDE.md` — project-wide conventions (provenance rule, folder layout) and the locked architectural decisions.
- Parent `INDEX.md`: `../INDEX.md` — entry point for design docs.
- Library plan v0.2: `../scratch/library-plan.md` — the spec this code implements. Read §5 for type signatures and §9 for the first-week task order.
- Working design notes: `../scratch/design-notes.md` — open architectural questions still in flight (AP shape, inhibitor placement, summary format).

The parent project is the source of truth for **what** to build and **why**; this subproject is the source of truth for **how it's currently built**.

## Status

Stage: **uniform-node refactor complete (Stages 1–6 all landed); verifier-policy phase ramp wired; judge sampling layer in place behind a Judge interface; sibling-concurrent dispatch with VRAM-aware throttling (unlock 2026-05-21).** Envelope, adapter, runner, recomposer, Hermes adapter, and demo are all on the uniform-node + evaluate/execute shape (parent CLAUDE.md, scratch/design-notes.md "JSON envelope sketch"). The verifier policy (locked 2026-05-20) drives critique injection per return_result child, with parent override and per-action telemetry. The judge sampler (locked 2026-05-19; 100% un-grounded / 10% grounded) audits resolved critiques, compares verdicts against a frontier Judge, and feeds agreement into the policy's happiness window. The runner dispatches siblings concurrently up to `Config.MaxConcurrency`, optionally throttled by `pkg/vram` (multi-platform GPU probe + sliding-window per-session estimator). A real frontier-backed Judge implementation is the next substrate piece (stub + interface land here; Claude-API-backed impl is a follow-up). Build is green; full test suite passes across every package under `go test -race`.

What's here:

- **Session envelope** (`pkg/session`) — uniform-node + evaluate/execute shape. Fields: `SchemaVersion`, `ID`, `ParentID`, `RootID`, `Path` (root→this), `ScopeHandle`, `Input` (`Payload{Kind, Content, ContentHash}`), `OutputSchema`, `Classification`, `NeedsVerification`, `VerifySpec`, `Budget`, `Evaluate{Playbook, Rationale, Confidence, TokensUsed}`, `Execute{Category, EmitSubtree|ReturnResult|VerifierSignal, TokensUsed}`, `ExitReason`, `Trace`, `Audit`. No `BrainFunction` field — playbook is *picked by evaluate*, not declared. `NewRoot()` and `NewChild()` helpers stamp the call-tree plumbing. Three execute categories: `emit_subtree` (plan), `return_result` (process, compose), `verifier_signal` (critique, verify_grounded). Predicates: `IsComplete`, `IsLeaf`, `IsInhibited`, `Depth`.
- **Classification levels** (`pkg/classification`).
- **Adapter** (`pkg/adapter`) — interface has two methods: `Evaluate(ctx, env) (env, err)` (picks the playbook) and `Execute(ctx, env) (env, err)` (runs the chosen playbook). Per-invocation session lifecycle is the adapter's responsibility.
- **Stub adapter** (`pkg/adapter/stub`) — configurable per-playbook. `WithDefaultPlaybook` / `WithEvaluator` shape Evaluate; `WithEmitSubtree` / `WithReturnResult` / `WithVerifierSignal` shape Execute by playbook. Counters: `Calls`, `EvalCalls`, `ExecuteCalls`, `CallsForPlaybook`. Used by tests.
- **Hermes adapter** (`pkg/adapter/hermes`) — adapter against the OpenAI-compatible HTTP gateway shipped by hermes-agent. Two-step contract: `Evaluate` POSTs to a dedicated `EvaluateEndpoint` (cheap-local-first per lock) and parses `{playbook, rationale, confidence}` from the run's structured output; `Execute` looks up `ExecuteEndpoints[playbook]` and parses one of three playbook-specific JSON payloads (emit_subtree for plan, return_result for process/compose, verifier_signal for critique/verify_grounded). Session IDs are **per-AP per-phase** (`<envID>:<phase>`), giving each invocation a fresh Hermes session per the locked "Specialist vs. invocation" rule (clean isolation, no cross-invocation contamination at the working-memory level). KV-cache hits across calls come from the underlying engine's global prefix caching (vLLM-style — keyed by token-prefix bytes, not by session_id), not from sharing sessions across APs. The `cmd/probe-prefix-cache` probe verifies that the deployed engine does this. Per-AP isolation is also a prerequisite for sibling concurrency (`runner.Config.MaxConcurrency`): concurrent calls on the same session_id would serialize at the engine layer or behave unpredictably. Approvals are rejected as inhibited (orchestrator owns gating, not the substrate). Two builders: `SingleEndpoint(baseURL, model)` binds evaluate + every v0 playbook to one Hermes (v0 dev shape); `MultiEndpoint(evaluate, byPlaybook)` routes each playbook to its own Hermes process (per-instance playbook substrate, locked 2026-05-18) and rejects missing entries up front. An httptest-based smoke test (`TestMultiInstance_RoutesEachPlaybookToOwnHermes`) stands up N independent fake Hermeses and confirms each `/v1/runs` request lands on the right server — the canary against routing regressions silently mixing per-playbook state. `RequireStructured` toggles strict JSON enforcement; default false (low-confidence fallback per category). Per-step `RawEvaluateInstructions` / `RawExecuteInstructions` override the default prompts. Cost ledger records once per phase. SSE decoder in `sse.go` is unchanged from the old shape.
- **Inhibitor** (`pkg/inhibitor`) — edge-property interface unchanged. Implementations updated to read the new envelope:
  - `pkg/inhibitor/hardcap` — path-depth cap (unchanged; reads `Edge.Path` length).
  - `pkg/inhibitor/confidence` — reads `Execute.ReturnResult.Confidence`. APs that didn't emit a return_result are skipped (missing signal, not a negative signal).
  - `pkg/inhibitor/repetition` — reads `Execute.ReturnResult.Result.Content`.
  - `pkg/inhibitor/contradictions` — reads `Execute.ReturnResult.Signals.Contradictions`.
  - `pkg/inhibitor/composite` — combines members; Abort > Restart > Continue precedence (unchanged).
- **Runner** (`pkg/runner`) — recursive tree-walker on the envelope. `Run(ctx, cfg, root) (Result, error)`. Calls `adapter.Evaluate` then `adapter.Execute` on every AP; branches on `Execute.Category`: `return_result` bubbles, `verifier_signal` records in `RunState`, `emit_subtree` constructs child envelopes and dispatches in **randomized sibling order** (`math/rand/v2` PCG; seedable via `Config.Rand`). **Concurrency is library-managed by default**: `Config.MaxConcurrency = 0` (the zero value) means "auto" — the runner constructs `vram.Auto()` + `DefaultSlidingWindowEstimator` + `DefaultVRAMModel` automatically and derives the per-wave cap from detected headroom, bounded above by `MaxConcurrencyCeiling` (default 8). `MaxConcurrency = 1` is strict serial; `N > 1` is a static cap further tightened by VRAM. Probe failure under the auto path falls back to serial (the safe choice when we can't measure). **Strict cancellation under concurrency**: the first sibling to fire inhibitor.Abort cancels the others via context, matching the locked one-inhibited-child-inhibits-the-parent rule. Inhibitor fires on every parent→child edge after the child resolves (root not checked). Restart→Abort downgrade still in effect. `Config.MaxInputBytes` optionally inhibits any AP whose `env.Input.Content` exceeds the budget — surfaces prompt bloat as a test failure rather than silent VRAM growth. Belt-and-suspenders `MaxDepth` cap alongside per-AP `Budget.DepthRemaining`. `Result` carries `Root`, `ResolvedPayload`, `Subtree`, `State`. Recomposer is required for any tree with emit_subtree APs. Race-detector-clean (`r.mu` serializes shared state, rand, and policy/sampler accesses).
- **Recomposer** (`pkg/recomposer`) — `Recompose(ctx, RecomposeSpec, []ReturnResultPayload) (ReturnResultPayload, error)`. Two implementations: `Concat` joins `Result.Content` with a caller-supplied `Separator` (`DefaultSeparator = " | "` suggested) and is the text-level v0 default; `Synth` plugs an LLM-shaped `Synthesizer` interface into the same fold machinery so reductions go through a real model rather than string-joining. Both honor the same spec semantics: `RecomposeNone` returns the zero payload, `RecomposeSequential` folds left-to-right, `RecomposePairwise` folds pairwise across rounds (odd-count rounds pass the trailing element through) — N-1 reductions in both shapes. Both take weakest-link confidence (`min`) and union signal bundles (Contradictions/OpenQuestions concat, GroundedPass AND when both set, nil otherwise). `Synth` invokes its synthesizer once per binary reduction with `{Left, Right, RecomposeSpec, StepIndex}`; the response may override confidence (zero falls back to weakest-link). Two Synthesizer implementations: `SynthStub` (tests/demo, configurable format) and `AnthropicSynthesizer` (frontier-backed; default model `claude-sonnet-4-6`, parses `{content, confidence}` from the Messages API). Compose-as-a-dispatched-AP with actual scope channels is a v1 concern; v0 owns the orchestration in the runner+recomposer pair.
- **Demo** (`cmd/oscillitron`) — exercises the uniform-node + evaluate/execute call tree end-to-end. Without `--hermes` or `hermes.url` in config, it uses the stub adapter (root plan emits three process APs + one critique, all three Execute categories demonstrated, Concat recomposer with `DefaultSeparator`). With `--hermes <URL>` (or `hermes.url=<URL>` in `--config`), it swaps in `hermes.SingleEndpoint`. If `--config` provides any `hermes.endpoints.<playbook>.url`, multi-endpoint mode wins (requires all five playbook endpoints). CLI flags: `--task`, `--seed` (0 = non-deterministic), `--max-depth`, `--depth-budget`, `--config`, `--hermes`, `--hermes-model`, `--strict`, `-v` (slog Info events). Concurrency + VRAM flags (all optional — library auto-manages by default): `--max-concurrency` (0 = library-managed, 1 = strict serial, N>1 = static cap), `--max-concurrency-ceiling` (hard safety cap on auto-derived cap), `--vram-budget` (operator override with KB/MB/GB suffix support; bypasses platform auto-detection), `--model-context-size`, `--prefix-tokens`, `--bytes-per-token`, `--max-input-bytes`.
- **Properties config** (`pkg/config`) — tiny stdlib-only `.properties` loader (Java-style `key=value`, `#` / `!` comments, dotted keys for hierarchy, typed accessors). Used by the demo for both single-endpoint (`hermes.url`) and multi-endpoint (`hermes.endpoints.<bf>.url`) Hermes setups. Deliberately a fraction of Spring Boot's surface — no profiles, no relaxed binding, no SpEL.
- **Cost tracker** (`pkg/cost`) — `Pricing` + `Tracker` with actual + frontier-counterfactual ledgers. Wired through both the Hermes adapter (per-phase `Record` calls populate `env.Trace.CostUSD` and `env.Trace.CostVsFrontierBaselineUSD` inline so the lean envelope carries cost) and the runner (`Config.Cost` is an optional observer; `Result.State.CostSummary` is snapshotted at end of Run, including on the error path).
- **VRAM probe + estimator** (`pkg/vram`) — multi-platform GPU memory probe and per-session VRAM estimator powering the runner's dynamic concurrency cap. `Probe` interface; auto-detecting `AutoChain` tries probes in priority order: operator override → nvidia-smi → rocm-smi → darwin-unified (build tag) → Linux DRM sysfs (build tag) → /proc/meminfo. `SlidingWindowEstimator` caps per-session bytes at `min(prefix + observed, ContextSize) × BytesPerToken + ModelResidentBytes`; `PrefixCacheGlobal` subtracts the prefix when the engine deduplicates it across sessions. Honest about gaps (Windows + non-NVIDIA has no auto-detect path); see `references/vram-platform-coverage.md`. Probes register themselves via init() in their platform files so adding a new platform is one file change.
- **Eval harness** (`pkg/eval`) — decoupled from the orchestrator (Runner is `func(ctx, Task) (string, error)`); no changes needed for the call-tree refactor.
- **Trace** (`pkg/trace`) — slog-backed `Tracer` with `Info` / `Error` sugar helpers and a `Discard` no-op. Oscillator, runner, and the demo now emit through `trace.Tracer` rather than `*slog.Logger` directly. The fat learning-loop trace record (verifier feedback, retrieval refs, etc.) lives here per the lean-AP-vs-fat-trace split.
- **Verifier policy** (`pkg/verifier`) — implements the locked-2026-05-20 phase ramp. `Policy.ShouldCritique(action, parentOverride, rand)` returns whether to emit a critique on a return_result. Bootstrap (`invocations < BootstrapThreshold`) → 1.0; steady-state → `max(floor, 1 - happiness_wilson_lower_bound)` over a sliding ring window of judge-sampling agreements. `HappinessScope ∈ {global, per_action}` is runtime-configurable; telemetry is populated for both regardless of which drives the rate. Wilson lower bound uses Acklam's inverse normal CDF approximation (no external math libs). Parent override (envelope.NeedsVerification) forces critique on top of the baseline; suppression by parent is not allowed. v0 defaults via `DefaultConfig()` mirror the lock (10k bootstrap, 15% floor, 2k window, 95% CI, global scope). `RecordJudgeAgreement(action, agreed)` is the entry point the judge layer uses to feed happiness.
- **Judge sampling** (`pkg/judge`) — the audit tier that feeds the verifier policy's happiness signal (locked 2026-05-19; 100% un-grounded, 10% grounded). `Judge` interface produces a frontier verdict on a `Request{Target, LocalVerdict, LocalIssues}`. `Sampler.ShouldJudge(target, rand)` reads `target.Execute.ReturnResult.Signals.GroundedPass` to decide which tier applies (nil → un-grounded → 100%; non-nil → grounded → 10%). `DefaultSamplePolicy()` returns the locked rates. Two implementations ship: `Stub` (tests + demo, configurable verdicts) and `AnthropicJudge` (frontier-backed; calls the Anthropic Messages API with a structured-output prompt, parses `{verdict, issues}`, tolerates markdown-fenced JSON). Built on `pkg/anthropic`.
- **Anthropic API client** (`pkg/anthropic`) — stdlib-only client for the Anthropic Messages API. POST `/v1/messages` with `x-api-key` + `anthropic-version` headers. Single `Messages(ctx, req)` method; streaming and tool-use are out of scope for the audit / synthesis use cases. Shared by `pkg/judge.AnthropicJudge` and `pkg/recomposer.AnthropicSynthesizer`.
- **Semantic pool** (`pkg/semanticpool`) — shared-knowledge store per the parent CLAUDE.md "Per-instance vs. shared resources" lock. `Pool` interface (`All`, `Get`); `FilePool` is JSON-backed with mtime-based reload; `Static` is in-memory for tests. v0 read shape: whole pool injected as a stable preamble. `RenderPreamble` produces a byte-stable text block (`[semantic-pool: N entries]\n- id: content\n...\n[/semantic-pool]`) — deterministic order means KV-cache hits compound. Soft budget: 2000 tokens / 8 KB; over-budget snapshots emit a warning trace via the adapter. Programmatic writes deferred to the curation layer.
- **Runner verifier + judge integration** — `Config.VerifierPolicy *verifier.Policy` is optional. After each return_result child of an emit_subtree plan resolves cleanly, the runner consults the policy and, if it says yes (or the child's NeedsVerification is set), injects a critique AP into the same scope. The critique's verifier_signal flows into `RunState.VerifierSignals` via the existing category branch; the recomposer never sees it. `RunState.PolicyCritiquesEmitted` distinguishes policy-injected critiques from adapter-emitted ones. When `Config.Judge` is also wired (with an optional `Config.JudgeSampler`), the runner samples per the locked rates after each critique resolves, calls the frontier Judge for an independent verdict, compares, and calls `Policy.RecordJudgeAgreement(action, agreed)`. Judge errors are non-fatal — recorded in `RunState.JudgeErrors` but never fail the run. New counters: `JudgeSamplesTaken`, `JudgeAgreements`, `JudgeDisagreements`, `JudgeErrors`.

**Deleted (no longer relevant under the uniform-node model):**
- `pkg/oscillator` — uniform-node lock kills the brain-function-typed wrapper. One adapter handles every AP; the playbook is *picked* by evaluate, not declared.
- `pkg/registry` — no `BrainFunction → instance` mapping; one adapter does it all.
- `pkg/topology`, `pkg/router`, `pkg/router/rule`, `pkg/decomposer` — already gone under the earlier call-tree refactor.

What's deliberately NOT here yet:

- Multi-instance Hermes against *real* Hermes processes — the multi-endpoint plumbing (`MultiEndpoint` builder, demo CLI properties wiring, httptest smoke test) is in place, but a smoke test against N actually-running `hermes gateway` processes on distinct ports has not been done yet. Defer until a real reason (per-playbook drift visibility) shows up.
- Anthropic API quotas / retries / rate-limit handling — `pkg/anthropic.Messages` calls the API with a basic timeout. Retries, backoff, and 429 handling are caller-side concerns and not built in (the runner already treats Judge errors as non-fatal, which is the relevant resilience).
- Critique-on-recomposed-bubble — the runner only injects critiques after return_result *children*. A plan's recomposed bubble is not critiqued (would require a v1 compose-as-AP rework).
- Approval handling — `/v1/runs/{id}/approval`. The adapter rejects approval.request events as inhibited; auto-approval / human-in-the-loop is a later PR.
- Real grader implementations beyond substring — LLM-as-judge and rules-DSL graders are seam-reserved but not built.
- **Curation layer** — programmatic writes to the semantic pool, recipe consolidation, exemplar promotion. v0 ships read-only semantic pool with operator-curated JSON; the curation layer that gates writes is the next substrate piece. Also leaves tree-merge-with-conflict-resolution as a separate Recomposer variant for future work.
- Compose-as-a-dispatched-AP with actual scope channels — v0 owns recomposition in the runner+recomposer pair; the compose playbook category exists at the adapter level but the call-tree orchestration is deferred.
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
# Strict mode — error on any non-JSON response from the substrate:
go run ./cmd/oscillitron --hermes http://127.0.0.1:8642 --strict

# Or commit settings to a file and reuse:
cp cmd/oscillitron/oscillitron.properties.example oscillitron.properties
# (uncomment and edit the hermes.url / hermes.model lines, then:)
go run ./cmd/oscillitron --config oscillitron.properties
# CLI flags still win; passing --hermes "" forces back to stub mode.
```

`--hermes` uses `hermes.SingleEndpoint` (one Hermes for every playbook — the v0 dev shape). For the multi-endpoint case (one Hermes per playbook action — the locked per-instance playbook substrate design), pass a `--config` properties file that sets `hermes.endpoints.evaluate.url` plus one entry per playbook; the demo constructs `hermes.MultiEndpoint` automatically. Programmatic callers can call `hermes.MultiEndpoint(evaluate, byPlaybook)` directly.

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
