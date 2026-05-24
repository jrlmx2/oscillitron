# Design conversation — 2026-05-24

Snapshot of architectural thinking from the 2026-05-24 session. **Not
locked, not committed to code paths beyond what landed in PRs.**
Captured so the line of reasoning can be picked up cold later. Each
section names: what was proposed, what's been validated empirically,
what's open, and where it sits relative to the existing roadmap.

## 1. The XML-tag format experiment (PR #64, partially superseded by #65)

**Proposed:** drop benchmark-specific MCQ wording from
`minimal.ProcessInstructions`; use task-agnostic XML tags
(`<response>`, `<confidence>`) as the universal wire format; kill
the OpenAI `response_format` JSON schema enforcement as a
substrate-specific bifurcation.

**Empirically validated:**

- **qwen2.5:7b** — 100% XML tag compliance, 100% confidence extraction,
  0 format_no_letter on a 10-case smoke. The format works.
- **phi4-mini** — 0% XML tag compliance, model produced token salad.
  Substrate capability floor too low to follow free-form instructions
  without engine-level constraints. **Confirmed phi4-mini below floor.**

**Conclusion:** the format design is sound; phi was below the
substrate-quality threshold the architecture assumes. **phi4-mini
dropped from the matrix going forward.** Killing `response_format`
remains the right call (it bifurcated cross-substrate behavior); the
prompt-text design is correct.

**Status:** PR #64 merged.

## 2. Tree orchestrator (PR #65, in review)

**Proposed:** wrap `runner.Run` as a `benchmark.Orchestrator` so the
bench can measure the full Oscillitron call-tree pattern (plan →
emit_subtree → child APs → recompose) as a third arm alongside Single
(frontier baseline) and Cope/Vote.

**Key design choices captured:**

- **Force `PlaybookPlan` on root** via an internal `forcePlanOnRoot`
  adapter wrapper. Pre-stamping `env.Evaluate` doesn't work — the
  runner unconditionally calls `adapter.Evaluate` and overwrites the
  stamp.
- **`recomposer.Synth` with `AdapterSynth`** wrapping the same
  substrate, not `Concat`. Each binary reduction is a real substrate
  call that integrates two child answers — the substrate participates
  in both divide and conquer. Concat would have been cargo-cult.
- **`MaxDepth = 10`** default. The model typically uses 1-2 levels and
  stops; the budget exists to allow genuinely deep decomposition.
- **No cope wrapping.** Tree's value stands on its own; wrapping with
  Coping would let frontier rescue mask whatever decomposition
  contributes.

**Empirically observed in smoke:**

- Tree fires the plan playbook, emits sub-APs, recurses, recomposes.
  The architecture works end-to-end after parser-tolerance fixes
  (verifySpecRaw.UnmarshalJSON, parseExecuteResponse fallback).
- On 10-case qwen2.5 GPQA Diamond: tree pass-rate 16.7% vs cope-vote-5
  40%. Small-sample finding; tree may underperform on MCQ-style
  benchmarks where decomposition doesn't fit the task shape. Worth
  re-measuring at 200-case scale.

**Open questions:**

