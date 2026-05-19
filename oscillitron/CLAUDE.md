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
- Cost tracker (`pkg/cost`) — `Pricing` + `Tracker` with parallel actual + frontier-counterfactual ledgers. Goroutine-safe. **Wired into runner via optional `Config.Tracker`**: the runner stamps `Trace.CostUSD` and `Trace.CostVsFrontierBaselineUSD` on every emission. Adapters report token usage via new `Outcome.TokensInput`/`Outcome.TokensOutput`; oscillator copies them into `Envelope.Trace` before emit. The Hermes adapter parses ACP `usage_update` notifications. Demo runner registers Claude Sonnet 4.6 pricing as the frontier counterfactual when the Hermes path runs.
- Eval harness (`pkg/eval`) — `Grader` interface, substring-match stub grader, and a `Run` function that drives a workload through a caller-supplied `Runner` (so the three Phase 1 comparison arms in library-plan §9 step 11 are substitutable).
- Decomposer (`pkg/decomposer`) — `Decomposer` interface + `Passthrough` no-op impl. Manual-workflow and LLM-driven impls deferred to subpackages.
- Recomposer (`pkg/recomposer`) — `Recomposer` interface + `Concat` impl, plus `pkg/recomposer/tree` (pairwise tree-reduce with a `PairMerger` seam). v0 `DeterministicPairMerger` combines verdicts with bracketed labels, applies ±10% confidence shift on exact-match agreement/disagreement, unions side fields, records disagreements as Contradictions, sums token usage. LLM-mediated `PairMerger` drops in later without touching the tree shape.
- Trace (`pkg/trace`) — `Tracer` interface with an slog-backed default. **Migrated:** oscillator/runner/cmd now emit through `Tracer`. Events are namespaced (`oscillator.emitted`, `oscillator.adapter_error`, `runner.routing`, `runner.inhibitor_abort`, `runner.router_terminal`, `runner.unwired_destination`). `Config.Logger` still accepted as a convenience (auto-wrapped in `trace.Slog`); `Config.Tracer` takes precedence. Langfuse / OTel backends drop into the existing seam without touching emit sites.
- Pool adapter (`pkg/adapter/pool`) — wraps N backing Adapters and round-robins (or any other `Strategy`) concurrent `Call`s across them. Lock-free dispatch via an atomic counter. The right tool for stateless throughput-bound workloads with a beefy backing model; the **wrong** tool for stateful specialist learning (skill/memory updates fan across replicas, breaking the per-region "single learning thread" invariant locked in `../CLAUDE.md`). Documented in package doc with explicit do/don't.
  - **Parallel kill switch**: `Adapter.SetParallel(false)` pins all dispatch to `backends[0]`, restoring single-learning-thread semantics at runtime. Other backends sit dormant — no error path, no callsite changes. Externalized via the standard pattern (`pool.PoolConfig.Parallel`, `OSCILLITRON_POOL_PARALLEL`, `--pool-parallel`). Accepts true/false/yes/no/on/off/1/0. Use as the escape hatch when parallel divergence (skill drift, non-determinism, a flaky backend) becomes a problem.
- **Externalized config pattern** (Spring-Boot precedence: defaults < env < flags < programmatic) applied across three packages so far. Each package exposes `DefaultX`, `ApplyEnv(X) (X, error)`, `RegisterFlags(fs, *X)`, and a one-shot `LoadX(fs, args)` convenience. Env/flag names exported as constants. Stdlib `flag` only.
  - `pkg/adapter/hermes/config_load.go` — `Config` (Name, BinPath, Cwd, Args, MaxContextTokens). Env prefix `OSCILLITRON_HERMES_*`, flag prefix `--hermes-*`.
  - `pkg/runner/config_load.go` — `Tunables` (BufferSize, ChainTimeout). Env prefix `OSCILLITRON_RUNNER_*`, flag prefix `--runner-*`. ChainTimeout wraps the run ctx with a deadline.
  - `pkg/cost/config_load.go` — `PricingConfig` (Frontier + Models map). Per-model table loaded as JSON to avoid env-var explosion. Default frontier = Claude Sonnet 4.6 list price ($3/$15 per MTok). Env prefix `OSCILLITRON_COST_*`, flag prefix `--cost-*`. `NewTrackerFromConfig` builds a `Tracker` directly from a resolved `PricingConfig`.
  - **Shared-FlagSet footgun documented in every loader**: don't call `LoadX(fs, nil)` intending to defer parsing — flag pointers dangle into the stack frame. For multi-package flag sharing, caller owns the value and calls `ApplyEnv` + `RegisterFlags` directly, then one `fs.Parse(args)` at the bottom. `cmd/oscillitron/main.go` is the canonical example.
