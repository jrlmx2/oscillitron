<!-- CLAUDE GENERATED -->
# Oscillitron design notes (working)

Ideas in motion. Not locked. Promote to `CLAUDE.md` (or to a `references/` doc) once an idea settles enough to act on.

Last updated: 2026-05-18

---

## Specialist lifecycle: session-bounded with token-budget threshold

**Idea.** A specialist isn't a long-lived entity — it's instantiated per task, runs against a token budget, and exits when either the task is done or the budget threshold is hit.

- Threshold ~70% of context window (tunable; varies by model and task).
- Two exit conditions, two summary shapes:
  - **Done before budget** — summary IS the answer.
  - **Threshold hit** — summary is "where I got, what I tried, what remains."
- Downstream session must know which kind it's receiving — they trigger different next moves.

**Why this is good.** Bounds context degradation (long-context attention dilution is well-documented), makes specialist cost predictable, gives a clean handoff point. Brain analog: working memory has finite capacity and must offload before exhaustion.

## Action potentials are summaries

**Idea.** The AP between sessions IS the compressed summary the upstream session produces on exit. The "spike" is just a compact handoff payload sized to be cheap for the downstream specialist to ingest.

**Why this is good.** Gives the AP a concrete data shape (until now hand-wavy). Unifies the spike concept with the practical handoff mechanic the system needs anyway.

**Open.** Structured (JSON-like fields: state, attempts, remaining, confidence) or freeform prose? Structured is cheaper to parse and route on; freeform retains nuance. Probably a hybrid — small structured envelope wrapping a freeform body.

## Multistep reasoning across sessions

**Idea.** Hard problems chain multiple sessions, each focused on its slice, handing summaries forward. This is tree-of-thoughts / chain-of-thought spread across explicit session boundaries with summaries as the bridges.

**Tradeoff.** Buys context-window cost amortization at the price of summary-loss risk in long chains. Mitigated by inhibition (below) and chain-depth caps.

## Inhibition as circuit-breaker

**Idea.** An inhibitor (specialist or edge property — undecided) watches a chain for drift signals and aborts/restarts when triggered. Brain analog: anterior cingulate detects conflict and signals override to prefrontal cortex.

**Drift signals to watch.**

- Grounded check failures (math wrong, code doesn't compile, retrieval empty).
- Contradiction with an earlier summary in the chain.
- Confidence drop across successive sessions.
- Repetition / cycling (specialist re-trying the same approach).
- Parallel-specialist disagreement (two specialists doing the same work diverge significantly).

**Restart design.**

- **Restart depth matters.** Full restart loses everything; restart-to-last-good-checkpoint is more efficient but harder to implement correctly. Probably want both modes — minor drift triggers re-statement, major drift triggers reformulation. Brain does both.
- **Hard max-iteration cap.** Even with inhibition, the inhibitor itself can fail or drift can be subtle. A hard cap (N sessions or M total tokens) is the belt-and-suspenders.
- **Detecting drift is itself a learnable skill.** The inhibitor's playbook grows from cases humans flagged "this should have stopped earlier."

## Summarizers are learnable too

**Idea.** What makes a good handoff summary depends on the next specialist's needs. Specialist-to-specialist handoff conventions grow over time as specialists learn each other's hungers. Exactly the kind of organic-within-niche growth the design wants.

## Risks to track

- **Summary loss compounding** in long chains.
- **Inhibition false positives** — stopping good reasoning that looks drift-like.
- **Inhibition false negatives** — subtle drift that doesn't trigger.
- **Cost of restart** — every restart pays the upstream cost again. Need to weight inhibition aggressiveness against this. Probably the right discipline is: restart cost should be tracked as part of the verifier signal, so the system learns to inhibit early *enough* but not *too* early.
- **Inhibitor capture.** If the inhibitor specializes against one drift pattern, it may miss others. Periodic strong-model audit on inhibited *and* non-inhibited chains is the tripwire.

## Open subquestions

- Token-budget threshold — fixed at 70%, or tuned per specialist / per model?
- Summary format — structured, freeform, or hybrid envelope?
- Inhibitor as a dedicated node vs. as a graph-edge property attached to every chain edge?
- Checkpoint granularity — every session boundary, or finer (every N tokens within a session)?
- How does the verifier signal reach the summarizer? A bad downstream outcome should retroactively reward/punish the upstream summary that fed it.