- Does tree pay off more on MATH-500 (stepwise reasoning naturally
  decomposes) than on GPQA Diamond (single MCQ doesn't)?
- Token cost is high (1 plan + N children + N-1 synth). On a
  capability-dense substrate (Haiku) this might be a worse trade than
  on a cheap one.

## 3. `cope.Replan` action (queued, PR not yet opened)

**Proposed:** add a fifth `cope.Action` between `ShipWithCaveat` and
`Escalate`. When confidence is low and stakes are high, try
"go deeper on the same substrate" *before* reaching for the frontier
rescue.

**Rule-table band:**

```
conf ≥ 0.85, any stakes      → Ship
conf 0.5–0.85, low/medium    → ShipWithCaveat
conf 0.5–0.85, high          → Replan          ← deeper tree, same substrate
conf < 0.5,    low/medium    → ShipWithCaveat
conf < 0.5,    high          → Escalate         (only if Replan exhausted)
```

**Implementation knobs for "more planning":**

- **Depth bump** — Replan #1 raises MaxDepth from N to N+K. Simplest.
- **Breadth nudge** — plan-playbook prompt hint to emit more children.
  Subtler, model-dependent.

**Budget:** `MaxReplans = 1` per case. Replanning is expensive;
unbounded would risk infinite loops.

**Architectural fit:** depends on Tree (Replan calls a deeper Tree on
demand). Tree must merge before Replan PR is meaningful.

**Status:** queued behind #65 merge.

## 4. Library-learned one-liner injection (v4.x or v5)

**Proposed:** the universal template stays static; when the library
detects an opportunity to improve output for a (substrate, task-type)
combination, it injects a learned **one-liner prefix** before the
prompt. The library never edits the scaffold — only prepends.

```
[learned one-liner — optional, library-injected per (substrate, task-type)]
[case prompt — built by loader, task-specific]
[ProcessInstructions — fixed, task-agnostic]
```

Properties:

- **Stable substrate** — prompt-engineering changes don't ripple
  through the architecture.
- **Composable** — multiple one-liners can stack independently.
- **Per-context** — different one-liners for different (substrate,
  task-type) buckets.
- **Auditable** — the library logs exactly what one-liner was applied
  and why.

**Maps to existing primitives:**

- `pkg/semanticpool` already injects a stable preamble (currently
  global; one-liner injection is per-(substrate, task-type)).
- `pkg/exemplar` already retrieves per-action context; a learned
  one-liner is essentially a single-string exemplar with the
  highest-confidence ranking.
- Curation cold path could propose candidate one-liners when a
  (substrate, action) bucket has a stable pass-rate gap vs published.

**Learning signal:**

- Bench-derived first (grader-feedback driven), same shape as v4's
  calibration-correction store.
- User-feedback later (the original v4 scope, now reframed as v5+
  signal source).

**Architectural fit:** parallel to v4 calibration-correction. Same
Store / Corrector pattern, different signal kind. Both are "library
learns over time what works for this substrate."

**Status:** design only; no code.

## 5. Substrate routing (v5/v6, biggest idea)

**Proposed:** add an axis orthogonal to the existing brain-function
playbooks — substrate selection based on content domain. Coding tasks
route to a coder-specialist substrate; research tasks to a general
chat substrate; design tasks to a multimodal substrate; etc.

```
                         ┌─ Existing axis (LOCKED 2026-05-18) ─┐
Cognitive role:           plan / process / critique / compose
                         └─────────────────────────────────────┘
                                          ×
                         ┌─ New axis ─────────────────────────┐
Substrate / domain:       coder | researcher | designer | …
                         └────────────────────────────────────┘
```

**Crucially: does NOT conflict with the brain-function lock.** The
lock said cognitive *roles* aren't node types. Substrate selection is
a different axis — picking which model serves the call, not picking
the cognitive role.

**Implementation shape:**

```go
type RoutingAdapter struct {
    Routes   []Route                  // {Classifier, Adapter}
    Default  adapter.Adapter
    Tracer   trace.Tracer
}

type Route struct {
    Classifier func(env session.Envelope) bool
    Adapter    adapter.Adapter
}
```

Three classification strategies, increasing sophistication:

1. **Static keyword rules** (v0) — `if strings.Contains(input,
   "code")`. Transparent, testable, cheap.
2. **Cheap LLM classifier** — a small model (3-4B) emits a domain tag.
   ~50 tokens per call.
3. **Embedding similarity** — embed substrate descriptions + the task,
   route to nearest. Closer to production MoE patterns.

**Hardware reality check (operator constraint, 2026-05-24):**

Multi-substrate routing **does NOT require simultaneous loading**.
Ollama unloads idle models after a timeout. The realistic shape is a
rotating hot-set of 1-2 specialists, with swaps on call boundaries:
"eject coder, load researcher." Adds latency but doesn't add VRAM
ceiling.

The local-LLM angle actually *favors* routing because there's no
per-call API cost on substrate swaps; the cost is load-time latency,
which amortizes across vote-5 within a single substrate route.

**Composes with existing patterns — three sharp questions:**

1. **Vote across substrates, or vote within?** Today vote-5 runs 5
   attempts on one model. Routing could vote-5 within the routed
   substrate (variance reduction, current pattern) OR vote-5 across
   different substrates (ensemble of specialists). Very different
   signals.
2. **Tree decompose with per-child routing.** Plan emits sub-APs;
   each sub-AP could route to a different specialist. "Solve this
   math" → math substrate; "Explain the physical setup" → general;
   "Write the Python check" → coder. The orchestrator becomes a
   *team of specialists coordinated by the runtime*. This is the
   version with real teeth.
3. **Cost vs value.** Loading specialists into rotation is only
   useful if specialists materially beat the general model on their
   domain. Empirically testable: A/B general-vs-routed-to-coder on a
   coding benchmark.

**Coder-variant inventory for our 5 families (researched 2026-05-24):**

| Family | Coder variant on Ollama |
|---|---|
| Qwen 3.5 | `qwen3-coder-next` (no explicit qwen3.5-coder yet) |
| Llama 3.1 | `codellama` (older backbone) |
| Mistral | `devstral-small-2` / `codestral` (large) |
| Hermes 3 | ❌ none — NousResearch ships general fine-tunes only |
| GLM 4 | `codegeex4` |

4 of 5 families have a coder counterpart. Hermes 3 would be the gap
in any general-vs-coder A/B.

**Project-narrative implication:**

- Today: "cheap-substrate orchestration ≈ frontier quality at
  fraction of cost."
- With routing: "cheap *specialist* substrates coordinated by the
  runtime ≈ frontier quality on every domain at fraction of cost."

Stronger pitch. Aligns with where production LLM systems are heading
(Claude's intent classification, GPT's tool routing, every serious
agent framework has multi-model dispatch).

**Status:** design only; deferred until v4 + Tree are validated.
Roughly v5 or v6 territory.

## 6. Hardware / quantization considerations (2026-05-24 specifics)

Local Ollama substrates pulled this session:

| Family | Tag | Size | Effective bits/param |
|---|---|---|---|
| Qwen 3.5 | `qwen3.5:9b` | 6.6 GB | ~6 (Q5/low-Q6 default) |
| Llama 3.1 | `llama3.1:8b-instruct-q6_K` | 6.6 GB | Q6_K explicit |
| Mistral | `mistral:7b-instruct-v0.3-q6_K` | 5.9 GB | Q6_K explicit |
| Hermes 3 | `hermes3:8b` | 4.7 GB | Q4_K_M default (no q6 tag) |
| GLM 4 | `glm4:9b` | 5.5 GB | Q4_K_M default (no q6 tag) |

Full notes: `scratch/substrate-quantization-notes.md`.

**For interpretation:** the Hermes / GLM Q4 quantization handicap is
~1-3 pp on GPQA/MMLU-Pro vs the Q6 trio. When reading cross-substrate
results, apply that adjustment before concluding a family is weaker.

## 7. Roadmap implications

Reframed ordering (subject to operator confirmation):

1. **v4** — calibration-correction substrate (per-(model, domain,
   raw-conf-band) Store + Corrector). Pinned in `scratch/v4-design.md`.
2. **v4.x** — library-learned one-liner injection (same Store shape,
   different signal kind). May fold into v4 or split.
3. **v5** — `cope.Replan` action (small extension, depends on Tree).
4. **v5+** — substrate routing. New `RoutingAdapter` + classifier +
   per-domain specialist substrates + the "team of specialists"
   composition. This is the big architectural move.
5. **v6** — multi-modal substrates (designer, visual, audio). Same
   routing pattern, different specialist axis.

## 8. Open architectural questions

Tracked here so they don't get lost:

- **Vote semantics under routing.** Vote-within-substrate vs
  vote-across-substrates as separate primitives, or one configurable
  shape?
- **Tree decompose + per-child routing.** Does plan's emit_subtree
  carry routing hints per sub-AP, or does each child re-classify on
  its own input?
- **Classifier substrate.** Static rules → LLM classifier → embedding
  similarity. When does each step pay off empirically?
- **Confidence aggregation across substrates.** Vote-across-substrates
  has heterogeneous confidence signals (different models calibrate
  differently). v4 calibration-correction handles within-substrate;
  cross-substrate is a new correction class.
- **Substrate eviction policy.** With multi-substrate routing on
  constrained hardware, when to evict which model. LRU? Workload-
  aware? Operator-pinned hot set?

## 8b. Embeddings as the v5 learning substrate (parked)

Decision 2026-05-24: this lives in v5, not v4 or v4.x. Validation
of the current library has to come first.

The framing is that embeddings turn out to be the natural backing
for multiple v5 learning loops the project has half-promised:

- **k-NN routing policy**: `(task_embedding, substrate, outcome)`
  triples accumulated; new task embeds → find K nearest → route
  to substrate that historically wins on that neighborhood.
- **Contextual bandit (LinUCB / Thompson)**: same shape with
  exploration; deliberately tries under-tested substrates on novel
  tasks so the policy improves rather than getting stuck.
- **Embedding-keyed calibration** (successor to v4's discrete
  `(Model, Domain, Band)` buckets): replace hand-crafted categories
  with embedding-keyed retrieval; system discovers task structure.
- **One-liner discovery via clustering**: cluster historical (task,
  prefix, pass_rate) triples by embedding; per-cluster best prefix
  becomes the injected one-liner.
- **Embedding-distance confidence in Tree recompose**: a third
  independent confidence signal beyond model self-report and
  verifier critique — close embeddings = children agreed.

Storage shape: small in-process vector store (5-10 substrate
descriptions + a rolling history of past tasks). No external vector
DB; the routing/calibration query is brute-force cosine similarity
over a few thousand vectors in Go's stdlib math. ~50 ms per query
on a tiny embedding model.

Embedding model candidates (already-tiny, Ollama-pullable):
- `all-minilm` (~25 MB, 384-dim) — smallest, lowest quality
- `nomic-embed-text` (~280 MB, 768-dim) — good default
- `qwen3-embedding` (~600 MB, 1024-dim) — best in-family alignment
- `embeddinggemma` — if we add Gemma to the matrix

**Project-narrative impact:** moves the pitch from "cheap-substrate
orchestration ≈ frontier quality" to "**cheap-substrate orchestration
that learns its own routing, calibration, and prompting from
accumulated experience, no labels or fine-tuning required**." Same
project, stronger story.

Not building any of this in v4. The current operator-stated priority
is **validate the library more** before substantive architectural
changes. Embeddings stay parked.

## 8c. qwen3.5:9b empirical issue (2026-05-24 smoke)

**Observation:** running the 3-arm smoke (frontier + cope-vote-5 + tree)
on `qwen3.5:9b` via Ollama, the Tree arm consistently times out on
case 1's root-AP plan call:

```
07:00:31  msg=runner.evaluated  playbook=plan  ← tree arm starts plan
07:10:31  msg=runner.execute_error  err="context deadline exceeded"
          ← HTTP client timed out after 10 minutes with no response
```

The model received the plan-playbook prompt (which asks for
`{subtasks: [...], recompose: ...}` JSON) and never returned a
complete response in 10 minutes. The vote-5 arm on the same model
on case 2 IS making progress (just slow), so this isn't a
total-substrate-hang — it's specifically the plan playbook causing
trouble.

**Possible causes (untested):**

1. **qwen3.5:9b is markedly slower than qwen2.5:7b on this hardware.**
   Larger model + new architecture may chew tokens slower at the
   same quantization tier.
2. **Plan playbook prompt produces over-long generation.** The
   default plan instructions ask for a structured JSON envelope; if
   the model elaborates inside JSON field values, the response can
   balloon. No max_tokens cap is enforced.
3. **Model hasn't been pre-warmed.** First call cold-loads the
   model into VRAM (~6.6 GB); the 10-minute timeout may have been
   competing with model-load time.

**Implication for the substrate-routing concept (§5 above):** when
the hot-set rotates, "load time" is a real per-substrate cost. If
qwen3.5:9b takes minutes to first-call-warm-up on this hardware,
that has to be amortized across enough subsequent calls to justify
the rotation. Worth measuring before committing to v5 routing.

**Implication for Tree (§2 above):** the plan playbook's request
for structured JSON may be too expensive on slower substrates. The
recomposer.AdapterSynth's synthesis step has the same shape — also
asks for structured output. Both could be exposed to the same
slowness on substrates that elaborate inside JSON.

**Action queued (not done yet):** wait for the in-flight smoke to
finish, inspect per-arm timings, decide if we need a faster
substrate, a smaller substrate, or a max_tokens cap on the plan
playbook prompt before continuing matrix runs.

## 9. What's NOT in this doc

- v4 calibration-correction design (pinned, see `scratch/v4-design.md`)
- bench-findings empirical record (see `scratch/archive/bench-findings-2026-05-23.md`)
- loader design sketches (see `scratch/loader-design-{mmlu-pro,math500}.md`)
- session handoffs (see `scratch/archive/session-*.md`)

This doc is *current-session architectural thinking*. The above docs
are *committed design state*. When something here graduates to
committed state, it migrates to a sibling doc or the parent
`CLAUDE.md`.

## 10. Status of in-flight work

As of write time:

- **PR #65** (Tree orchestrator + parser tolerance) open, in review.
- **qwen3.5:9b GPQA Diamond Tree smoke** running in background.
- All architectural ideas above are design-only; no committed code
  for substrate routing, cope.Replan, one-liner injection, or v4
  calibration-correction.
