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

## Playbook persistence: exemplars and recipes (schema sketch)

**Where this fits.** Per parent CLAUDE.md (Self-improvement loop), specialization is layered: playbooks, retrieval indexes, routing topology, prompt templates. Playbooks are the *learned-within-niche* substrate of a brain-function specialist — what it accumulates over time about how to do its job. The locked principle (2026-05-18): exemplars are ground truth, per-specialist; recipes are consolidated distillations; vetted cross-specialist recipes can promote to the shared semantic pool, gated by curation. This section sketches the *shapes* of those two records and the lifecycle that ties them together. No Go code yet — the schemas firm up once a real Hermes adapter is exercising them.

### Exemplar (per-specialist, ground truth)

One exemplar = one *recorded invocation worth remembering*. Captured at invocation completion by the curation layer, not by the adapter itself. Brain analog: an episodic memory trace tagged for cortical consolidation later.

Sketch:

```
exemplar:
  id:                 stable identifier (sess-… reuse is fine — exemplars are pinned trace pointers)
  specialist:         brain-function role (reasoning/critic/retrieval/...)
  recorded_at:        timestamp
  input_signature:    compact fingerprint of the invocation Input (content + OutputSchema; enough to find "similar past asks")
  classification:     the run's classification level (carried from envelope)
  output_signature:   compact fingerprint of the Output.Content + key Signals (for dedupe + similarity search)
  trace_ref:          pointer into pkg/trace (the fat record — verifier feedback, retrieval refs, cost ledger, full subtree topology)
  verdict:            curator's judgment — kept / probationary / archived
                      (NOT the inhibitor's verdict; that's a different axis)
  signals:
    confidence:       Output.Confidence at completion
    grounded_pass:    bool — did grounded checks (compile/exec/retrieval-match) clear?
    downstream_outcome: enum — succeeded / inhibited / rejected / ignored
                      (filled in when the *parent* recomposes or the verifier reports)
    explicit_signal:  optional thumbs-up/-down if the user left one
    implicit_signal:  optional engagement bundle (kept reading, copied, asked follow-up)
  usage:
    times_retrieved:  how often the exemplar has been pulled for an analogous invocation
    last_retrieved_at: timestamp — feeds the rot detector
  links:
    derived_recipes:  list of recipe ids that consolidated this exemplar (back-edge for traceability)
```

Notes:

- **Exemplars are pinned trace pointers, not copies of the trace.** The full record lives in `pkg/trace`. The exemplar carries enough to be searchable (signatures), evaluable (signals), and findable (trace_ref). Keeps the playbook store small.
- **Per-specialist.** Each brain-function role has its own exemplar store. A retrieval exemplar means nothing to a critic. Cross-specialist learning happens at the recipe layer (below), not at the exemplar layer.
- **The verdict field is the curation handle.** `kept` exemplars feed consolidation; `probationary` exemplars are watched but not yet trusted; `archived` exemplars are removed from active retrieval but kept for audit.
- **Downstream outcome is the verifier signal arriving late.** A bad downstream result retroactively flips a `kept` upstream exemplar to `probationary`, regardless of how good its local signals looked. This is how summary-loss-style drift gets caught at the playbook layer — the upstream "looked fine, did damage" pattern.

### Recipe (per-specialist, consolidated; promotable to shared semantic pool)

A recipe is a *distilled how-to* drawn from several exemplars. One recipe captures a recurring strategy a specialist applies. Brain analog: cortical consolidation extracting a stable rule from many episodic traces.

Sketch:

```
recipe:
  id:                 stable identifier
  specialist:         the brain-function role that authored it
  title:              human-legible label (curation surface; "induction over loop counter")
  when_to_apply:      structured trigger conditions
                      — input shape pattern (input_signature predicate)
                      — classification floor/ceiling
                      — upstream context (e.g. "when planning emitted a critic SubAP about correctness")
  prompt_fragment:    the actual reusable scaffold (template, instruction block, exemplar set, or retrieval shard pointer)
  expected_signals:   what "this recipe worked" looks like (confidence range, grounded_pass=true, expected output_signature class)
  evidence:
    drawn_from:       exemplar ids (≥ N, where N is a curation threshold)
    consolidated_at:  timestamp the consolidation job created/updated this recipe
    last_validated_at: timestamp of most recent strong-model audit pass
  status:
    scope:            per-specialist | shared-semantic (promotion gated by curation)
    health:           green | yellow | red — rot tracking (below)
    times_applied:    counter; feeds health and rot
    win_rate:         fraction of applications with downstream_outcome=succeeded
                      (computed; updated by the consolidation job)
```

