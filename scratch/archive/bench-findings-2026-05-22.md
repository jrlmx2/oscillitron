
# Bench findings — 2026-05-22

End-of-session summary of every bench run we did today, what we
learned, the configuration discoveries that bit us, and what's
still open. Companion to `scratch/session-2026-05-22-handoff.md`
(the pre-session-start handoff) — this one is the post-session
delta.

Sister doc to track: the v3 architectural redesign that came out
of empirical findings here. Not started yet (deferred to next
session per operator). See "Open architectural questions" below
for the framing.

---

## 1. Chronological run log

Eight bench attempts today. Five produced usable data; three
were dead ends that taught us something operationally.

| # | Substrate | Config | Outcome | What we learned |
|---|---|---|---|---|
| 1 | gemma-4-e4b via LM Studio (4k ctx) | 3 cases, vote-3, ceiling=1 | Fail: every call hit `n_keep: 14098 >= n_ctx: 4096` | Hermes' default persona injects ~14k tokens of boilerplate (toolsets + 87 skill SKILL.md files). LM Studio's default load context is too tight to fit. |
| 2 | gemma at LM Studio 16k ctx, ceiling=2 | 3 cases | Single passed 2/3; Vote ERRORED 3/3 on `Context size exceeded` | LM Studio shares the n_ctx budget across concurrent sessions. 3 parallel vote attempts × ~14k tokens each blew past the shared pool. |
| 3 | gemma at LM Studio 32k ctx, ceiling=1 | 3 cases | 67% both arms (sample too small to mean anything) | Sequential vote works. Substrate functioning. Confirmed the smoke loop. |
| 4 | gemma via LM Studio (full 198) | 198 cases, ceiling=2 | Died at case 10: LM Studio reverted to n_ctx=4k on its own (autounload?). 130+ cases errored after. ~9 salvageable. | LM Studio is unreliable for multi-hour runs — it autonomously unloads / reverts loaded-model settings. Not a viable production substrate. |
| 5 | phi4-mini via Ollama+Hermes (full 198) | 198 cases, ceiling=2 | Got to 60 cases @ ~15% pass before user killed for the suffix experiment. **Zero errors.** | Ollama+Hermes is operationally stable. Phi4-mini's pass rate is **format-broken**, not random — see findings section. |
| 6 | Haiku via Anthropic API | 30 cases | Single 50% passed; Vote-3 404'd all 30 (URL config bug — operator-side: `--orchestrator-url` default kept Hermes URL when switching to anthropic) | **Real config bug.** `cmd/bench` URL defaults aren't substrate-aware; switching `--orchestrator-substrate` to `anthropic` doesn't reset URL to empty. Should fix. |
| 7 | Haiku via Anthropic, URL fixed | 30 cases | Single 37.9% / Vote-3 43.3% / Vote tokens 3.2× single | Real orchestration lift on competent substrate (~5 points). Per-run variance was 50→38% on the Single arm across two runs — 30 cases isn't enough to bound a single run's quality with any confidence. |
| 8 | phi4-mini + `--prompt-suffix` | 198 cases, format-recipe suffix | **Currently running (task b3913eyi5).** Started ~22:50. | TBD — the experiment is whether the suffix recovers the 71% extract-empty leakage and pushes phi4-mini pass-rate from 13% toward 30-40%. |

---

## 2. Headline findings

### 2.1 Phi4-mini is not random on GPQA Diamond — it's format-broken

The single most important empirical result today. Decomposing
phi4-mini's 60 completed cases:

| Failure mode | Frontier (Single) | Vote-3 |
|---|---|---|
| Passed | 13.6% (8) | 16.7% (10) |
| **`extracted=""` (no letter committed)** | **71.2% (42)** | 43.3% (26) |
| Wrong letter | 15.3% (9) | 40.0% (24) |
| Adapter error | 1 | 0 |
| **Conditional accuracy when committed** | **47.1%** | 29.4% |

Phi4-mini reasons fine. It just doesn't end its response with a
parseable letter, so the extractor returns empty and we record
the case as fail. **47% conditional accuracy is Haiku-comparable.**

This collapses a class of hand-wringing about "phi4-mini is too
weak for GPQA." It isn't. It's mis-formatted.

### 2.2 Vote-3's alphabetical tiebreaker biases toward "A" on weak substrates

Of 24 Vote-3 wrong-letter picks on phi4-mini, **16 were "A"** —
two-thirds. The expected-answer distribution across those failures
is approximately uniform across A/B/C/D, so this isn't an artifact
of the dataset. The cause is `pkg/benchmark/orchestrator/vote.go`'s
tiebreak: "alphabetical (deterministic)". When attempts disagree
(or some come back empty), alphabetical "A" wins ties.

