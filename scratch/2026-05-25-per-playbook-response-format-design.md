# Per-playbook ResponseFormat — design spec

**Date:** 2026-05-25
**Problem:** `Config.ResponseFormat` is a single schema (`{response, confidence}`) applied to every `oneCall`, including Plan Execute calls that need `{sub_aps, recompose}`. Engine-level JSON constraint makes it impossible for the model to produce the plan shape. Result: Tree arm errored 198/198 cases with `plan returned unknown recompose spec ""`.

**Fix:** per-playbook Execute response_format schemas.

## Changes

### 1. `pkg/adapter/minimal` — schema factories

New exports alongside existing `ProcessSchema()`:

- `PlanSchema()` — `{sub_aps: [{input_kind, input, output_schema, classification, needs_verification}], recompose: enum(pairwise|sequential|none)}`
- `CritiqueSchema()` — `{verdict: enum(pass|fail|issues), issues: [{severity, where, what}]}`
- `VerifyGroundedSchema()` — same shape as CritiqueSchema
- `ComposeSchema()` — same shape as ProcessSchema (`{response, confidence}`)
- `AllPlaybookFormats()` — returns `map[session.Playbook]map[string]any` pre-wrapped with `AsResponseFormat`

The recompose field uses `"enum": ["pairwise", "sequential", "none"]` so the engine forces a valid value — eliminates the empty-string bug at the constraint layer.

### 2. All 4 OpenAI-compat adapters (ollama, vllm, lmstudio, hermes)

Each adapter:
- Adds `Config.ExecuteResponseFormats map[session.Playbook]map[string]any`
- Adds a `responseFormat` parameter to `oneCall` (currently reads `a.cfg.ResponseFormat` unconditionally)
- `Evaluate` passes `nil` to `oneCall` (no schema enforcement — prompt-only)
- `Execute` looks up `cfg.ExecuteResponseFormats[playbook]`, falls back to `cfg.ResponseFormat` if missing

Fallback preserves backward compat: callers that only set the global `ResponseFormat` still work for process-only workloads.

### 3. `cmd/bench` — wiring

`buildAdapter` calls `minimal.AllPlaybookFormats()` when `--structured-output` is true and sets `cfg.ExecuteResponseFormats` on each OpenAI-compat adapter config.

### What does NOT change

- Instruction prompts (already describe the right JSON shapes per playbook)
- Parsers (`parseEmitSubtreeJSON`, `parseReturnResultJSON`, `parseVerifierSignalJSON`)
- `forcePlanOnRoot` wrapper in Tree orchestrator
- Evaluate path (stays prompt-only, no schema enforcement)
- Hermes adapter (`/v1/runs` surface ignores `response_format`; field is set but harmless)

## Verification

- Existing tests pass (`go test -race ./...`)
- New unit tests in `pkg/adapter/minimal` for each schema factory
- Re-run a small Tree bench (~10 cases) on llama3.1 to confirm Plan calls produce valid `{sub_aps, recompose}` with a non-empty recompose spec