Notes:

- **`when_to_apply` is structured, not freeform.** That's what makes a recipe retrievable by the adapter at invocation start without re-asking an LLM "which recipe applies here?". The structure may be coarse early (input_signature similarity + classification range) and grow finer over time.
- **`prompt_fragment` is deliberately opaque.** Whatever the adapter needs to consume — a Jinja-style template, a few-shot block, a retrieval index pointer, a tool-use directive. Schema doesn't constrain it; consumers do.
- **Promotion to shared semantic.** A recipe earns `scope: shared-semantic` when (a) its win_rate is stable above a threshold over a minimum number of applications, AND (b) the strong-model audit confirms it generalizes beyond its origin specialist's idiosyncrasies. The curation layer owns the promotion call; nodes don't write to the shared pool unilaterally.
- **Win-rate is computed on `downstream_outcome`, not local confidence.** A recipe that produces confident-but-wrong outputs is exactly the kind of sycophancy drift we're trying to catch — local signals don't earn promotion.

### Consolidation job (the "sleep" loop)

Background job that runs periodically per-specialist, not on the inference path. Brain analog: sleep-driven cortical consolidation of hippocampal traces.

Pipeline:

1. **Scan recent exemplars.** New `kept` exemplars since last run, plus any whose `downstream_outcome` field flipped.
2. **Cluster by input_signature + classification + upstream context.** Looking for groups of ≥ N exemplars that handle similar invocation shapes the same way.
3. **For each cluster:**
   - If a matching recipe exists, update its `evidence` (append exemplar ids, recompute win_rate, refresh `last_validated_at` if signals are still healthy).
   - If no matching recipe exists and the cluster is large + healthy enough, *propose* a new recipe. Proposal goes through one round of strong-model audit before activation.
   - If a matching recipe exists but the new exemplars' signals contradict it (win_rate drops, contradictions appear, grounded_pass fraction falls), flip the recipe's health to `yellow`; further degradation flips to `red` and the recipe is retired.
4. **Rot pass.** Recipes with `times_applied` stagnant beyond a TTL get health `yellow`. Recipes contradicted by other healthier recipes inside the same specialist get flagged for the curator. Underused recipes are merged or archived.
5. **Promotion pass.** Recipes with green health, win_rate above threshold, sufficient applications, and a passing cross-specialist audit get promoted to `shared-semantic`.
6. **Garbage collection.** Archived exemplars older than the retention window are pruned (the trace record is *not* — that's the immutable audit log).

Notes:

- **Strong-model audit lives in this loop, not the inference path.** Both new-recipe proposals and periodic re-validation pass through the same audit gate that pkg/eval already seam-reserves. This keeps audit cost amortized across many invocations.
- **The consolidation job IS where artifact rot is fought.** Per parent CLAUDE.md "Artifact rot is a first-class concern": prune underused, merge redundant, flag contradictions, retire unhealthy. All of that happens here, on a cadence, not lazily during inference.
- **Promotion is one-way for now.** A `shared-semantic` recipe that decays gets demoted to `per-specialist` first (visible to its origin), then archived. The shared pool stays small and trustworthy.

### Open subquestions (playbook-specific)

- **Storage substrate.** In-memory + JSON snapshots per specialist (cheap, inspectable, rollback-safe) versus an embedded KV (BoltDB / Badger). Probably JSON for v0; KV when the per-specialist store outgrows memory.
- **input_signature fingerprint.** Token-set hash? Small-embedding cluster id? Both? The recipe `when_to_apply` retrieval performance hinges on this — needs to be cheap enough to consult on every invocation.
- **Cross-specialist audit format.** When a recipe is being considered for promotion, what does the strong-model audit actually see — the recipe alone, or recipe + a sample of its exemplars? Sample feels right but increases audit cost.
- **Probationary exemplar handling.** Do `probationary` exemplars contribute to recipe consolidation at reduced weight, or not at all until promoted back to `kept`?
- **Recipe interactions.** When two recipes match `when_to_apply` for the same invocation, what's the tiebreaker — win_rate, recency, specificity? Probably specificity-then-win-rate, but the precedence rule wants exercise.

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
