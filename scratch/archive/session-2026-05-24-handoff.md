# Session handoff — 2026-05-24

A new session can pick up the work from this file alone. Today (and
overnight from 2026-05-23) was the biggest single architectural arc of
the project so far — twelve PRs merged covering Tree orchestration,
ThinkingPolicy plumbing, JSON-output restoration, capability floor,
and three docs. Critical paths inline below.

## TL;DR

**Codebase is now where the v3.5 design always intended.** Output is
JSON (`{response, confidence}` shape, schema-enforced via
`response_format`); reasoning-capable substrates have a policy hook
(`pkg/thinking`) that's structurally correct but **upstream-blocked**
by Ollama #15392 for /v1 endpoint; Tree orchestrator + parser
tolerance + per-substrate adapters are all on main. Capability floor
documented (≥7B / ≥Q5_K_M / honors response_format) and `phi4-mini`
formally excluded.

**A 3-arm GPQA Diamond run is in flight on llama3.1:8b-q6** as of
write-time — the first clean cross-arm test of the restored
architecture. Expected ~60-90 min wall-clock; output at
`/tmp/v36-llama-3arm.{jsonl,log,report.json}`.

## What's in flight right now

| Process | PID file | Logs | Output |
|---|---|---|---|
| llama3.1 3-arm GPQA Diamond bench (198 cases, frontier + cope-vote-5 + tree) | `/tmp/v36-llama-3arm.pid` | `/tmp/v36-llama-3arm.log` | `/tmp/v36-llama-3arm.jsonl`, `/tmp/v36-llama-3arm-report.json` |

**Resume sequence**:

```bash
# 1. Is the bench done?
ps -p $(cat /tmp/v36-llama-3arm.pid) -o pid,etime 2>&1 | tail -1
wc -l /tmp/v36-llama-3arm.jsonl

# 2. Pull aggregate + categorization + calibration:
grep -A 5 '^--- Aggregate ---' /tmp/v36-llama-3arm.log
grep -A 14 'Failure categorization' /tmp/v36-llama-3arm.log
grep -A 14 'Confidence calibration' /tmp/v36-llama-3arm.log

# 3. Tree-specific signals:
grep -oE 'msg=runner\.[a-z_]+' /tmp/v36-llama-3arm.log | sort | uniq -c
grep -oE 'action=(ship|ship_with_caveat|escalate|escalate-failed|refuse)' /tmp/v36-llama-3arm.log | sort | uniq -c
```

**Flags used**: `--vote-n 5 --stakes rotate --cope --tree --tree-max-depth 10 --minimal-output --notice` against `llama3.1:8b-instruct-q6_K` (orchestrator AND frontier — self-comparison shape).

## What merged today (PRs #57–#68)

The full arc, oldest to newest:

| # | Title | Why |
|---|---|---|
| #57 | session handoff + bench-findings-2026-05-23 + simplifier cleanup | yesterday's session capture |
| #58 | v3.5 bug bundle (#1, #2, #3, #5 from code review) | fix RuleTable default-fill, vote confidence aggregation, regex word-boundary, prompt closing-position |
| #59 | §11 addendum + loader sketches for MMLU-Pro / MATH-500 | doc-only follow-up to #57 |
| #60 | MMLU-Pro loader | `pkg/benchmark/loader/mmlu_pro` (~12k cases, 10-option MCQ) |
| #61 | MATH-500 loader + BoxedAnswer grader | open-ended math support |
| #62 | wire MMLU-Pro + MATH-500 into cmd/bench dispatch | `--benchmark` flag + per-benchmark grader/extractor binding |
| #63 | remove CLAUDE GENERATED provenance markers (retire convention) | doc cleanup |
| #64 | switch process playbook to XML-tag format; kill JSON schema enforcement | **(retired by #67)** XML workaround for phi4-mini |
| #65 | Tree orchestrator (full call tree as bench arm) | `pkg/benchmark/orchestrator.Tree`; force PlaybookPlan on root; Synth recomposer |
| #66 | ThinkingPolicy plumbing + reasoning trace logging | `pkg/thinking` package; `Config.Thinking`; reasoning-trace events |
| #67 | restore JSON output structure + set capability floor | revert XML → JSON `{response, confidence}`; new `references/model-capability-floor.md` |
| #68 | docs: capture qwen3.5 /v1 think-suppression investigation | `references/reasoning-model-setup.md` updated with 7-probe matrix; `scratch/` design docs landed |

**Net architectural state after these:**

- Output protocol: JSON `{response: string, confidence: number}`, schema-enforced via `response_format` on Ollama / vLLM / LM Studio.
- Substrate roster: 5 above-floor models pulled (qwen2.5 was deleted; we have qwen3.5, llama3.1, mistral, hermes3, glm4).
- Tree orchestrator wired as `--tree` flag; uses `recomposer.AdapterSynth` with the same substrate.
- ThinkingPolicy interface + 5 stock policies; flag `--thinking off|on|by-stakes|by-playbook|substrate-default`.
- Capability floor documented at `references/model-capability-floor.md`; phi4-mini excluded.

## Key empirical findings from today

### 1. qwen3.5 reasoning suppression is broken on Ollama /v1

7-probe matrix on qwen3.5:9b — full results in
`references/reasoning-model-setup.md`. Headline:

| Endpoint | think | Reasoning length |
|---|---|---|
| `/v1` | false | 564–741 chars (every probe) |
| `/v1` | unset | 741 chars |
| `/api/chat` | false | **0 chars** |
| `/v1` (llama3.1 control) | n/a | 0 chars |

