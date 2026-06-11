<!-- CLAUDE GENERATED -->

# Oscillitron — Go module

This directory is the Go module for Oscillitron. The project README — purpose, architecture overview, benchmark findings, quickstart — is one level up: [`../README.md`](../README.md). The detailed package inventory and code conventions live in [`CLAUDE.md`](CLAUDE.md).

## Build, test, run

```bash
go build ./...
go test -race ./...
go run ./cmd/oscillitron          # offline demo (stub adapter, no keys or models needed)
```

Requires Go 1.26+.

## Layout

```
cmd/oscillitron/      demo runner — full call tree against stub or Hermes substrate
cmd/bench/            benchmark driver (GPQA Diamond, MMLU-Pro, MATH-500, GSM8K, ARC, ...)
cmd/curate/           cold-path exemplar curation from bench output streams
cmd/phase1/           email-drafting quality gate (orchestrator vs frontier baseline)
pkg/session/          AP envelope schema (evaluate/execute, call-tree plumbing)
pkg/adapter/          Adapter contract + substrates: hermes, ollama, vllm, lmstudio, anthropic, stub, minimal, curated
pkg/runner/           recursive call-tree walker with concurrent sibling dispatch
pkg/recomposer/       result folding (concat or LLM-synthesized)
pkg/inhibitor/        edge circuit-breakers (hardcap, confidence, repetition, contradictions, composite)
pkg/vram/             multi-platform VRAM probe, KV-cache estimator, process-wide governor
pkg/cope/             Ship / ShipWithCaveat / Escalate / Refuse rule-table dispatcher
pkg/notice/           prompt- and response-side inadequacy detectors, confidence extraction
pkg/stakes/           effort-routing axis (attempt scaling per stakes level)
pkg/verifier/         critique-sampling phase ramp (Wilson-bound happiness signal)
pkg/judge/            frontier-audit sampling layer
pkg/benchmark/        harness: loaders, orchestrators (Single/Vote/Tree/Coping), graders, calibration, categorization
pkg/curation/         cold-path exemplar mining
pkg/exemplar/         per-action BM25-ranked exemplar store
pkg/trace/            slog tracing, correlation IDs, OTel export, multi-backend fan-out
pkg/cost/             cost ledgers with frontier-counterfactual accounting
```

### Pre-commit hooks

```bash
../scripts/install-hooks.sh   # from repo root: sets core.hooksPath = scripts/git-hooks/ (gofmt + go vet)
```