Real bug worth fixing in a follow-up PR. Probable fix: tiebreak
on per-attempt confidence (when surfaced by adapter) or by random
choice with a seeded PRNG.

### 2.3 Orchestration value depends on substrate competence

Two data points from today:

- **Haiku Single ~38-50% → Vote-3 43%.** Real but modest lift
  (~5 points) at 3.2× token cost.
- **Phi4-mini Single 13.6% → Vote-3 16.7%.** Within sampling
  noise on 60 cases, and Vote-3's *conditional accuracy* is
  actually *worse* (47% → 29%) because of the A-bias above.

So there's a substrate floor below which ensemble orchestration
adds noise rather than signal. Phi4-mini-on-GPQA is below it,
in part because of the tiebreaker bug, in part because there's
nothing left to vote on once empties are excluded.

### 2.4 Substrate quality + format leakage matter way more than orchestration shape

In rough order of empirical impact on phi4-mini-on-GPQA pass
rate (estimated):

1. **Format recipe (the running experiment)** — estimated lift
   from 13% toward 30-40% if extract-empty drops to near-zero.
   Single intervention. Free. *Awaiting data.*
2. **Substrate choice** — moving from phi4-mini to Haiku at the
   same prompt is empirically +25-35 points on GPQA. Costs API
   dollars; defeats the small-hardware goal.
3. **Ensemble orchestration (Vote)** — empirically ~5 points
   when substrate is real, 0 when substrate is leaky.
4. **Self-consistency tiebreaker fix** — would recover the 16
   wrong-A picks → ~5-15 points on Vote-3 specifically.

The 1-vs-3 ordering says: prompt engineering (recipes) >
ensemble orchestration. That matches the operator's pushback on
v2's exemplar direction.

---

## 3. Architectural learnings

### 3.1 The original v2.0 substrate (exemplars-as-few-shot) is probably the wrong primary channel

Reasoning chain:
- GPQA cases are unique by design (no near-duplicates between
  cases). An exemplar of "the answer to question A is C" doesn't
  teach a model anything about question B.
- The architecture's listed substrate channels are "exemplars,
  recipes, retrieval shards." We built exemplars (the lowest-
  leverage). Recipes (the highest-leverage) require postmortem
  on audit signal, which we haven't built.
- The format-leakage pattern on phi4-mini is exactly the kind
  of insight a postmortem cold-path session would surface as a
  recipe; few-shot exemplars wouldn't catch it.

Conclusion (tentative — under discussion next session):
**postmortem-derived recipes should be the primary substrate
channel.** Exemplars stay in the codebase as one channel among
several, but aren't the lift mechanism.

### 3.2 The audit layer that already exists isn't being used by bench

`pkg/verifier` (phase-ramp policy, Wilson lower bound on judge
agreement) and `pkg/judge` (100% un-grounded / 10% grounded
sampling with Anthropic-backed judge) are *wired into the runner*,
but the bench bypasses the runner entirely — Single and Vote call
`adapter.Execute` directly. The verifier/judge signal that would
drive postmortem isn't being collected during a bench run.

To use it in bench either:
- Wire the runner into bench (heavyweight refactor)
- Add a lightweight `Auditor` interface that captures rich signal
  beyond pass/fail (`{verdict, issues, vote_disagreement,
  confidence_drop}`) per-case in the JSONL