- Runner timeout cross-validation — at startup `runner.Run` walks the oscillator map; any adapter implementing the optional `adapter.MinTimeoutAdvisory` interface (`MinCallTimeout() time.Duration`) is checked against `Config.ChainTimeout`. If the chain timeout is shorter than an adapter's declared floor, an advisory event (`runner.chain_timeout_below_adapter_floor`) is emitted via the tracer naming the offending oscillator. Not a hard fail — the per-Call floor on each adapter is the actual guard. `hermes.Adapter.MinCallTimeout()` implements the advisory.
- Interactive REPL (`cmd/oscillitron --interactive`) — reads prompts line-by-line from stdin, runs each through the configured topology, prints verdict + cost summary. EOF or `quit`/`exit` exits. Works with stub topology (`oscillitron[stub]> `) or single-node Hermes topology (`oscillitron> `). The Hermes path reuses ONE persistent process across prompts; the topology and oscillator goroutines are rebuilt per prompt because `runner.Run` consumes them. Toggle via `--interactive` flag or `OSCILLITRON_INTERACTIVE=true`.
- Hermes timeout floors (`pkg/adapter/hermes`) — `MinConnectionTimeout` (default 30s) and `MinCallTimeout` (default 30s) refuse setup or per-Call when the caller's ctx deadline is shorter than the floor. Prevents the "fire prompt → cancel mid-stream → opaque 'client closed' error" failure mode by failing fast with clear messages (`hermes: setup ctx has X remaining; need at least Y for handshake` at `New()`, `ExitInhibited` + signal `prompt_timeout_too_short` at `Call`). Externalized via `--hermes-min-connection-timeout` / `--hermes-min-call-timeout` / matching env vars. Set to a negative value to disable.
- Hermes adapter (`pkg/adapter/hermes`) — **end-to-end validated against a real Hermes ACP server (2026-05-18).** Supervises one long-lived Hermes process per oscillator (spawned under `context.Background()` so it survives the caller's setup ctx). Speaks ACP over stdio via the minimal Go client in `pkg/adapter/hermes/acp` (initialize / session/new / session/prompt + session/update notifications). One persistent ACP session per Adapter; per-region `sync.Mutex` serializes APs so Hermes' skill/memory updates accrue deterministically (parent `../CLAUDE.md` lock 2026-05-18). AP → single text ContentBlock; assistant chunks → `Outcome.Verdict`. Stop reasons map to `ExitDone`/`ExitInhibited`. Structured output via prompt-engineered trailing fenced JSON block (`pkg/adapter/hermes/output.go`): every prompt includes a format instruction; the adapter parses the last ` ```json ` block and maps `confidence`/`signals`/`open_questions`/`contradictions` into `Outcome`. Verdict falls back to raw output if the model jammed everything into the JSON block. **Wire framing: newline-delimited JSON-RPC 2.0 — confirmed by live round-trip.** **Token counts: client-side estimate (chars/4)** — ACP's `usage_update` carries `{size, used}` (context-bar metering), not per-turn input/output counts. Hermes knows the real counts internally but doesn't surface them through ACP. **`Config.MaxContextTokens` enforces a client-side hard ceiling**: if a prompt's chars/4 estimate exceeds it, `Call` short-circuits with `ExitInhibited` + signal `prompt_exceeds_max_context` rather than letting Hermes silently truncate via its compression pipeline. Zero disables; set to the model's native window minus a margin for system-prompt + scaffolds.
- Hermes integration test (`internal/test/hermes`) — gated on `OSCILLITRON_HERMES_BIN`. Skipped in CI; run manually once a Hermes ACP-server binary is on the box.
- Demo runner (`cmd/oscillitron`) — dual-mode: default stub demo as before; when `OSCILLITRON_HERMES_BIN` is set, runs a single-node `code` topology backed by the real Hermes adapter (matches the Phase 2 seed-list lock).

Runner currently downgrades inhibitor `Restart` to `Abort` (logged with an annotated reason) because checkpointing isn't built yet. Replace with a real restart path when checkpointing lands.

What's deliberately NOT here yet:

- Hermes-side seeding for the `code` specialist — user-side configuration, not Oscillitron code.
- Setup scripts (`scripts/`):
  - `setup-hermes-local.sh` — installs the Hermes ACP server (clones repo, runs setup-hermes.sh non-interactively, writes the `hermes-acp` wrapper). OS-agnostic.
  - `setup-hermes-backend.sh` — installs and configures a local model backend (Ollama / LM Studio / vLLM / custom OpenAI-compatible). Detects OS, RAM, GPU. RAM-aware model defaults (7B/14B/32B based on detected RAM at Hermes' 64K floor). For Ollama, builds an extended-context Modelfile-derived tag (e.g. `qwen2.5-coder-7b-instruct-q6_K-osc`) with `PARAMETER num_ctx 65536` so the backend ACTUALLY serves 64K instead of Hermes lying about it. Writes Hermes config across main + all auxiliary subsystems (compression / summarization / memory) with the same context_length. Regenerates the `hermes-acp` wrapper with backend-appropriate env vars.
- Hermes-side config gotchas learned during first integration (now handled by `setup-hermes-backend.sh` so future setups are turnkey): (a) `model.provider: ollama` in config.yaml is silently dropped — must use `provider: custom` with the Ollama base_url; (b) `OPENAI_BASE_URL` / `OPENAI_API_KEY` env vars are required even though config has them (Hermes' auxiliary-client router reads env, not config, for fallback resolution); (c) Hermes enforces ≥64K context window on the main model AND each auxiliary (compression / summarization / memory); (d) on RAM-constrained hardware (≤18 GB unified), the 64K floor pushes 14B Q6 models into swap — use 7B Q6 instead; the setup script auto-picks based on detected RAM; (e) first inference cold-starts the model (~30–60s) — keep adapter call ctx generous, never tied to short setup ctx.
- Cost tracker wired into the runner — package is ready (`pkg/cost`); wiring lands with the real adapter so token counts come from somewhere real.
- Real grader implementations beyond substring — LLM-as-judge and rules-DSL graders are seam-reserved but not built.
- Manual / LLM-driven decomposer impls — `Passthrough` is the only impl today.
- Anything compliance-shaped (audit ledger, manifest, classification routing) — Phase 4.

## Build, run, test

```
go build ./...
go test ./...                          # unit + concurrency tests
go test -race ./...                    # required before merging anything touching goroutines
go test -bench=. -benchmem -run=^$ ./...   # benchmarks (orchestrator-overhead floor)
go run ./cmd/oscillitron
```

The `-race` invocation is **mandatory** for any change touching `runner`, `oscillator`, `cost.Tracker`, `acp.Client`, or any Adapter — these are the goroutine-rich surfaces and the concurrency tests are written to fail loudly if a mutex or atomic gets removed.

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
