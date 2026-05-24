# Oscillitron

Production-grade LLM handling at a fraction of the cost. A neural-ensemble runtime where weak/cheap base models are wrapped as "oscillators" coordinated through "action potentials" (spike-like events), with specialization growing organically over time inside scaffolded seed niches.

**Stage:** scaffolding (Phase 2 thin skeleton).

Design, architecture, and the library plan live in the parent knowledge-work project at `../`. Start there: [`../CLAUDE.md`](../CLAUDE.md), then [`../scratch/library-plan.md`](../scratch/library-plan.md).

## Quick start

```bash
go build ./...
go test ./...
go run ./cmd/oscillitron
```

Requires Go 1.21+.

The demo wires a 3-oscillator topology (router → code-analyst → writer), fires one AP, and logs every hop until the inhibitor's hard cap fires or the AP reaches a sink.

## Layout

```
cmd/oscillitron/         demo runner
pkg/session/             AP / Session Envelope schema (library-plan §5.1)
pkg/classification/      data classification levels
pkg/adapter/             Adapter interface + stub impl
pkg/oscillator/          specialist wrapper (adapter + ID + channel)
pkg/router/              Router interface + rule.Router
pkg/topology/            directed graph of oscillator edges
pkg/inhibitor/           Inhibitor interface + hardcap.Inhibitor
```

## Status

See [`CLAUDE.md`](CLAUDE.md) for what's implemented, what's deliberately deferred, and the open placeholders (module path, license, subproject-vs-sibling-repo).