The lightweight Auditor seems right. Failure categorization (PR
#42 in the task list) is the start of this.

### 3.3 The substrate-floor framing splits into two

| Approach | Substrate floor | Why |
|---|---|---|
| **Ensemble (vote / critique)** | Substrate must be doing real reasoning — generating signal to vote on / critique | Voting noise = more noise. Critique on a random answer = critique of nothing. |
| **Postmortem / recipe** | Substrate must have *consistent* failure modes (doesn't matter if good or bad) | Postmortem learns from the *shape* of failure, not the *substance* of success. Phi4-mini's `extracted=""` is perfectly learnable. |

Postmortem has a lower floor. That's a real and useful claim.

### 3.4 Cost wedge changes shape when the goal is small-hardware

Project thesis (parent CLAUDE.md): "Production-grade LLM handling
at a fraction of the cost."

But operator clarified today: **the cost in question is not the
API dollar — it's the dependency on cloud / API infrastructure.**
Goal = enable real LLM work on small local hardware (3-8B models,
quantized, on consumer Apple Silicon or similar).

That reframes the success criterion:
- "Match Sonnet's output" isn't the target.
- "Lift phi4-mini's 13% on GPQA toward Haiku's 50%" *is* the
  target.
- Every architectural addition gets judged by how much of that
  37-point gap it closes.

This puts recipes (cheap, substrate-agnostic, learnable from
consistent failures) at the center of the project rather than
ensembles (which need substrate that's already competent).

---

## 4. Operational discoveries (what bit us, how to avoid)

### 4.1 LM Studio autonomously reverts loaded-model settings

Multi-hour bench runs against LM Studio cannot be trusted. Around
case 10 of our long phi4-mini run (LM Studio era), LM Studio
unloaded the model and reloaded it with default settings — n_ctx
back to 4096. Hermes errors started immediately and continued for
the next 130 cases.

**Recommendation (now in practice):** use Ollama, not LM Studio,
for long unattended runs. Ollama+Hermes was stable for the full
60 cases of the killed phi4-mini run.

### 4.2 Hermes' default persona is enormous (~14k tokens)

`~/.hermes/skills/` had 87 SKILL.md files. Hermes auto-discovers
and renders them into a `~/.hermes/.skills_prompt_snapshot.json`
that gets injected on every call. We set `toolsets: []` (operator
config edit, 2026-05-22 today) which dropped only a small chunk
(~250 tokens) because the skill registry is the real bulk.

**Operational rule:** if running Hermes against a small-context
model, plan for Hermes to require ~14-16k tokens of headroom on
top of your actual prompt. With phi4-mini@65k context this is fine;
with anything tighter (gemma@4k, etc.) it's the first thing to
debug when calls fail.

### 4.3 Hermes requires ≥64K context (even if you tell it the model is smaller)

Setting `model.context_length: 32768` got rejected by Hermes with:
"Model has a context window of X, which is below the minimum
64,000 required by Hermes Agent." Operator override (set it higher
in config than the model actually supports) might work but is
fragile.

Practically: Hermes wants room for its boilerplate + persona +
working context. 64k is its baseline. Pick a model that supports
that natively (phi4-mini does at 128K).

### 4.4 cmd/bench --orchestrator-url default isn't substrate-aware (BUG)

When `--orchestrator-substrate anthropic` is passed but
`--orchestrator-url` is not, the URL stays at its default
`http://127.0.0.1:8642` (the Hermes default). The anthropic
adapter then POSTs to Hermes's address → 404. The frontier
arm's URL default is `""` which correctly defaults to the real
Anthropic API.

**Fix:** the URL flag default should depend on the substrate
flag value. Real bug, worth a small PR.

### 4.5 LM Studio shares n_ctx across parallel sessions

Even if you load a model with n_ctx=16384, three parallel
inference calls each requesting ~14k tokens of context blow
past that shared pool. Ollama's parallel-request model is
different (each gets its own slot up to num_ctx).

**Operational rule:** for LM Studio, divide your loaded n_ctx
by your expected parallelism to get the real per-call ceiling.
For Ollama, the per-slot budget is independent.

---

## 5. State of the loop / what's running

**Running right now (background task `b3913eyi5`):**
- phi4-mini full 198-case GPQA Diamond bench
- With `--prompt-suffix` set to the format recipe
- Started ~22:50; ETA ~10-14 hours
- Output paths: `/tmp/gpqa-phi4mini-suffix-{run.log,report.json,stream.jsonl}`

**Pending (not running):**
- Failure categorization PR (#42 in task list)
- v3 architectural redesign discussion
- Vote tiebreaker fix
- cmd/bench URL default fix
- Hermes audit signal wiring into bench (lightweight Auditor)

**Tagged + released:**
- v1.0.0 (bench-ready foundation)
- v2.0.0 (self-improvement loop closed — exemplar/curated path)
  - Note: v2.0.0 architecture is now under reconsideration
    (see 3.1)

**Cost spent today:** ~$0.30 in Anthropic API (mostly the
Haiku 30-case runs)

---

## 6. Open architectural questions

For the v3 design discussion next session. None of these are
settled:

1. **Is the bench the right testbed for the loop at all?**
   GPQA cases are unique by design; cross-case transfer is
   limited. Real workloads (email drafting per Phase 1) have
   more structural similarity between cases.
2. **What does the substrate actually hold?** Recipes /
   anti-patterns / exemplars / retrieval shards — which are the
   primary channels and which are secondary?
3. **How does "audit happens anyway" wire into bench?**
   Lightweight Auditor interface vs runner integration.
4. **How does the cold path actually do postmortem?**
   Pure-LLM analysis vs statistical-clustering+LLM-naming vs
   operator-in-the-loop.
5. **How does the warm path consume the substrate?** Few-shot
   in user-prompt vs recipe-in-system-prompt vs tiered injection.

Process suggestion (also pending): draft
`scratch/postmortem-design.md` as the working spec, iterate via
edits, ship when it converges. Same pattern as
`scratch/library-plan.md` was used originally.

---

## 7. What this doc replaces

This doc is the empirical companion to the v3 design discussion.
When that discussion produces `scratch/postmortem-design.md`,
this file becomes historical evidence the design is grounded in.

Don't update this file. If a future session adds findings, write
a new dated doc.