**The `think` flag is correctly emitted by our adapter but Ollama
ignores it on /v1 for reasoning models.** Upstream PR
[ollama/ollama#15392](https://github.com/ollama/ollama/pull/15392) is
fixing this. Our ThinkingPolicy code (PR #66) is structurally correct
and will become effective when that upstream lands.

**Practical implication:** reasoning-capable substrates (qwen3.x,
deepseek-r1, magistral, exaone-deep, glm-5, kimi-k2-thinking, gemma4
reasoning tags) are **out of bench scope today**. The roster defaults
to non-reasoning models.

### 2. JSON restore works cleanly on non-reasoning substrates

10-case smoke on llama3.1:8b-q6 (pre-3arm-bench):
- frontier: 30% pass, 0 format failures, 0 errors
- cope-vote-5: 40% pass, 0 format failures, 0 errors
- +10pp voting uplift; classic small-model calibration shape

The architecture works as designed when the substrate honors
response_format. No mysterious slowness, no XML weirdness, no
parser quirks.

### 3. LM Studio cross-validation deferred

LM Studio's `/v1` has no `think` parameter at all; reasoning behavior
on its surface would be uncontrollable by design. Couldn't directly
verify because qwen3.5 isn't on local LM Studio disk and pulling
6 GB just for cross-validation isn't worth it. Architectural
expectation captured in `references/reasoning-model-setup.md`.

## Architectural pieces parked

These came up in conversation and have docs/sketches but no code:

| Concept | Where | When |
|---|---|---|
| `cope.Replan` action (5th cope action — "deeper tree on same substrate" before escalating to frontier) | sketched in `scratch/design-conversation-2026-05-24.md` | follow-up to Tree |
| Library-learned one-liner injection per (substrate, task-type) | same | v4.x or v5 |
| Substrate routing (RoutingAdapter, per-task substrate selection) | same | v5 — explicitly NOT this session per operator pause |
| Embeddings as v5 learning substrate (k-NN routing, contextual bandit, embedding-keyed calibration) | same | v5 |
| Per-playbook role rules (Plan / Compose / Critique boundary prompts) | sketched in conversation, not in design doc yet | follow-up to Tree |
| v4 calibration-correction layer | `scratch/v4-design.md` (PINNED for fresh review — not in any PR) | v4 |

## What to do NOT do at session start

- **Don't kill the running bench** — `/tmp/v36-llama-3arm.pid` is the 3-arm GPQA Diamond run on llama3.1; let it finish so the next session has real cross-arm numbers.
- **Don't try to use qwen3.5:9b on /v1** — the upstream Ollama bug means 3-15-min calls per request on hard prompts. Use llama3.1 or qwen2.5 instead.
- **Don't commit `scratch/v4-design.md`** — operator-pinned for fresh review. Untouched in working tree across multiple PRs.
- **Don't merge to main directly** — workflow lock: branch from main, one PR at a time, wait for merge. Has held all session.
- **Don't restart LM Studio with the GUI** — operator stated preference. CLI (`~/.lmstudio/bin/lms`) is fine for headless work but qwen3.5 isn't on disk.
- **Don't autonomously pull more Ollama models** — disk is at 86%+; existing roster has 5 substrates and ~30 GB; ask before adding more.

## Critical files to know about

- `references/model-capability-floor.md` — the substrate-roster discipline (≥7B, ≥Q5_K_M, honors response_format)
- `references/reasoning-model-setup.md` — the qwen3.5 /v1 investigation; what works, what doesn't, what upstream is fixing
- `references/substrate-routing.md` — small-model auto-routing (predates this session; still current)
- `scratch/archive/bench-findings-2026-05-23.md` — yesterday's qwen + phi + Haiku 198-case numbers; baseline for comparing today's llama3.1 results
- `scratch/design-conversation-2026-05-24.md` — full design state across architectural threads (XML experiment, Tree, ThinkingPolicy, embeddings, substrate routing); pure design, no code commitments
- `scratch/substrate-quantization-notes.md` — Q4 vs Q6 across the roster (Hermes 3 + GLM 4 are at Q4; ~1-3pp gap)
- `scratch/v4-design.md` — pinned for fresh review; untouched; NOT in any PR

## Open PRs at handoff time

None. All session PRs (#57-#68) merged. The next PR is whichever direction
you take after reviewing the bench numbers.

## Suggested next-session priorities

In rough priority order:

1. **Pull the bench's aggregate** — does Tree on llama3.1 work end-to-end on a real 198-case load? `scratch/archive/bench-findings-2026-05-23.md` is the comparison baseline.
2. **Per-playbook role rules** — Plan / Compose / Critique boundary prompts. We discovered today that even when Tree's plumbing works, the *model* often defeats the tree structure by answering directly in the Plan call. Role rules are the prompt-level fix and the highest-leverage next architectural lever. Design discussed but no PR yet.
3. **Re-pull qwen2.5:7b** as a reference baseline. We deleted it earlier in the session; bench-findings-2026-05-23 used it heavily for cross-substrate comparison. Re-pulling gives us a known-good calibration point.
4. **Track Ollama #15392** — when it lands, reasoning substrates become benchable again and our ThinkingPolicy plumbing comes alive without code changes on our side.
5. **v4 calibration design review** — operator-pinned; comes off the pin when ready.

## Quick-resume one-liner

```bash
ps -p $(cat /tmp/v36-llama-3arm.pid) -o pid,etime 2>&1 | tail -1; wc -l /tmp/v36-llama-3arm.jsonl; tail -5 /tmp/v36-llama-3arm.log
```

Tells you in 3 lines: is the bench still running, how many cases done, what the last log entries look like.
