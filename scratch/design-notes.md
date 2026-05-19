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

## Action potentials are invocations (was: summaries)

**Updated 2026-05-18.** Earlier framing called APs "summaries that hand off between peer specialists." That's been superseded by the call-tree lock in the parent CLAUDE.md: an AP is *an invocation of one brain function*, not a peer handoff. The "summary" framing still applies to what the invocation *produces* on exit (its output, classification, sub-AP seeds, exit reason), but the AP-as-it-enters-an-invocation is a call, not a handoff.

**Working envelope sketch** (stable enough to code against; final shape settles when the Hermes adapter exercises it):

- `SchemaVersion` — additive evolution.
- `BrainFunction` — which function to invoke. Exactly one (siloed).
- `Input` — the thing to operate on.
- `OutputSchema` — handoff contract for the output. *Also* serves as the preloaded prompt requirement that forces the producing LLM to self-classify against it.
- `ParentRef` — position in the call tree (root = nil).
- `Budget` — token/depth allotted to this subtree.
- On completion, the invocation populates: `Output`, `Classification` (against schema), `Confidence`, `SubAPs []SubAPSeed`, `ExitReason`.

**AP vs. trace.** The AP stays lean — it's read by the next invocation, so token cost compounds over every hop. The fat learning-loop record (verifier feedback, retrieval shards consulted, full tree topology, cost ledger entries, calibration metadata) lives in `pkg/trace`, off the inference path. Package boundary enforces the separation.

**Sub-AP seeds.** What the producing brain function emits to spawn further invocations. Likely a small struct: `{BrainFunction, Input, OutputSchema}` — enough to construct the child AP. Exact field set settles with the adapter.

## Multistep reasoning across sessions

**Idea.** Hard problems form a *call tree* of invocations (LOCKED 2026-05-18 — see parent CLAUDE.md), each focused on its slice, recomposing results back up. Tree-of-thoughts spread across explicit session boundaries with sub-AP emission as the descent mechanism and the recomposer as the ascent mechanism. Earlier framing called this a "chain" — superseded; chains are just the degenerate case of a tree with one child per node.

**Tradeoff.** Buys context-window cost amortization at the price of summary-loss risk in deep subtrees. Mitigated by per-subtree inhibition (below) and depth/budget caps per subtree plus a global cap on the whole tree.

## Inhibition as circuit-breaker

**Idea.** An inhibitor (edge property — LOCKED 2026-05-18, see root CLAUDE.md) watches a chain for drift signals and aborts/restarts when triggered. Brain analog: anterior cingulate modulates signal flow between regions rather than acting as a separate stage.

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
- Checkpoint granularity — every session boundary, or finer (every N tokens within a session)?
- How does the verifier signal reach the summarizer? A bad downstream outcome should retroactively reward/punish the upstream summary that fed it.
