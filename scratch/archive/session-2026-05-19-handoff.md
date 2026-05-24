
# Session handoff — 2026-05-19

Pick-up note for tomorrow. Context from a design session on the AP workflow, playbook substrate, and benchmarking.

## Where the conversation landed

Started from "how do we benchmark improvement over the base model" — agreed honest framing: at v0 the wrapper is 3 placeholder nodes, so any benchmark right now measures **scaffolding lift** (critic+compose vs single-shot), not the **learning lift** that's the pitch. Learning lift needs the curation layer to exist. Pivoted to fleshing out the system first, then benchmark.

## New design decisions reached this session (not yet in CLAUDE.md)

These are agreed and waiting to be written into `CLAUDE.md`'s Architecture section and locked-decisions list:

1. **Uniform node model.** No structurally distinct seed nodes per brain function. One AP-handling workflow runs at every recursion level. Specialization lives in the **playbook substrate keyed by action**, not in node types. This sharpens the brain-function lock-in (cortical microcircuit uniformity) rather than breaking it.

2. **Two-step AP shape: evaluate → execute.**
   - **Evaluate:** LLM call that picks the right playbook for this AP.
   - **Execute:** run that playbook; produces a result and/or emits sub-APs.
   - Every AP evaluates (no trivial-skip path at v0).

3. **Cheap-local-first, frontier as last-ditch.** Evaluate runs on Hermes-on-base by default. Claude is reserved for the `delegate` runtime escalation gate (critic failed past retry budget) and for sampled `verify_judge` audits — not freely selectable by evaluate.

4. **v0 playbook set — 5 playbooks evaluate can pick from:**

   | Playbook | Envelope-input | Execute-pulls | Output | Output category |
   |---|---|---|---|---|
   | `plan` | task | — | `{subtasks, recompose}` | emit_subtree |
   | `process` | task | — | result | return_result |
   | `critique` | prior result + context | — | pass / issues | verifier_signal |
   | `verify_grounded` | result + check spec (from envelope) | — | pass / fail | verifier_signal |
   | `compose` | `{scope_handle, expected_count}` | 2 results from scope channel | combined result | return_result |

   Cut from earlier proposals: `parse` (premature differentiation of `process`), `terminate` (envelope flag, not a playbook), `delegate` (runtime escalation mechanism, not evaluate-visible).

5. **Three output categories** (non-uniform — envelope must encode):
   - `emit_subtree` — produces sub-APs into parent's scope; doesn't return up
   - `return_result` — value flows up the tree
   - `verifier_signal` — pass/fail/issues flows to **the runtime**, not next AP. Runtime owns retry / proceed / escalate policy.

6. **Plan bundles recompose spec.** Plan's output is `{subtasks: [...], recompose: pairwise|sequential|none}`. Can't decompose meaningfully without saying how it composes back.

7. **Compose input is scope-channel-based.** Compose AP doesn't *receive* 2 sibling results — it receives `{scope_handle, expected_count}` and pulls 2 from a parent-scoped channel at execute time. Sibling-triggered semantics per earlier lock.

8. **Sibling dispatch is randomized.** Runner pops ready sibling APs in random order, not emission order. Keeps v0 baseline honest about not relying on emission order; future parallel runtime won't change observable behavior.

9. **Judge sampling policy.** 100% judge on un-grounded outputs, 10% sample on grounded. Will revisit if cost gets ugly.

10. **Specialists are substrate, not nodes.** Per-instance playbook stores keyed by action tag. The "specialist" abstraction survives but moves out of the structural/code layer into the data layer.

## Open questions left unresolved at the cutoff

Two locks I asked for right before the Hermes detour — pick these up first tomorrow:

1. **Critique-emission policy — lock parent-invocation as the emitter (option a)?** Parent decides when its child results need a critique pass, encoded as `needs_verification: true` on the child AP or as a sibling critique-AP emitted after the result lands. Alternatives rejected: (b) process auto-emits critique (doubles cost on every process call), (c) runtime injects transparently (makes runtime opinionated about verification policy, fights playbook-as-evaluate's-choice). My recommendation was (a).

2. **Pairwise compose — lock pre-emitted (option a)?** Parent pre-emits N-1 compose APs at plan-completion time. Alternative: (b) compose result re-enters channel and next waiting compose self-chains. (a) is more APs but trace-faithful — every reduction step is explicit. My recommendation was (a).

## After those lock

3. Update `CLAUDE.md` — Architecture section + locked-decisions list — to reflect items 1–10 above. Replace the `reasoner → critic → composer` placeholder framing with the uniform-node + evaluate/execute + playbook-substrate shape.

4. Sketch the **JSON envelope** — single source of truth for AP shape across evaluate/execute, the three output categories, scope handles for compose, verification metadata for verify_grounded. This is the next lock and the foundation everything else hangs off.

5. Refactor the demo (`oscillitron/cmd/oscillitron`) onto the new envelope and uniform-node shape. Existing `reasoner/critic/composer` placeholder code gets replaced.

6. Then — and only then — the benchmark harness becomes worth building, because there's something coherent to measure.

## Parked: the Hermes-agent monitoring idea

Raised at end of session, then explicitly parked as "conflation of context." Capturing in case it returns:

> Start a Hermes agent to monitor the codebase, suggest improvements, and learn from OK/reject verdicts. Hook it up to Gemma 3n E4B via LM Studio.

Three issues I raised that would still apply if this returns:

1. **The OK/reject learning loop IS the curation/playbook substrate.** Without it (which is what we're designing above), rejections evaporate. Either build a minimal scratch playbook store first, or run statelessly and bank verdicts as raw material for the real substrate later.

2. **State of local infra unknown.** I haven't probed whether LM Studio is running (typically `localhost:1234`, OpenAI-compatible API), whether `gemma-3n-e4b` is loaded, or whether a Hermes process is actually running anywhere. The `pkg/adapter/hermes` HTTP client exists but the server side is unverified.

3. **"Suggest improvements" needs scoping.** To Go code in `oscillitron/`? Design docs in `scratch/`? Bugs vs refactors vs architectural drift? Open-ended produces mush. My recommendation was: Go code in `oscillitron/pkg/` as the narrow start (gradeable, exercises the local model end of the stack).

If this returns: probe local state first, then pick scope, then define verdict schema (verdict format = training signal shape), then build the minimal stateful loop.

## Benchmarking thread (still alive, deferred)

When the design above is in place:

- Bench harness lives at `oscillitron/cmd/bench`.
- Two paths over the same task set: (a) single Hermes call to base, (b) full Oscillitron call tree.
- Emit `{task, output, cost, latency, grade}` per row.
- Start grounded-only (GSM8K-slice, HumanEval-style slice) — Claude-as-judge layer added on top per the locked sampling policy.
- Learning-curve view = same harness re-run after each curation cycle; watch the Oscillitron column move while base stays flat.

## Tomorrow's first move

Lock the two open questions (critique-emission, pairwise pre-emit). Then start the CLAUDE.md update.
