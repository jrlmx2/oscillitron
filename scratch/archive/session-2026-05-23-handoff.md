<!-- CLAUDE GENERATED -->
# Session handoff — 2026-05-23

A new session can pick up the work from this file alone. Critical paths are inline.

## TL;DR

v3.0–v3.5 all merged to `main`. The full v3 chain (notice → cope) is operational end-to-end on `qwen2.5:7b-instruct-q6_K` via ollama-direct. **Two benches still running in background as of handoff time** — pick up by tailing the JSONL files (paths below). Code-simplifier landed 4 small wins (in this PR's diff). Code review found 8 candidate bugs, 3 cross-confirmed; recommended next-PR bundle described below.

## What's in flight right now

| Process | PID file | Logs | Output |
|---|---|---|---|
| qwen2.5:7b Diamond bench (198 cases, full v3.5 stack) | `/tmp/v35-final.pid` | `/tmp/v35-final.log` | `/tmp/v35-final.jsonl`, `/tmp/v35-final-report.json` |
| Watcher (auto-launches phi when qwen exits) | `/tmp/v35-watcher.pid` | `/tmp/v35-watcher.log` | — |
| phi4-mini Diamond bench (198 cases, same config — queued, not yet started) | `/tmp/v35-phi.pid` (created when launched) | `/tmp/v35-phi.log` | `/tmp/v35-phi.jsonl`, `/tmp/v35-phi-report.json` |

**Resume sequence**:

```bash
# 1. Is qwen bench done?
ps -p $(cat /tmp/v35-final.pid) -o pid,etime 2>&1 | head -2
wc -l /tmp/v35-final.jsonl

# 2. Did phi auto-launch?
cat /tmp/v35-watcher.log
ls /tmp/v35-phi.pid 2>&1 && ps -p $(cat /tmp/v35-phi.pid) -o pid,etime

# 3. Pull final aggregates + calibration tables once each finishes:
grep -A 6 '^--- Aggregate ---' /tmp/v35-final.log
grep -A 10 'Confidence calibration' /tmp/v35-final.log
# Same for /tmp/v35-phi.log when phi finishes.
```

**Both benches use the full v3.5 stack**: `--vote-n 5 --stakes rotate --cope --notice --minimal-output --structured-output`. Orchestrator and frontier are the SAME model (self-escalation pattern). Substrate = ollama-direct.

## What just merged (last 14 commits on main)

| # | Title | Phase |
|---|---|---|
| #48 | v3.0: stakes-driven attempt scaling | v3.0 |
| #49 | v3.1: notice layer — prompt-side signals | v3.1 |
| #50 | v3.2: notice — response-side + confidence extraction | v3.2 |
| #51 | hygiene: gofmt + git hooks | infra |
| #52 | v3.3: confidence surfaced + calibration table | v3.3 |
| #53 | v3.4: cope rule-table dispatcher + frontier escalation | v3.4 |
| #54 | QoL + confidence stub fix | hygiene + critical bugfix |
| #55 | v3.5: structured-output schema enforcement (response_format) | v3.5 |
| #56 | v3.5 follow-up: percent-normalize confidence values | v3.5 fix |
| (#47 earlier) | v3 design doc | docs |

**Critical: PR #55 merged before the percent-normalize amend landed.** PR #56 closed that gap. Both are on main now.

## v3 architecture state

```
Bench case (with stakes annotation)
  ↓
Vote orchestrator runs N attempts on substrate (effective N from stakes.AttemptScale)
  ↓
Each attempt:
  • PRE-CALL: notice prompt-side detectors                    [v3.1]
  • CALL: POST /v1/chat/completions with response_format     [v3.5]
    → model emits {answer: string, confidence: 0..1}         [forced by schema]
  • POST-CALL: notice response-side detectors                [v3.2]
  • parseReturnResultJSON → NormalizeConfidence (percent → decimal) [v3.5]
  • Confidence stamped on env.Execute.ReturnResult           [v3.3]
  ↓
Vote aggregates: mean confidence across successful attempts
  ↓
Coping dispatcher reads (confidence, stakes) → Action       [v3.4]
  • Ship           → return inner
  • ShipWithCaveat → return inner tagged
  • Escalate       → re-run on Frontier, pool tokens/calls
  • Refuse         → return empty (no frontier wired)
  ↓
Bench report: aggregate / categorization / calibration / sliding window
```

## Code-simplifier landed (in THIS PR's diff)

Four small wins (the modifications I'm carrying forward as part of this handoff PR):

1. `oscillitron/pkg/benchmark/calibration/calibration.go` — dropped no-op `sort.SliceStable` (less always returned false); removed unused `sort` import.
2. `oscillitron/cmd/bench/main.go` — removed dead `_ = props // future expansion`.
3. `oscillitron/pkg/cope/cope_test.go` — replaced hand-rolled `contains` helper with `strings.Contains`.
4. `oscillitron/pkg/benchmark/orchestrator/vote.go` — Go 1.22+ idioms: `for i := range effectiveN`, `strconv.Itoa(i)` over `fmt.Sprintf("%d", i)`.

All four committed in this PR. `go test -race ./...` green across 40 packages.

## Code review findings (8 candidates, ranked by severity)

Full details in conversation history. **Cross-confirmed by multiple angles** = high confidence.

| # | File | Bug | Severity | Cross-confirmed |
|---|---|---|---|---|
| 1 | `coping.go:77` | Partial-zero RuleTable bypasses default-fill (AND should be OR) | **HIGH** | ✅ Angles A + C + simplifier |
| 2 | `vote.go:183` | Empty-extraction attempts still aggregate into Answer.Confidence | **HIGH** | Angle A |
| 3 | `response.go:167` | `confidenceRE` matches inside "overconfidence" → extracts 5 → 0.05 | MED | Angle A |
| 4 | hermes/vllm/lmstudio `structured.go` | Only ollama wires `EffectiveConfidenceFromRaw`; others lose raw-text confidence | MED | Angle B (multi-substrate) |
| 5 | `minimal.go:58` | ProcessInstructions removed closing-position discipline ("End your response with…") | MED | Angle B |
| 6 | `coping.go:127` | Cost double-counting on Escalate when inner-rate ≠ frontier-rate | MED | Angle C |
| 7 | `calibration.go:150` | Custom bands with Hi=1.0 silently drop exact-1.0 cases (use Hi=1.01 trick) | LOW | Angle A |
| 8 | `coping.go:113` | Escalate path doesn't validate frontier returned non-empty Extracted | LOW | Angle C |

**Recommended next-PR bundle (high-confidence, small, unambiguous):**

- #1 (partial RuleTable) — fix the AND→OR, or use a `Configured bool` sentinel
- #2 (empty-extraction aggregation) — move the `if r.confidence > 0` check inside the `extracted != ""` block in `vote.go`
- #3 (regex word boundary) — change `confidenceRE` to `(?i)\bconfidence\s*[:=]\s*...` or anchor with `(?:^|\s)`
- #5 (minimal.go closing-position) — restore "End your response with..." in `ProcessInstructions`

Larger / discussion-worthy fixes (separate PRs):
- #4 (multi-substrate parity for raw-text confidence) — needs design pass
- #6 (cost double-counting) — needs operator-facing decision

## Wider architectural threads still open

- **v4 — user-feedback intake.** The keystone for calibrated learning over time. Design lives in `scratch/v3-design.md` §2.5. Not started.
- **qwen2.5:7b overconfidence finding.** From the 3-case smoke earlier today: high-band (0.96 mean conf) passed only 42.9% on GPQA hard science. A calibration-correction layer that learns "trust this model's confidence less on Physics" would be v4.x territory. Worth measuring at 198-case scale once the bench completes.
- **Hermes adapter parity.** Hermes uses `/v1/runs`, no `response_format` surface. If we want format enforcement on Hermes, the path is either (a) Hermes-server-side soul.md tweak, or (b) accept Hermes is a separate path with text-only enforcement.

## Files for resuming work

- `scratch/v3-design.md` — v3 architecture lock, includes deferred-to-v4 list (§8)
- `oscillitron/CLAUDE.md` — current package inventory; updated for each v3 phase
- `CLAUDE.md` (root) — project status (v3 complete, 2026-05-23)
- `references/substrate-routing.md` — auto-routing rule (small→ollama, larger→hermes)
- Smoke results from today (3-case test in conversation history): qwen2.5:7b GPQA Diamond 10-case smoke showed 50% frontier vs 30% cope-vote-3 — calibration inverts on hard science. Confirms v3.5 plumbing works; substrate-quality is the limiting factor.

## Open PRs at handoff time

- This PR (claude/session-handoff-2026-05-23) — pending: includes 4 simplifier wins + this handoff doc

## What NOT to do at session start

- Don't kill the bench processes — they're running for a reason
- Don't push to origin/main directly — workflow lock is "branch from main, one PR at a time"
- Don't merge this handoff PR if benches still running — let them finish first so the next session can pull final numbers

## Quick-resume one-liner

```bash
ps -p $(cat /tmp/v35-final.pid) -o pid,etime; wc -l /tmp/v35-final.jsonl; ls /tmp/v35-phi.pid 2>&1; tail -5 /tmp/v35-watcher.log
```

That tells you in 4 lines: is qwen still running, how far along, did phi launch, what the watcher logged.
