# Dense-model-calls-focused-models-as-tools — routing design

---

## Executive summary (read this first — the 8-iteration arc in 60 seconds)

**The verdict on the dense direction.** "Dense model orchestrates cheap models as tools" is **not a cost win and never was.** Frontier-orchestrator-per-request costs 5–6× a *single* frontier call (it multiplies token volume ~7× at frontier rates); mid-tier-orchestrator is viable but is just the *existing* Vote/Tree orchestrators on a hosted substrate (`--orchestrator-substrate`), still ~5–6× the locked local path. The only surviving value is **packaging/ergonomics** (surfacing per-action specialists as tool schemas) and **operational simplicity** (operators who can't run local GPUs) — never cost. **The existing locks are vindicated by the arithmetic** (§2.8): ordering is `local-cheap (locked) < hosted-mid < frontier-single < frontier-orchestrator`. Everything downstream is two small lock-compatible grafts that fell out of the survey, each turned into a falsifiable keep/cut experiment.

**The three buildable outcomes.** All three are PR-ready to diff level. Each is an experiment against the existing architecture as the null (a graft earns its keep only by clearing a pre-stated threshold *above measurement noise*; "no significant difference" = cut):

| Buildable | What it is (one line) | Keep / cut gate (one line) | Lock verdict | PR-ready spec |
|---|---|---|---|---|
| **Thread B — semantic entropy** | discrete SE over Vote's N-attempt histogram → `conf = 1 − H/ln(N)`, fed into the existing `cope.RuleTable.Decide` as a better-calibrated confidence scalar | **Build** if ECE-delta vs. self-report ≥ 0.03 (no extra false-confident high-stakes ships); **cut** if < 0.01 | **compatible** (zero tension — one better scalar into a decision already made) | **§4b** |
| **H0-cost experiment** | two `cmd/bench` GPQA arms differing only in `--orchestrator-substrate` (local-Vote-5 vs hosted-Haiku-Vote-5) — no new runtime code | **Cut dense as a cost play** if PassRate(dense) − PassRate(null) ≤ +5 pp (n≈198 noise floor); +5..+10 pp → operational-only; >+10 pp → revisit | **n/a** (measures the §2.8 conclusion; locks unaffected) | **§4c** |
| **Thread A — kNN playbook-hint router** | cross-action BM25 kNN over `pkg/exemplar` → advisory `Hint` *seeding* (never skipping) Evaluate; primary metric = `router.evaluate_overrode_hint` disagreement rate | **Cut on inertness** if disagreement < 5%; **keep** if ≥ 15% AND hint net-flips cases right-ward (>+3 pp on disagreements / >+5 pp on multi-playbook) | **compatible** (one soft *dependency* tension — embeddings — deferred behind a measurement gate) | **§4d** |

**Recommended build order (§4a):** Thread B (cheapest, zero lock tension, tightest-CI test) → H0-cost (no new code, two bench invocations) → Thread A (largest lift, behind its inertness gate) → free-form Phase-1 keep-gate (the *only* place a router-keep or an SE-v1-clusterer / embedding-dependency decision can be fairly made). Dense-as-packaging deferred until a concrete operator asks; the stateful re-planning loop stays a Hermes-substrate concern under wrap-not-fork.

**Where each question is answered (pointer table):**

| Question | Section |
|---|---|
| Is "dense" frontier or mid-tier, and does it ever win on cost? | **§2.8** (head-on blended-cost arithmetic) |
| How does a model-as-tool call map onto the AP envelope without a new node type? | **§2.9** (calling protocol; reuse `plan`→`process`→`compose`) |
| The two surviving grafts' interface sketches | **§2.10** (router), **§2.11** (semantic entropy) |
| The falsification experiment / decision procedure (null hypotheses, arms, thresholds, run plan) | **§2.12** (esp. §2.12.2 router gate, §2.12.3 SE, §2.12.7 ordered run plan, §2.12.8 outcome table) |
| **The load-bearing router-inertness stress-test (is GPQA even the right test?)** | **§2.12.9** (iteration 9) |
| PR-ready diff-level specs | **§4b** (Thread B), **§4c** (H0-cost), **§4d** (Thread A) |
| Build order | **§4a** |

**Convergence note (iteration 9):** the doc is **build-complete and, as of iteration 9, the last genuinely high-leverage open question (router inertness) is resolved on paper.** The research has converged; what remains is *execution* (open the PRs, run §2.12.7), not design. See the iteration log (§6) for the explicit stop-recommendation.

---

**Status:** exploratory design doc. Iteration 9 (2026-06-14). **Iteration 9 adds the Executive Summary above and the §2.12.9 router-inertness stress-test, then declares the research converged.** The stress-test (§2.12.9) finds GPQA is structurally rigged to make the router look inert — the `Tree` arm hard-pins every child AP to `process` and the curation cycle writes every exemplar under a single `--curate-action`, so the disagreement rate is forced near zero by construction, not measured. The corrected gate: **the router's only fair test is a heterogeneous-playbook workload; on GPQA the < 5% disagreement is a foregone conclusion that proves nothing about the mechanism.** Build-order consequence: do NOT spend a router PR on a GPQA inertness run — either build the minimal heterogeneous harness first, or (recommended) run Thread B + H0-cost and defer Thread A until a real multi-playbook corpus exists. §4a and §2.12.2 updated with the sharpened fair-test definition. *Prior state below.*

**Status:** exploratory design doc. Iteration 8 (2026-06-14). **Iteration 8 turns Thread A (the router, build-plan Phase 3) into a PR-ready, diff-level, TDD-ordered spec — §4d "Thread A — PR-ready spec (kNN playbook-hint router)" — checked against the *real* current source. With Thread B (§4b) and H0-cost (§4c) already PR-ready, this completes the third and largest graft, so all three buildables are now executable. Delivered: `pkg/exemplar/across.go` (the additive `RetrieveAcross` + `Neighbor` reusing `bm25.go`'s `buildBM25Index`/`score`/`tokenize` verbatim), a new stdlib-only `pkg/router` (`Router`/`Hint`/`ExemplarRouter` with majority-vote + margin-abstain + a `validPlaybook` guard), the ~12-line runner hook before `adapter.Evaluate` (Option-A steering text, never a skip) with the `router.hint_produced`/`router.evaluate_overrode_hint` events + `RouterHintsProduced`/`RouterHintOverrides` counters, the `--router*` flags (default OFF, `--tree`-only), a 14-step TDD table, a scope fence, and a lock re-verification. Four code-vs-doc mismatches in the iter-4 §2.10.3 sketch found and CORRECTED (§4d.0): (1) `RetrieveAcross` must NOT go on the `Store` interface — that breaks `pkg/curation`/`pkg/adapter/curated` + the `var _ Store` assertion; it's a `*FileStore` method + an optional `AcrossRetriever` the router type-asserts; (2) `Exemplar.Action` is a free `string` so the router needs a `validPlaybook` filter; (3) the hook slots at runner.go:416 inside `resolve`, before `adapter.Evaluate`, and `env.Evaluate` is a NIL `*Evaluate` on entry — so Option B can't write into it; (4) v0 is Option A (steering text into `env.Input.Content`, which exists) only. Scoped to ONE PR off `main`; no embeddings, no model-tier routing, no `Store` change, no experiment RUN.**

**Prior state (iteration 6):** **Iteration 6 turned build-plan Phase 1 into a PR-ready, diff-level, TDD-ordered Go spec — §4b "Thread B — PR-ready spec" — checked against the *real* current source: full `pkg/semanticentropy` package (stdlib-only `Clusterer`/`ExactMatch`/`Entropy`/`Confidence` with the `H=−Σp ln p`, `conf=1−H/ln(N)`, N<2/N=0 edge cases), the additive `Answer.SEConfidence` diff (and its forced lockstep `answerJSON` twin), the ~10-line `Vote` population, the `calibration.ECE`/`Brier`/reliability-slope offline scorer, and the `--cope-confidence-source self|semantic-entropy` flag on `Coping`. Three code-vs-doc mismatches the iter-4/5 sketches got wrong are flagged and CORRECTED in §4b.0: (1) `answerJSON(or.Answer)` is a direct struct conversion so the two structs must change in lockstep; (2) the SE normalization base is the vote total Σ n_c, NOT `successes` (which includes empty-extraction attempts the histogram drops); (3) the confidence-source switch belongs on `Coping`, not `pkg/cope` (the rule table stays a pure `Decide(conf,stakes)`). Scoped to ONE PR off `main`, TDD-ordered, with an explicit not-in-scope list (no NLI/embedding clusterer, no blend, no Thread A, no experiment RUN).**

**Prior state (iteration 5):** Iteration 1 was a breadth survey of 7 routing mechanisms vs. the locked architecture; iteration 2 ran the head-on blended-cost comparison (§2.8) and resolved the central fork: **"dense" must mean a mid-tier model, never the frontier — and even then it loses to the locked cheap-local path on cost; its only wins are the quality-bound niche or operational simplicity.** Iteration 3 (§2.9) specifies the **models-as-tools calling protocol** — expressible as the root AP running `plan`→`process`→`compose` (NO new playbook, NO new node type), provided the orchestrator stays stateless-per-AP (the stateful re-planning loop is tensioned + deferred). Iteration 4 turns the two lock-compatible threads into **buildable Go interface sketches against the real code**: §2.10 the **kNN/semantic playbook-hint router** over `pkg/exemplar` (`RetrieveAcross` → `pkg/router.ExemplarRouter` → advisory `Hint` seeded into Evaluate, never replacing it) and §2.11 **discrete semantic entropy over Vote's N attempts** feeding `cope.RuleTable.Decide` as the existing confidence scalar. **Both come back zero-hard-lock-tension** (router has one soft dependency tension deferred behind a measurement gate; SE is fully lock-compatible). Iteration 5 (§2.12) **converts the doc from design into a decision procedure**: three null hypotheses — the doc's own conclusions turned into targets to attack — **H0-cost** (dense mid-tier buys no quality over local Vote), **H0-router** (the kNN hint is inert / no better than Evaluate), **H0-SE** (semantic entropy no better-calibrated than self-reported confidence), each with exact `cmd/bench`/`cmd/phase1` arms, the existing telemetry it reads, a decision threshold tuned to the n≈198-case noise floor, the minimal additive instrumentation, an offline ECE/Brier/reliability-slope calibration scorer, an ordered 10-step run plan, and a falsification outcome table. With all four priorities now addressed, §4a gives the **build order**: Thread B (SE) first (cheapest, zero tension, highest-power test), then H0-cost (no new code), then Thread A (router) behind its inertness gate, then the free-form keep-gate; dense-as-packaging deferred until an operator need appears.

**This doc does NOT resolve any locked decision.** Where the new direction conflicts with a lock in `../CLAUDE.md`, the tension is flagged explicitly and left open. Treat this as a research scratchpad feeding a possible future architectural fork, not a commitment.

---

## 1. Framing: the "dense model calls focused models as tools" idea

The current Oscillitron runtime is a **call-tree of action-potential (AP) invocations** (LOCKED 2026-05-18). A complex problem is solved by recursively invoking a *uniform* AP-handling workflow; each AP runs **evaluate → execute** (LOCKED 2026-05-19), where evaluate is a **cheap-local-first** LLM call that picks one of five playbooks (`plan`, `process`, `critique`, `verify_grounded`, `compose`). Frontier model use is *restricted*: only the `delegate` runtime-escalation gate (critic failed past retry budget) and sampled `verify_judge` audits may touch a frontier model. Specialization lives in **per-action playbook substrate**, not node types. The "router" of older drafts collapsed to a brain-function **dispatcher** — there are no persistent routing edges or static routing weights.

**The new direction** inverts the cost topology. Instead of cheap-local-first with frontier reserved at the leaves/escalation, a **dense (frontier/orchestrator) model sits at the center** and exposes the cheap/focused models as **callable tools**. The frontier model does the planning, the routing, and the recomposition; the cheap models are invoked as tool-calls to do narrow, bounded work (extract this field, draft this paragraph, run this check). This is the HuggingGPT/JARVIS "controller LLM + expert models as tools" shape, and the LangGraph "supervisor delegates to specialist workers" shape.

### The central tension (genuinely open)

| Axis | Existing locked model | New dense-orchestrator framing |
|---|---|---|
| Who plans/decomposes | cheap-local Evaluate picks `plan`; planning runs on the cheap substrate | the **frontier** model plans and decomposes |
| Who routes | uniform Evaluate step (cheap), playbook-keyed | the **frontier** model decides which tool/model to call |
| When frontier runs | only `delegate` escalation + sampled `verify_judge` audits | frontier runs on **every** task as the orchestrator |
| Where specialization lives | per-action playbook substrate (data layer) | tool definitions / model cards in the orchestrator's prompt; specialist models behind tools |
| Cost shape | dominated by many cheap calls; frontier is a rare tripwire | dominated by the central frontier call; cheap calls are the leaves |

**The load-bearing conflict:** the project's entire cost thesis is "production-grade quality at a fraction of the cost" by running cheap substrate and touching frontier rarely. The dense-orchestrator framing pays for a frontier call on *every* request just to do the routing/planning. The routing-cost research (§2.7 below) is blunt about this: **if your router is itself a frontier call, you have already paid most of the cost you were trying to save.**

**Iteration 2 resolved this with arithmetic (§2.8 below).** The short version, which the rest of the doc now treats as settled:

- **"Dense" = frontier (Sonnet/Opus orchestrator on every request) LOSES on cost** against the locked cheap-local-Evaluate path, by a wide margin, on both the Phase-1 and GPQA workloads. The frontier-per-request orchestrator costs **roughly the same as or more than the frontier single-call baseline** it was supposed to undercut, because the orchestration overhead (plan + critique + recompose) is *added on top of* an already-frontier-priced per-token rate. It only wins in one narrow niche (§2.8.4): tasks where a single frontier call genuinely fails the quality bar and orchestration is the *only* way to pass it — i.e., quality-bound, not cost-bound, problems.
- **"Dense" = mid-tier (a hosted open 70B-class model, or Haiku, as the orchestrator) is VIABLE** and preserves the cost thesis, *if and only if* the leaves are pushed down to local/near-zero substrate. A mid-tier orchestrator at ~$0.4–0.9/Mtok blended is **5–35× cheaper per token than the frontier**, so paying for orchestration on every request still nets out far under a frontier single-call. This is the only "dense" reading that survives.
- **The cheapest of all** remains the LOCKED path: cheap-local Evaluate (~$0 marginal on owned hardware) on every AP, frontier only at the rare `delegate` gate + sampled `verify_judge`. Nothing the dense framing offers beats it on cost; the dense framing's case has to be made on *quality* or *operational simplicity*, never on cost.

So the decision is: **if you adopt "dense" at all, it must mean a mid-tier model, never the frontier — and even then it is a quality/simplicity play layered on cheap leaves, not a cost win over the existing architecture.**

---

## 2. Routing mechanisms surveyed (iteration 1: breadth)

For each: **what it is**, **the routing signal**, **the cost/quality tradeoff**, and **key parameters**. Mapping onto Oscillitron is collected in §3.

### 2.1 LLM cascade / cascading routing (cheap-first, escalate-on-low-confidence)

**What it is.** Query the cheapest model first; accept its answer if a scoring/confidence function clears a threshold, else escalate to a more expensive model. The escalation decision happens *after* generation (post-hoc), unlike a router that decides *before* generation.

**Routing signal — the escalation scorer.** This is the load-bearing piece and it varies:
- **FrugalGPT** (Chen, Zaharia, Zou, Stanford; arXiv:2305.05176): a **separate fine-tuned DistilBERT regression scorer** `g(query, answer) → [0,1]` predicts answer reliability; accept if `g > τ`, else escalate. Cascade order and per-stage thresholds are learned on a validation set. Reported up to **98% cost reduction at GPT-4-matched quality** (with caching + approximation contributing).
- **AutoMix** (arXiv:2310.12963): small model self-verifies (entailment: does the answer follow from context?); a **POMDP router** treats query difficulty as hidden state, self-verification confidence as a noisy observation, and accept/escalate as the action. Learns a robust policy from as few as ~50 examples. ~2x+ cost reduction.
- **Cascade-Aware Training** (arXiv:2406.00060): trains the small model to *know its place in the cascade* via a token-loss that masks tokens neither model gets right; uses intrinsic log-likelihood confidence for deferral — **no external scorer**. ~13% FLOPs cut at fixed accuracy.
- **GATEKEEPER** (arXiv:2502.19335): hybrid fine-tuning loss that *calibrates* small-model confidence (keep confidence on correct, push toward uniform on incorrect) so a simple threshold defers well.
- **Speculative cascades** (Google Research, 2025): merges cascade deferral with speculative decoding — token-level accept/defer instead of whole-query.

**Cost/quality.** Strong when most queries are easy (cheap model handles them, expensive model amortized over the hard tail). Pays a latency cost (sequential attempts) and, for FrugalGPT-style, requires training the scorer. Two-stage cascades are often near-optimal; multi-stage rarely helps (decision-theoretic result, arXiv:2605.06350).

**Key parameters.** Per-stage threshold vector τ (the cost-quality dial), scorer training data, cascade ordering (cheap→expensive).

### 2.2 Learned routers / cost-quality routers (RouteLLM-style)

**What it is.** A router predicts, *from the query alone, before any model runs*, which model tier is sufficient. Pre-generation, not post-hoc.

**Routing signal & training.** RouteLLM (LMSYS, Ong et al.; arXiv:2406.18665) trains four router variants on **Chatbot Arena pairwise preference data + LLM-judge-augmented synthetic labels** to predict `P(strong model wins | query)`:
1. **Similarity-weighted ranking** — cosine-weighted Bradley-Terry over historical queries.
2. **Matrix factorization** — low-rank `P(strong>weak) = σ(uᵀv)` over model/query embeddings.
3. **BERT classifier** — sentence-transformer embed → binary win classifier.
4. **Causal-LLM classifier** — fine-tuned small LLM emits the routing decision.

A threshold α dials cost-vs-quality. Reported: **95% of GPT-4 quality at ~14% GPT-4 calls** on MT-Bench (≈3.66x savings); similar on GSM8K/MMLU.

**Adjacent work.** Hybrid LLM (Ding et al., ICLR 2024; arXiv:2404.14618) — a DeBERTa router predicts the small-vs-large *quality gap* and routes on a tunable tolerance τ (~40% fewer expensive calls). RouterDC (NeurIPS 2024) — dual-contrastive query/model embeddings. BEST-Route (Microsoft; arXiv:2506.22716) — routes both *model* and *number of samples* (best-of-N on a cheap model can beat one strong call), up to 60% savings at <1% quality drop. **kNN routers** (arXiv:2505.12601) — non-parametric: embed query, vote over which model won on the k nearest historical queries; **often matches or beats learned parametric routers** with far less training data.

**Cost/quality.** Routing overhead is tiny if the router is cheap (BERT/kNN/embedding). The catch is **training-data dependence** (you need query→model-performance labels) and **poor generalization to route types not seen in training**.

### 2.3 Confidence- / difficulty-based routing and calibration signals

**What it is.** Use a model's own uncertainty (or a cheap difficulty estimate) to decide accept-vs-escalate or which tier to route to.

**Signals, reliability, and cost (this is the crux):**
- **Token logprobs / margin / entropy** — single pass, ~free. But raw logprobs are **overconfident and poorly calibrated** (ECE often high); margin (top-1 vs top-2) is a weak-but-usable signal.
- **Verbalized confidence** ("I'm 90% sure") — single pass, but **systematically overconfident**, worsened by RLHF. Worse than logprobs in some studies.
- **Semantic entropy** — sample N (10–20) responses, cluster by meaning, entropy over clusters. **Best-calibrated** of the cheap signals (ECE 0.05–0.14) but costs **10–20x** (multi-sample) and **cannot run pre-generation**.
- **Self-consistency / ensemble agreement** — agreement across samples or across N cheap models; strong calibration (AUROC ~72–89). Costs the N samples.
- **p(True)** — generate then ask "is this correct? yes/no", read P(yes). ~2x cost; well-calibrated on MCQ, weaker free-form.
- **Confidence tokens** (Self-REF; arXiv:2410.13284) — train special tokens that encode confidence; single-pass, better-calibrated than logprobs/verbalized.
- **Difficulty predictors** — lightweight classifier/probe on hidden states predicts query difficulty *up front* (single pass, ~free); LLMs internally encode perceived difficulty. VAE-based difficulty (DAAO; arXiv:2509.11079) self-updates from workflow success/failure — ~11% accuracy up at ~36% cost down.

**Key takeaway.** Cheap single-pass confidence (logprobs/verbalized) is unreliable alone; reliable confidence (semantic entropy, ensemble) costs multiple samples. There is no free, well-calibrated confidence signal — this directly constrains any confidence-gated escalation.

### 2.4 Model-as-tool dispatch (strong model exposes weaker specialists as tools)

**What it is.** The framing closest to this project's new direction. A controller LLM plans, then dispatches subtasks to specialist models exposed as tools (via model cards / tool schemas in its prompt), then recomposes their outputs.

**Canonical mechanisms.**
- **HuggingGPT / JARVIS** (arXiv:2303.17580): ChatGPT as project manager. Four stages — **Task Planning** (decompose intent into a DAG), **Model Selection** (read textual model-card descriptions of candidate HF models, pick via structured output), **Task Execution**, **Response Generation** (recompose). Dynamic candidate selection keeps the prompt from listing all 400+ models.
- **LangGraph supervisor**: a supervisor LLM classifies intent and routes to specialist worker agents; control returns to the supervisor after each. Reported ~94% routing accuracy (routing is its sole job) at a latency/cost penalty (2 calls vs 1; ~4.2s vs 2.8s single-domain).
- **Native function-calling**: tools described by JSON schema; model auto-selects and fills arguments. Note: models **miscalibrate tool necessity** (call too much or too little) — "To Call or Not to Call" (arXiv:2605.00737).

**Dispatch signal.** The orchestrator's own reasoning over tool/model-card descriptions in-context (not a learned router). Cheap if the orchestrator is cheap; expensive if it's frontier.

**When it wins / loses.** Wins when decomposition is non-obvious, routing accuracy is critical, and specialists are cheap & abundant (orchestrator overhead amortizes over many cheap subtask calls). Loses when routing is straightforward (a cheap classifier/semantic router suffices), latency SLA is tight, or — critically — when the orchestrator *is* the frontier and the task could've been answered by one cheap call. Hybrid cascade routing now beats pure orchestration on many cost benchmarks. A hierarchical supervisor-worker reached ~98.5% of best F1 at ~61% of cost (MAS-Orchestra; arXiv:2601.14652).

### 2.5 Mixture-of-experts routing (token/sequence-level gating)

**What it is.** Sparse architecture: a learned **gating network** routes each *token* to top-k of N expert FFNs inside one trained network (Switch Transformer k=1; Mixtral k=2 of 8). Load-balancing auxiliary loss + expert-capacity caps keep experts evenly used; router z-loss stabilizes training.

**What transfers to model-level orchestration:**
- **Top-k selection** as a ranking/priority mechanism over a pool.
- **Load balancing / capacity** — distributing query load evenly across specialist instances to avoid hotspots (maps onto Oscillitron's VRAM governor + concurrency, §3).
- **Gating as a learned function** of an input representation, rather than hand rules.

**What does NOT transfer:**
- **Token-level granularity** — you can't partially route a whole query to two specialists and merge token-by-token without re-decoding. Model-level routing is query-scoped.
- **Differentiable joint training** — MoE experts co-train with the gate via gradients; specialist models are trained/operated independently. Oscillitron explicitly does **no weight updates**.
- **Expert homogeneity** — MoE experts share architecture/interface; specialist models are heterogeneous.

MoE is the wrong granularity for Oscillitron but a useful source of *vocabulary* (gating, top-k, load balance, capacity).

### 2.6 Semantic / embedding-based routing

**What it is.** Embed the query, match it against pre-defined routes (each route = 10–20 example utterances or a centroid) by cosine similarity, pick the best route above a threshold; fall back to a default/LLM if nothing clears. Structurally identical to RAG retrieval, but it retrieves a *route* instead of a document. Reference impl: Aurelio semantic-router.

**Routing signal.** Frozen embedding model (sentence-transformers / BERT / embed-3) + cosine similarity to route reference embeddings. **No LLM call** for the routing decision.

**Cost/quality.** Single embedding call (~5–50 ms, ~no tokens) — order(s) of magnitude cheaper than LLM routing; ~90% accuracy on well-defined routes; threshold is the load-bearing hyperparameter (plus an optional top-1-vs-top-2 margin check). Limited to routes you've defined; ambiguous queries need a fallback.

### 2.7 LLM-as-router vs heuristic/classifier router (the cost-of-routing tradeoff)

**The central tradeoff for this project.** Routing *overhead* is negligible as a fraction of a frontier generation **only if the router is cheap**. If the router itself is a frontier call, you've spent most of the money you were trying to save.

| Router | ~Cost/req | Latency | Accuracy | Generalization |
|---|---|---|---|---|
| Frontier LLM (GPT-4o/Opus) | $0.003–0.01 | 1–5 s | 90%+ | excellent, novel queries |
| Small LLM (0.5B fine-tuned) | ~$0.001 | ~430 ms | 85–95% | good, limited |
| Fine-tuned BERT | ~$0.0001–0.001 | 30–50 ms | 85–95% | poor (domain-bound) |
| Semantic embed + kNN | ~$0.00001 | 5–20 ms | 70–85% | route-bound |
| Rule/regex | free | <1 ms | 60–80% | very limited |

**Hybrid (the practical answer).** Cheap classifier/embedding router first; escalate to an LLM-router only on low-confidence cases. Cuts expensive-judge calls to ~20–30% of traffic, ~40–70% cost reduction, 90–95% of frontier quality. **The danger the dense framing must avoid:** using a frontier model as the router-of-everything defeats the cost thesis unless the alternative was "use that frontier for everything anyway."

---

## 2.8 Head-on blended-cost comparison: dense-orchestrator-per-request vs. the locked cheap-Evaluate path (ITERATION 2 — the falsification test)

This is the load-bearing analysis. It answers open question #1 (is "dense" the frontier or a mid-tier?) and #7 (cost-model the inversion head-to-head) using real current pricing and the project's own two concrete workloads. **All token assumptions are explicit and the arithmetic is shown** so the numbers can be re-derived or contested.

### 2.8.1 Current per-Mtok pricing (May/June 2026, verified — see Sources §5)

| Tier | Model | Input $/Mtok | Output $/Mtok | Blended* $/Mtok |
|---|---|---|---|---|
| **Frontier** | Claude Opus 4.8 | 5.00 | 25.00 | ~11.00 |
| **Frontier** | Claude Sonnet 4.6 | 3.00 | 15.00 | ~6.60 |
| **Mid-tier (hosted)** | Claude Haiku 4.5 | 1.00 | 5.00 | ~2.20 |
| **Mid-tier (hosted)** | GPT-4.1 mini | 0.40 | 1.60 | ~0.76 |
| **Mid-tier (hosted)** | GPT-4o mini | 0.15 | 0.60 | ~0.29 |
| **Mid-tier (hosted-open)** | Llama-3.3-70B (Together/Fireworks) | ~0.60–0.90 | ~0.79–0.90 | ~0.75 |
| **Mid-tier (hosted-open)** | Qwen-2.5-72B (DeepInfra/Alibaba) | ~0.35 | ~0.40 | ~0.38 |
| **Cheap-local** | open ≤14B via Ollama/vLLM on owned GPU | ~0 marginal | ~0 marginal | **~0** (amortized HW + electricity only) |

*Blended uses the project's own 70/30 input/output mix (`cmd/phase1` `report()` uses exactly this split: `tokens*7/10` in, `tokens*3/10` out). Blended = 0.7·in + 0.3·out.

The project's `pkg/cost` already models exactly this: `Pricing{InputUSDPerMTok, OutputUSDPerMTok}` per model, a `frontier` baseline for the counterfactual ledger, plus `HardwarePricing{USDPerHour}` for the local-substrate case where there is no API bill (cost = wall-clock × hourly rate). The `Tracker` keeps `TotalActualUSD` and `TotalFrontierUSD` in parallel and reports `SavingsRatio`. The numbers below are what that ledger would print.

> Note: `cmd/phase1/main.go` `defaultPricing` currently has Haiku at `0.80/4.00` and the **`claude-opus-4-7`** entry at `15.00/75.00` — both **stale** vs. verified current rates (Haiku `1.00/5.00`; Opus 4.7 & 4.8 both `5.00/25.00`). **The fix and its corrected model key (the entry is keyed `claude-opus-4-7`, NOT `claude-opus-4-8` as earlier prose implied) are fully verified and spec'd as a standalone v1.0.0 patch PR in §4c.1 / §4c.0 (iteration 7).** The analysis below uses the verified current rates.

### 2.8.2 Workload A — Phase-1 email drafting (the kill-or-proceed workload)

The actual orchestrator shape from `cmd/phase1`: **1 plan + N drafts + (N−1) synth + 1 critique + 0–1 revise**. Take the nuanced default N=3 → 1 + 3 + 2 + 1 + 1 = **8 calls**. Email tasks are short; assume per-call **600 in / 250 out** (drafts run longer, plan/critique shorter — averages out). Frontier single-call baseline: **1 call, 600 in / 350 out**.

Per-request token totals:
- **Orchestrated path:** 8 calls × 850 tok = **6,800 tok** (4,800 in / 2,000 out).
- **Frontier single-call baseline:** **950 tok** (600 in / 350 out).

Cost per request, by who runs the orchestration:

| Design | Orchestrator substrate | Arithmetic | $/request |
|---|---|---|---|
| **(B) LOCKED path** | cheap-local (Ollama, ~$0) leaves; frontier only on rare delegate | 6,800 tok × ~$0 + (delegate ~5% of requests × 950 tok × Sonnet) | **~$0.0003** (≈ delegate amortized) |
| **(A-mid) Dense, mid-tier** | Haiku orchestrator on all 8 calls | (4,800·$1 + 2,000·$5)/1e6 = $0.0048 + $0.010 | **$0.0148** |
| **(A-mid2) Dense, cheap-hosted-mid** | GPT-4o-mini orchestrator on all 8 calls | (4,800·$0.15 + 2,000·$0.60)/1e6 | **$0.0019** |
| **(A-frontier) Dense, frontier** | Sonnet orchestrator on all 8 calls | (4,800·$3 + 2,000·$15)/1e6 = $0.0144 + $0.030 | **$0.0444** |
| — Frontier single-call baseline (reference) | Sonnet, 1 call | (600·$3 + 350·$15)/1e6 | **$0.0071** |

**Read this carefully.** The frontier *single call* costs **$0.0071**. The **dense frontier orchestrator costs $0.0444 — 6.3× MORE than just calling the frontier once.** Orchestration multiplies token volume ~7× (8 calls of 850 tok vs. 1 call of 950 tok), and at a frontier per-token rate that multiplication is paid in full. **Dense-frontier doesn't just lose to the cheap path; it loses to the thing it was supposed to replace.** The only way frontier-orchestration is ever rational is if the 7× token spend buys quality a single frontier call cannot reach (§2.8.4).

The **dense mid-tier** variants land at **$0.0019–0.0148** — at or below the $0.0071 frontier single-call, i.e. the orchestration overhead is absorbed because the per-token rate dropped 3–35×. GPT-4o-mini-as-orchestrator ($0.0019) is **3.7× cheaper than one frontier call** *and* gets the orchestration (ensemble + critique + revise) Phase-1 found to lift quality. That is a real, defensible "dense" design — but it is mid-tier, and it still loses to the locked ~$0 local path by ~6×.

### 2.8.3 Workload B — GPQA Diamond (Vote and Tree orchestrators)

GPQA questions are long with reasoning output. Assume per-call **900 in / 600 out = 1,500 tok**. `pkg/benchmark/orchestrator`:
- **Vote (N=5):** 5 parallel attempts, majority vote, no synth call → **5 × 1,500 = 7,500 tok**.
- **Tree:** plan + ~3 process + 2 synth reductions ≈ 6 calls → **6 × 1,500 = 9,000 tok**.
- **Single (frontier baseline):** 1 × 1,500 tok.

Vote-5 cost per request:

| Design | Substrate | Arithmetic (Vote-5, 7,500 tok = 4,500 in / 3,000 out) | $/request |
|---|---|---|---|
| **(B) LOCKED / local Vote-5** | cheap-local ~$0 | 7,500 × ~$0 | **~$0** |
| **(A-mid) mid-tier Vote-5** | Qwen-2.5-72B | (4,500·$0.35 + 3,000·$0.40)/1e6 | **$0.0029** |
| **(A-mid) mid-tier Vote-5** | Haiku | (4,500·$1 + 3,000·$5)/1e6 | **$0.0195** |
| **(A-frontier) frontier Vote-5** | Sonnet | (4,500·$3 + 3,000·$15)/1e6 = $0.0135 + $0.045 | **$0.0585** |
| — Frontier Single (reference) | Sonnet, 1 call (900 in/600 out) | (900·$3 + 600·$15)/1e6 | **$0.0117** |
| — Frontier Single (reference) | Opus, 1 call | (900·$5 + 600·$25)/1e6 | **$0.0195** |

Same shape, sharper: **frontier Vote-5 ($0.0585) is 5× a single Sonnet call ($0.0117)** and 3× a single Opus call. The N-way fan-out is the multiplier; at frontier rates it is brutal. **Mid-tier Vote-5 on Qwen-72B ($0.0029) is 4× cheaper than a single Sonnet call** while keeping the ensemble — and the project's own v1 result is that vote+critique on cheap substrate ≈ frontier quality. That is precisely the existing thesis; "dense mid-tier" here is just *the existing Vote orchestrator with a hosted mid-tier substrate instead of local*, which is a deployment choice the codebase already supports (`--orchestrator-substrate`).

### 2.8.4 The one niche where dense-frontier wins

Dense-frontier-orchestrator is rational only when **all** of these hold:
1. A single frontier call **fails the quality bar** (the task is genuinely hard — the 7× token spend is buying reachable-only-via-orchestration quality, not redundancy).
2. The cheap/mid substrate **cannot** produce leaves good enough for a frontier orchestrator to recompose into a passing answer (otherwise push the leaves down — that's the mid-tier design, cheaper).
3. Latency budget tolerates the multi-call tree.
4. You were going to spend frontier money anyway and orchestration *raises the ceiling* rather than *lowering the floor cost*.

This is the **quality-bound, cost-insensitive** regime: frontier research assistants, high-stakes single answers where $0.04 vs $0.007 is irrelevant but a wrong answer is expensive. It is explicitly **not** Oscillitron's Phase-1 thesis ("production-grade quality at a *fraction of the cost*"). For that thesis, dense-frontier is disqualified on cost and the comparison isn't close.

### 2.8.5 Recommendation (retires open question #1)

1. **"Dense" must mean mid-tier, never frontier.** Frontier-orchestrator-per-request is dominated on cost by both the locked local path (~$0) *and* the frontier single-call it meant to replace (it costs 5–6× more than just calling the frontier once). Reject it for the cost thesis. Reserve it mentally for the quality-bound niche (§2.8.4), which is out of Phase-1 scope.
2. **Mid-tier dense is viable but is not a new cost win — it's the existing architecture with a hosted substrate.** A Haiku/GPT-4o-mini/Qwen-72B orchestrator running the existing plan→draft→critique→recompose tree lands at **$0.002–0.015/request**, at or under a frontier single call, while keeping the orchestration quality lift. But the locked cheap-**local** path beats it by ~5–6× because local marginal cost is ~$0. So the *only* reason to choose hosted-mid over local-cheap is operational (no GPU to own, no ops burden), not cost.
3. **The cost regime where dense beats the existing architecture: there isn't one.** Under every regime measured, ordering is `local-cheap (locked) < hosted-mid-tier < frontier-single < frontier-orchestrator`. Dense never takes the cost crown; the locked path already holds it. The honest conclusion the loop asked for: **dense loses on cost; its only win is (a) quality in the cost-insensitive niche, or (b) operational simplicity for operators who can't run local GPUs — and (b) still wants the orchestration to ride a *mid-tier*, not frontier, substrate.**
4. **What this means for the architecture:** the locks are *vindicated* by the arithmetic. Cheap-local Evaluate + frontier-only-at-delegate is the cost-optimal point. The dense direction's residual value is narrow: surfacing per-action specialists as *tool schemas* a mid-tier orchestrator can call (the §2.4 model-as-tool shape) — a **packaging/ergonomics** change, not a cost change, and one that can be expressed without breaking uniform-node (see open question #4, now the live thread). Iteration 3 designs that calling protocol concretely against the AP envelope in §2.9 — it is the only part of the dense idea that survived this section with its value intact.

---

## 2.9 The models-as-tools calling protocol on the AP envelope (ITERATION 3 — the surviving thread)

This section specifies, concretely, the one piece of the dense idea that survived §2.8: surfacing focused/cheap specialists as **callable tools** to a (mid-tier) orchestrator, expressed as a thin layer over the locked primitives — *not* a replacement for them. It answers open question #1 (the §4 list): does a tool-call become an `emit_subtree` sub-AP, the tool-name a playbook tag, the tool-result a `return_result` on the scope channel — and is that still the uniform node running a different playbook, or a privileged orchestrator node?

### 2.9.1 What "model-as-tool" means in the current literature (real research)

The framing has two distinct industrial shapes, and the distinction is load-bearing for the uniform-node verdict:

- **Native function-calling / tool-as-schema (Anthropic Messages API, OpenAI function calling).** A tool is a static declaration `{name, description, input_schema}` where `input_schema` is a JSON Schema (`type`, `properties`, `required`, optional `input_examples`). The model emits a `tool_use` content block carrying a `tool_use_id` + a JSON `input` validated against the schema; the caller runs the tool and returns a `tool_result` content block keyed by the same `tool_use_id`; the model resumes with the result in-context. `tool_choice` can *force* a particular tool, turning the model into a schema-validated router. (Anthropic "Define tools" / "Advanced tool use"; OpenAI function-calling.) **A "model-as-tool" under this shape is: wrap a focused model behind a tool whose `description` is its model-card and whose `input_schema` is its task contract — the orchestrator selects it by name and fills its arguments.** This is the HuggingGPT "model selection by reading textual model-card descriptions" mechanism, modernized into typed function-calls.

- **Orchestrator-workers via structured-output decomposition (Anthropic's own cookbook + production multi-agent research system).** The reference `orchestrator_workers.ipynb` does **not** use function-calling tools at all. The orchestrator emits a structured list of subtasks — each `{type, description}` — via structured output (XML in the cookbook; equivalently JSON); a worker is invoked per subtask with `{original_task, task_type, task_description}`; results accumulate into a list `[{type, description, result}, …]` and a final synthesis call recomposes them. Anthropic's production Research system spawns subagents dynamically with `{objective, output_format, tool/source guidance, boundaries}`, each in its own isolated context window, returning results that bubble back to a lead agent — and **the lead agent persists its plan to Memory across many subagent calls** so it doesn't lose strategic direction when its context window saturates. (Anthropic "Building Effective Agents"; "How we built our multi-agent research system".)

**The key research finding for this project:** the two shapes converge on the *same data flow* (decompose → dispatch-per-subtask → collect → synthesize), but differ on **who holds state**. The function-calling shape lets the orchestrator carry the whole tool-call conversation in one context. Anthropic's orchestrator-workers cookbook is *stateless per worker* — each worker gets a self-contained `{task, type, description}` and returns a result; the orchestrator's only persistent state is the subtask list + the final synthesis. **Oscillitron's locked `plan`→`emit_subtree`→`process`→`compose` IS the stateless cookbook shape, almost field-for-field.** That is the graft point, and it is the shape that preserves uniform-node (see §2.9.4).

### 2.9.2 The tool schema, and how it maps to the envelope

A focused specialist exposed as a tool is, in Anthropic-tool terms:

```jsonc
{
  "name": "process.extract_decline_rationale",   // <playbook>.<specialist-action-tag>
  "description": "Focused drafter for polite-decline email bodies. Input: the task + tone constraints. Output: a single drafted body with a self-reported confidence.",
  "input_schema": {                               // == the child AP's contract
    "type": "object",
    "properties": {
      "task":         { "type": "string" },       // → SubAPSeed.Input.Content (Kind:"task")
      "constraints":  { "type": "string" },        // → folded into Input.Content or OutputSchema
      "output_shape": { "type": "string" }         // → SubAPSeed.OutputSchema
    },
    "required": ["task"]
  }
}
```

The envelope already carries every field this schema needs, with **one structural gap**:

| Tool-schema element | Envelope home today | Gap? |
|---|---|---|
| tool `name` (which specialist) | `SubAPSeed` has **no playbook/specialist field** — child playbook is picked by the child's *own* Evaluate | **YES — see §2.9.5** |
| tool `description` (model-card) | per-action playbook substrate (`pkg/exemplar`, playbook prompt) — not on the envelope, lives in the data layer | no (substrate, as locked) |
| `input_schema.task` | `SubAPSeed.Input` (`Payload{Kind:"task", Content}`) | no |
| `input_schema.output_shape` | `SubAPSeed.OutputSchema` | no |
| `tool_use_id` (correlates call→result) | `Envelope.ID` of the child AP | no |
| `tool_result` (the returned value) | `Execute.ReturnResult.Result` (`Payload`) bubbling up the scope channel | no |
| `tool_choice: force` (router pin) | `Tree` orchestrator already forces `PlaybookPlan` on the root via the bench; same lever | no |

So **the tool-result half of the protocol is a perfect fit** — a child AP's `ReturnResult` IS a `tool_result`, correlated by the child's `Envelope.ID` exactly as `tool_use_id` correlates a `tool_use`/`tool_result` pair. The only friction is the **tool-name → which-specialist** half, because `SubAPSeed` deliberately omits a playbook tag (uniform-node: the child picks its own playbook at Evaluate). §2.9.5 resolves how to express specialist selection without adding a node type.

### 2.9.3 Worked example, end to end (Phase-1 email decline)

Task: *"Decline the vendor's proposal but keep the door open for next quarter."* A mid-tier orchestrator (Haiku, per §2.8's mid-tier-only verdict) runs the root AP. Field-level flow:

1. **Root AP — Evaluate.** Haiku runs the evaluate step on the root envelope (`Input.Kind:"task"`). It picks `Evaluate.Playbook = plan`. (`Tree` orchestrator can force this via the existing `PlaybookPlan` pin, exactly as it does in `pkg/benchmark/orchestrator.Tree` today — that pin is the `tool_choice:force` analog and it already exists.)

2. **Root AP — Execute (`plan`, category `emit_subtree`).** Haiku produces `Execute.EmitSubtree`:
   ```jsonc
   {
     "sub_aps": [
       { "input": {"kind":"task","content":"Draft a polite-but-firm decline body, warm close, leave Q3 door open."},
         "output_schema": "{ body: string, confidence: number }" },
       { "input": {"kind":"task","content":"Draft a decline body emphasizing budget-cycle timing, no commitment."},
         "output_schema": "{ body: string, confidence: number }" }
     ],
     "recompose": "pairwise"
   }
   ```
   This is **the orchestrator's "tool call" list** — each `SubAPSeed` is one model-as-tool invocation. The "tool name" lives in the `Input.Content` framing + `OutputSchema` contract (the specialist's task description), NOT a separate field — that is the §2.9.5 decision.

3. **Runner dispatches the sub-APs** in randomized sibling order (locked), each a fresh child envelope via `NewChild` (stamps `ParentID`, `RootID`, `Path`, `ScopeHandle`, derives `Budget`). Each child's `Envelope.ID` is its `tool_use_id`.

4. **Each child AP — Evaluate→Execute.** A child runs *the same uniform workflow*: its own Evaluate step picks `process` (a focused drafting task), its Execute returns `Execute.ReturnResult = {result: {kind:"result", content:"<drafted body>"}, confidence: 0.78}`. This `ReturnResult` **is the `tool_result`** for that child's `tool_use_id` (= the child's ID). It bubbles up the parent's scope channel exactly as the locked `return_result` category specifies.

5. **Recompose (`compose` / recomposer).** The parent's recomposer folds the two `ReturnResultPayload`s pairwise (`RecomposePairwise`), taking weakest-link confidence, into one combined body — the orchestrator's "synthesize tool results" step. In v0 the runner+recomposer own this; v1's compose-as-dispatched-AP would make it a `compose` AP pulling from the scope channel by `{scope_handle, expected_count}` (locked), which is the literal "collect N tool_results then synthesize" loop.

6. **Verifier (optional, orthogonal).** The verifier policy may inject a `critique` AP on each `return_result` child (locked phase-ramp) — this is the orchestrator-workers "check the worker output" step, and it already exists. Its `verifier_signal` goes to the runtime, never the next AP.

**Net:** the entire models-as-tools call — orchestrator plans, calls N focused specialists as tools, collects their results, synthesizes — is *exactly* the locked `plan → emit_subtree → process(×N) → compose` tree. No primitive is missing except the tool-name→specialist binding (§2.9.5).

### 2.9.4 Uniform-node verdict: PRESERVED for the stateless shape; TENSIONED for the stateful shape

**The uniform-node lock says: ONE AP-handling workflow runs at every recursion level; specialization is in the playbook substrate keyed by action, NOT in distinct node types; APs are invocations siloed to one brain-function each; the call tree dissolves on return.**

**Verdict — PRESERVED, on one condition.** In the worked example the "orchestrator" is *not a distinguished node*. It is the root AP running the `plan` playbook — the same Evaluate→Execute workflow every child runs. The children are the same workflow running `process`. "Tool name = which specialist" reduces to "which playbook + which per-action substrate the child's Evaluate selects," which is data-layer specialization (locked: specialists-are-substrate), not a structural node type. The tool-call/tool-result correlation rides existing fields (child `ID` ↔ `tool_use_id`; `ReturnResult` ↔ `tool_result`). **So a faithful models-as-tools protocol does NOT require a privileged orchestrator node — it requires the `plan` playbook, which already exists. Uniform-node holds.**

**The condition — and the genuine tension.** This holds *only if the orchestrator is stateless per AP* — i.e., the root `plan` AP emits its whole subtree once and blocks on recomposition (locked: "parent blocks on subtree"), rather than running a **persistent tool-calling loop** that holds a growing conversation across many sequential tool calls and re-plans between each. The native function-calling shape (§2.9.1, first bullet) and Anthropic's production research system (which *persists the lead agent's plan to Memory across many subagent calls*) are the **stateful** shape: one long-lived orchestrator context that issues tool-call → reads result → reasons → issues next tool-call, N times, accumulating state.

That stateful loop **directly tensions two locks**:
- *"APs are invocations, siloed to one brain-function each … the call tree dissolves when it returns" (LOCKED 2026-05-18).* A persistent orchestrator that holds context across 10 sequential tool calls is **one invocation doing many brain-functions over a long-lived session** — the antithesis of siloed/dissolving. It is a Hermes-style agent loop, which is exactly what the wrap-not-fork lock pushes *into* Hermes, not into the orchestration layer.
- *"Two-step evaluate→execute, every AP" + "uniform node."* A re-planning loop is not evaluate-once-execute-once; it's a controller that interleaves planning and dispatch. Modeling it as "one AP" breaks the two-step shape; modeling it as "a chain of plan APs that each see prior results" requires feeding sibling/child results back *into a parent's next Evaluate*, which the locked dataflow forbids (verifier signals go to the runtime; results bubble to the recomposer; nothing flows back *into* a parent's planning step mid-tree).

**Both sides, honestly:**
- *For allowing the stateful loop:* it is what the strongest real systems do (Anthropic Research beat single-agent Opus by 90.2% on internal eval with exactly this lead-agent-holds-context shape). Re-planning between tool calls is more powerful than one-shot decomposition when the right next subtask depends on what the last one returned (genuinely sequential reasoning, not fan-out). One-shot `plan` cannot express "call specialist B *only if* specialist A's result looks like X."
- *Against:* it requires a privileged, long-lived orchestrator node holding cross-call state — a **new structural kind** the uniform-node lock exists to forbid — and it re-introduces the mid-tree result→parent feedback edge the locked dataflow deliberately cut. It also re-prices the orchestrator: a persistent loop re-sends the growing context on every tool call, inflating tokens (the §2.8 cost analysis assumed independent calls, not a snowballing conversation).

**Recommendation:** adopt **only the stateless shape** — models-as-tools = `plan`(one-shot decompose) → `emit_subtree` → `process` leaves → `compose`. It captures the fan-out case (the common one), preserves every lock, and needs no new playbook. **Explicitly DEFER the stateful re-planning loop** as a separate, lock-tensioning design problem (it belongs in the same bucket as "async sub-AP emission across subtrees," already deferred). Sequential-dependent decomposition in v0 is approximated by *nested* plans (a child can itself be a `plan` AP that decomposes further once it sees its slice), which stays within the dissolving call-tree. Where genuine "decide-next-call-from-last-result" is required, that is a Hermes-substrate concern (per-instance agent loop), not an orchestration-layer concern — consistent with wrap-not-fork.

### 2.9.5 New playbook, or reuse `plan`+`process`? — DECISION: reuse; do NOT add `delegate`/`dispatch`

The question: does models-as-tools need a new playbook (a `delegate`/`dispatch` action the Evaluate step can pick), or does it reuse the existing set?

**Recall `delegate` was explicitly CUT from the v0 set** as "a runtime escalation mechanism, not evaluate-visible" (LOCKED 2026-05-19). That cut is *reinforced* here, not reversed. Two different things wear the word "delegate":
- **Runtime escalation `delegate`** (the cut one) — the cope/inhibitor gate that sends a failed result to the frontier. Correctly a *runtime mechanism*, not an Evaluate choice. Stays cut.
- **"Dispatch a sub-AP to a specialist"** (the models-as-tools sense) — this is **already what `plan`+`emit_subtree` IS.** Plan's entire job is to emit sub-APs into the scope; that *is* the dispatch. Adding a `dispatch` playbook would duplicate `plan` with a different name, and would have to produce `emit_subtree` anyway (same Category). It buys nothing the `plan` playbook doesn't already buy.

**Envelope-level justification for reuse:** the three output Categories are fixed (`emit_subtree`/`return_result`/`verifier_signal`). A model-as-tool *call* is an `emit_subtree` (a `plan` already produces exactly that). A model-as-tool *result* is a `return_result` (a `process`/`compose` already produces exactly that). There is **no fourth Category** a tool-call/tool-result needs, therefore **no playbook gap.** Introducing `dispatch` would either (a) map to `emit_subtree` — redundant with `plan`, or (b) demand a new Category — which breaks the locked three-category model. Both are worse than reuse.

**The ONLY real change** the protocol wants is the §2.9.2 gap: a way for `plan` to *name which specialist* a `SubAPSeed` should resolve to, when the operator wants explicit pinning (the `tool_choice:force` analog) rather than letting the child's Evaluate pick. Two lock-compatible options, neither a new playbook:

- **Option A (zero schema change, recommended for v0):** encode the specialist hint in `SubAPSeed.Input.Content` / `OutputSchema` framing — the child's Evaluate reads it and picks the matching playbook + per-action substrate. This is pure prompt/substrate steering, touches no schema, and keeps the child genuinely uniform (it still *picks*; the parent only *suggests*). It is the §2.9.1 cookbook shape (`{task, type, description}` where `type` is just a hint string).
- **Option B (additive schema field, defer unless measured-needed):** add an optional `SubAPSeed.PlaybookHint Playbook` (omitempty) that, when set, the runner uses to *pin* the child's Evaluate (skip-or-bias the evaluate roll). This is the literal `tool_choice:force`. It is additive (schema-stable per the `ask-before-breaking-JSON` rule) but it **edges toward declaring the child's brain-function**, which is the thing uniform-node moved out of the structural layer. Flag: Option B is defensible only as a *bias*, never as a hard skip of Evaluate (skipping Evaluate would re-introduce a declared-node-type by the back door). **Recommend NOT building B until a measured case shows Option A's prompt-steering is too unreliable** — keep the burden of proof on the schema change.

**Decision (retires open question #1 of the §4 list): REUSE `plan`+`process`+`compose`. No new playbook. No new node type.** The models-as-tools protocol is the locked `plan → emit_subtree → process → compose` tree with the orchestrator surfaced as the root `plan` AP and specialists surfaced as per-action substrate behind `process` leaves. Specialist *pinning* (if ever needed) is Option A (prompt steering, zero schema) first, Option B (optional additive `PlaybookHint` bias) only if measured. The stateful re-planning loop is deferred (§2.9.4).

### 2.9.6 The tools-execute-after-verified-output lock: does mid-tree sub-inference violate it?

**The lock (Phase-1, 2026-05-21):** *tools and connectors execute AFTER the orchestrator has produced a verified output, never inside the call tree; side effects are a separate post-task layer that reads a verified result.*

**Verdict: calling a focused model mid-tree is CONSISTENT with the lock — the lock gates side-effecting connectors, not sub-inference.** The lock's load-bearing word is *side effects*. Its worked rationale is about email-sending, API calls, connectors that *act on the world* — those must wait for a verified proposal. A model-as-tool call is **pure inference that produces a proposal**; it changes nothing in the world, it only contributes a draft/extraction/check that flows up to recomposition and *then* through verification. The whole call tree (plan/process/compose/critique) is already "sub-inference inside the tree" — that is what the runtime *is*. Adding "the leaves are framed as tool-calls to focused specialists" does not introduce a side effect; the leaves are still `process` APs returning `return_result` payloads.

**The line stays bright, with one caveat to honor.** The lock blurs *only if* a "tool" is allowed to be a side-effecting connector (a real "send_email" / "query_database" tool) rather than a focused *model*. The protocol here is strictly **models**-as-tools — inference specialists. **Flag/guardrail:** the protocol MUST NOT be the wedge that smuggles connector-tools into the call tree. A connector ("send this email", "write this file") remains a post-verification side-effect layer, exactly as locked. The schema in §2.9.2 enforces this implicitly — a model-as-tool's `tool_result` is a `Payload` (text proposal), never a connector receipt. Concretely: the protocol's tools all resolve to `process`/`compose` APs (return_result); none resolve to a connector invocation. If a future "tool" needs to *act*, it does not enter the tree — it reads the verified `Result.ResolvedPayload` afterward, per the lock.

---

## 2.10 Thread A — kNN/semantic router over `pkg/exemplar` as a playbook *hint* for Evaluate (ITERATION 4)

This is the most lock-compatible surviving graft (handed to iteration 4 as new open question #1; §3.2/§3.6 + §2.9.5 Option A). It makes the §2.9.5 "Option A specialist hint" *automatic* instead of operator-hand-written, by reading the substrate already on disk. **It does not route models** (that was the dense inversion, killed in §2.8 on cost). It routes *playbooks* — it produces an **advisory hint** for the cheap-local Evaluate step, never a hard override. Evaluate still owns the pick (locked: every AP evaluates; cheap-local-first). The router is a *prior*, Evaluate is the *posterior*.

### 2.10.1 What it routes, and why a playbook hint (not a model/tier)

Three candidate things a router could output. Two are ruled out by locks; one survives:

- ~~**Which model tier** (cheap-local vs. mid vs. frontier)~~ — this is the RouteLLM/dense-inversion decision. **Ruled out:** §2.8 settled that frontier-per-request loses on cost and the locked path is cost-optimal; a tier router would re-open that with a net-new pre-Evaluate "which substrate" stage the uniform-node lock forbids (§3.2). Frontier stays at `delegate`/`verify_judge`.
- ~~**Which specialist *node***~~ — there are no node types (uniform-node). Ruled out by definition.
- **Which *playbook* the Evaluate step is likely to pick** (`plan` / `process` / `critique` / `verify_grounded` / `compose`), surfaced as a hint with a confidence + margin. **This survives** — it is data-layer (reads the per-action exemplar store), it changes no structure (the hint rides existing prompt/substrate steering, §2.9.5 Option A), and it requires no weight updates. It is the automation of "the operator framed the SubAPSeed to bias the child's Evaluate" — now the framing is computed from what *actually worked* on the k nearest past prompts.

Tie to §2.9 models-as-tools: when a `plan` AP emits a `SubAPSeed`, the router can pre-compute the most-likely playbook for that sub-task's `Input.Content` and write it into the §2.9.5-Option-A hint string — so the child's Evaluate starts with an evidence-backed prior instead of a cold read. That is the *only* place the router touches the models-as-tools protocol, and it touches it as steering text, not schema.

### 2.10.2 The retrieval substrate it reads, and the BM25-vs-embedding decision

The exemplar store is **per-action** (`FileStore.Dir/<action>.json`), and `Retrieve(ctx, action, prompt, k)` already does **BM25 over exemplar `Prompt`s**, ranked, with `Score`+`AddedAt` tiebreakers (read: `pkg/exemplar/bm25.go`, `exemplar.go`). Crucially, today's `Retrieve` requires you to **already know the action** — you pass it in. The router's job is the inverse: *given a prompt, predict the action.* So the router cannot call `Retrieve(action, …)` directly; it must rank exemplars **across all actions** and read off which action the nearest neighbors carry.

Two ways to get cross-action nearest neighbors, and the decision between them:

- **kNN-over-BM25 (zero new dependency, recommended for v0).** Add one method to the store that scores a query against *every* action's corpus and returns the global top-k with their `Action` labels. This reuses the exact BM25 index machinery already in `bm25.go` (`buildBM25Index`, `score`) — no embedding model, no new dep, stdlib-only (honors the stdlib-first rule). The kNN-beats-learned-routers result (arXiv:2505.12601) is specifically that *non-parametric neighbor voting matches or beats trained parametric routers with far less data* — and BM25 lexical neighbors are a legitimate non-parametric retriever. The weakness is lexical: BM25 misses paraphrase ("decline politely" vs. "turn down graciously"). For the *playbook*-hint task this is tolerable, because playbook choice correlates with surface structure (a "draft X" prompt → `process`; a "is this correct?" prompt → `critique`) more than with deep semantics.
- **kNN-over-embeddings (one frozen-model dependency, defer until BM25 is measured insufficient).** Swap/augment BM25 with cosine over sentence embeddings (the Aurelio semantic-router / vLLM-semantic-router shape; kNN-LM datastore shape, arXiv:1911.00172). Catches paraphrase; costs a frozen embedding model dependency (a real break from stdlib-first). **Lock verdict on the embedding model (the flagged tension):** a *frozen* embedding model is **retrieval infrastructure, not a trained specialization weight.** The no-weight-updates lock governs *specialization* — "no fine-tuning of model weights; specialization lives in playbooks/retrieval/prompts/topology." An embedding model used only to compute cosine distance for retrieval is **part of the retrieval layer that the lock explicitly *permits* as a specialization substrate** ("Retrieval indexes — growing per-instance stores"). It is never trained, never updated by the loop; it is a fixed measuring stick, exactly like the BM25 IDF constants are. So embeddings do **not** violate no-weight-updates. They *do* trip the **stdlib-first dependency rule** (open question #5) — that is the real cost, and the reason to defer until BM25-kNN is measured too lexical.

**Decision: build kNN-over-BM25 first; it has zero lock tension and zero new dependency.** Embeddings are a measured upgrade, not a starting point.

### 2.10.3 Interface sketch (fits the real `pkg/exemplar.Store`)

A new `pkg/router` package, consuming the existing `exemplar.Store`. The store gains **one** additive method (cross-action kNN); everything else is new code that reads it.

```go
// pkg/exemplar — ONE additive method on the store interface.
// Backwards-compatible: existing Retrieve(action, …) is untouched;
// this is the action-agnostic sibling the router needs.
type Neighbor struct {
    Exemplar Exemplar // carries .Action, .Score, .Prompt, .Output
    Sim      float64  // BM25 score (or cosine, if an embedding store)
}

// RetrieveAcross ranks exemplars across ALL actions by prompt
// similarity and returns the global top-k with their action labels.
// Implemented in FileStore by building a BM25 index per action file,
// scoring the query against each, and merging by score-desc. Same
// k1=1.5/b=0.75 params as Retrieve; same tokenizer.
RetrieveAcross(ctx context.Context, prompt string, k int) ([]Neighbor, error)
```

```go
// pkg/router — the advisory playbook-hint router. No node type,
// no model call, no weight update. Pure read over the substrate.
package router

import (
    "context"
    "github.com/jrlmx2/oscillitron/pkg/exemplar"
    "github.com/jrlmx2/oscillitron/pkg/session"
)

// Hint is the advisory output. Empty Playbook == "no opinion"
// (below threshold / empty corpus) — Evaluate proceeds cold.
type Hint struct {
    Playbook   session.Playbook // top-voted action among the k neighbors
    Confidence float64          // winning_votes / total_votes (∈ [0,1])
    Margin     float64          // (top1 − top2) vote share; ambiguity guard
    K          int              // neighbors actually found (corpus may be < k)
}

// Router produces an advisory playbook hint from the AP input.
// Advisory ONLY — the runner/adapter MAY seed Evaluate with it,
// MUST NOT skip Evaluate (skipping would re-declare the node type
// the uniform-node lock forbids — see §2.9.5 Option B caveat).
type Router interface {
    Hint(ctx context.Context, in session.Payload) (Hint, error)
}

// ExemplarRouter is the kNN-over-BM25 implementation.
type ExemplarRouter struct {
    Store        exemplar.Store // reads RetrieveAcross; never writes
    K            int            // neighbors to poll (default 8)
    MinConfidence float64       // below → empty Hint (default 0.5)
    MinMargin    float64        // top1−top2 floor (default 0.15) — abstain on ties
}

func (r ExemplarRouter) Hint(ctx context.Context, in session.Payload) (Hint, error) {
    nbrs, err := r.Store.RetrieveAcross(ctx, in.Content, r.K)
    if err != nil || len(nbrs) == 0 {
        return Hint{}, err // empty hint == abstain; Evaluate runs cold
    }
    // Majority vote over neighbor .Action labels (the kNN-router
    // mechanism from arXiv:2505.12601). Optionally Sim-weighted.
    votes := map[session.Playbook]float64{}
    for _, n := range nbrs {
        votes[session.Playbook(n.Exemplar.Action)] += 1 // or += n.Sim
    }
    top1, top2, total := winnerRunnerUpTotal(votes) // small helper
    h := Hint{
        Playbook:   top1.key,
        Confidence: top1.val / total,
        Margin:     (top1.val - top2.val) / total,
        K:          len(nbrs),
    }
    if h.Confidence < r.MinConfidence || h.Margin < r.MinMargin {
        return Hint{}, nil // ambiguous → abstain (the locked cheap-local default)
    }
    return h, nil
}
```

### 2.10.4 Where it slots: a pre-Evaluate *advisory* hook, never a replacement

The router runs **before** the adapter's `Evaluate`, and its output is **seeded into** Evaluate, not substituted for it. Two integration points, both lock-clean:

1. **Runner-level (preferred).** The runner already calls `adapter.Evaluate(ctx, env)` on every AP (`pkg/runner`). Add an optional `Config.Router router.Router`. Before Evaluate, if wired, compute `h := Router.Hint(ctx, env.Input)`; on a non-empty hint, stamp it into the envelope as steering — concretely, append a `[playbook-hint: <playbook> (conf=…, k=…)]` line to a *prompt-steering* field the adapter reads, **or** set the §2.9.5-Option-B `env.Evaluate.HintPlaybook` (additive, omitempty) which the adapter treats as a *bias* in its evaluate prompt. The adapter's Evaluate still emits its own `{playbook, rationale, confidence}` — it may agree or override. Emit a `router.hint` trace event (playbook, confidence, margin, k) and a `router.evaluate_overrode_hint` event when Evaluate's pick ≠ the hint, so the disagreement rate is measurable (that rate is the empirical justification for keeping vs. cutting the router).
2. **Curated-adapter-level (alternative, zero runner change).** `pkg/adapter/curated` already wraps an inner adapter and *retrieves exemplars per Execute*. The router is the same read at *Evaluate* time. But the curated adapter deliberately passes `Evaluate` through unmodified today ("augmenting playbook *selection* with exemplars is meta-level different … intentionally deferred"). Wiring the router here is exactly *un-deferring that*, scoped to a hint. Acceptable, but the runner-level hook keeps the router visible to all adapters uniformly.

**Critical lock guardrail (from §2.9.5 Option B):** the hint MAY *bias* Evaluate's prompt; it MUST NOT *skip* Evaluate. A hard skip ("hint says `process`, don't bother evaluating") re-introduces a declared brain-function by the back door — the precise thing uniform-node + evaluate-every-AP forbid. Abstention (empty hint below threshold + margin) is the safe default and routes to the locked cheap-local cold Evaluate.

### 2.10.5 Lock verdict — Thread A

| Lock | Verdict |
|---|---|
| **uniform-node** | **PRESERVED** — no node type added; the router is a stateless read over substrate, output is steering text/bias, not structure. |
| **specialists-are-substrate** | **PRESERVED & extended** — reads the per-action exemplar store (the substrate), produces a per-action hint. Pure data-layer. |
| **evaluate→execute, every AP** | **PRESERVED** — Evaluate still runs on every AP and still owns the pick. The router is a prior seeded *into* Evaluate, never a replacement (hard-skip explicitly forbidden). |
| **no-weight-updates** | **PRESERVED** — kNN-over-BM25 trains nothing. The optional embedding upgrade uses a *frozen* embedding model = permitted retrieval infrastructure, not a trained specialization weight (argued §2.10.2). |
| **stdlib-first** | **PRESERVED for v0** (BM25-kNN reuses existing `bm25.go`); **TENSIONED only if** the embedding upgrade is adopted (new dependency — open question #5). Defer embeddings until BM25-kNN is measured too lexical. |
| **§2.9.5 Option A/B** | **REALIZES Option A automatically** (computed hint replaces hand-written framing); the optional `env.Evaluate.HintPlaybook` bias is Option B used strictly as a *bias*, never a hard skip. |

**Net: zero hard lock tension. One soft dependency tension (embeddings), deferred behind a measurement gate.** This is buildable today: one additive `RetrieveAcross` method on `exemplar.Store` + a new stdlib-only `pkg/router` + one optional `Config.Router` field on the runner.

---

## 2.11 Thread B — semantic entropy over Vote's N attempts as an escalation signal into `cope.RuleTable` (ITERATION 4)

Vote already produces N independent attempts and a vote distribution (`pkg/benchmark/orchestrator.Vote`). **Semantic entropy** — entropy over *meaning-clusters* of the N answers — is a near-free, well-calibrated confidence signal computable from outputs Vote **already generates**. It is the single most lock-compatible quality lever in this whole doc: it adds a *signal*, changes *nothing structural*, and rides cost Vote already paid.

### 2.11.1 The research (real sources)

Semantic entropy (Kuhn, Sun, Gal, ICLR 2023, arXiv:2302.09664; Farquhar, Kossen, Kuhn, Gal, *Nature* 2024) measures uncertainty in **meaning-space**, not token-space: sample N completions, cluster them by *semantic equivalence* (bidirectional NLI entailment via a model like DeBERTa), then take the entropy over the cluster distribution. It is the **best-calibrated of the cheap signals** (the survey numbers in §2.3: ECE 0.05–0.14, AUROC well above raw logprobs/verbalized confidence) precisely because it quotients out "same idea, different words" — the failure mode that makes naive self-consistency over raw strings overcount disagreement.

The variant that fits Oscillitron exactly is **discrete / black-box semantic entropy** (Farquhar et al.; radiology-VLM application arXiv:2510.09256): it is **frequency-based**, needs **no logprobs and no model internals** — just the sampled output strings and a clustering function. That matches Vote precisely: Vote has N output strings (`results[i].raw` / `results[i].extracted`) and **no logprob access** (the adapters return text, not logits). Discrete SE over the cluster *counts* is `H = −Σ_c (n_c/N)·log(n_c/N)`, where `n_c` is the size of meaning-cluster `c`. This is sunk-cost-free: the N samples are already taken.

### 2.11.2 How it differs from the existing `vote.tally` distribution string

Vote already emits a `distribution` string (`A=3,B=1,C=1`, alphabetical, `formatVoteDistribution`). That is **exact-string** clustering over the *extracted* form. Semantic entropy is a **strict generalization**:

- For **MCQ / extracted-canonical workloads (GPQA today)**, the extracted form is already `A`/`B`/`C`/`D` — exact-match clusters *are* meaning clusters. **Semantic entropy = entropy of the existing vote map, for free, with zero new machinery.** The `votes map[string]int` already built in `Vote.Answer` is exactly the cluster-count histogram. This is the v0 path: compute `H` from `votes` — no NLI, no embeddings.
- For **free-form workloads (Phase-1 email drafts, future open-ended benchmarks)**, exact-string clustering badly *overcounts* uncertainty (two correctly-worded decline emails are different strings but the same meaning → spuriously high entropy → spurious escalation). There the clustering must be **semantic**: NLI-mutual-entailment (the canonical method) or embedding-cosine threshold (cheaper, the discrete-SE shortcut). This is the v1 path, and it reuses the *same* frozen-embedding-as-retrieval-infra argument from Thread A §2.10.2 if cosine clustering is chosen.

So: **v0 = entropy over the exact-match vote histogram Vote already has (free); v1 = swap the clustering function for a semantic one when free-form workloads need it.** The cluster*er* is the only pluggable piece.

### 2.11.3 Interface sketch (fits the real `Vote` + `cope.RuleTable`)

```go
// pkg/semanticentropy — stdlib-only for the v0 exact-match path.
package semanticentropy

// Clusterer groups N raw/extracted answers into meaning-clusters.
// Returns cluster sizes (order irrelevant — entropy is symmetric).
type Clusterer interface {
    Cluster(answers []string) (sizes []int)
}

// ExactMatch is the v0 clusterer: identical strings cluster together.
// For MCQ/extracted-canonical workloads this IS meaning-clustering,
// and it's exactly the histogram Vote.formatVoteDistribution builds.
type ExactMatch struct{}

func (ExactMatch) Cluster(answers []string) []int {
    counts := map[string]int{}
    for _, a := range answers {
        if a == "" { continue } // failed extraction is not a cluster
        counts[a]++
    }
    out := make([]int, 0, len(counts))
    for _, n := range counts { out = append(out, n) }
    return out
}

// Entropy is discrete semantic entropy over cluster sizes:
//   H = −Σ (n_c/N)·ln(n_c/N)      (natural log; N = Σ n_c)
// Returns 0 for a single cluster (full agreement = certain).
func Entropy(sizes []int) float64 { /* … */ }

// Confidence maps entropy to a [0,1] confidence the cope table reads.
// Normalized by the max possible entropy ln(N) (all-singletons), so
// it's comparable across different N (stakes-scaled attempt counts):
//   conf = 1 − H/ln(N)            (1 = unanimous, 0 = all-disagree)
// N<2 (can't measure spread) returns the neutral 0 ("no signal"),
// which cope.Decide reads as mid-band → ShipWithCaveat (safe).
func Confidence(sizes []int, n int) float64 { /* … */ }
```

```go
// pkg/semanticentropy/nli (v1, separate subpackage so the dep is
// isolated like pkg/trace/otel) — the semantic clusterer for
// free-form. Bidirectional-entailment clustering per Kuhn/Farquhar.
type EntailmentClusterer struct {
    Entail func(ctx context.Context, a, b string) (bidirectional bool, err error)
    // Backed by an NLI model OR a frontier yes/no call OR embedding
    // cosine ≥ τ. The frozen-model-as-retrieval-infra argument
    // (§2.10.2) covers the embedding variant for no-weight-updates.
}
```

### 2.11.4 Wiring it into Vote → `cope.RuleTable.Decide`

The cope table's confidence input today is `inner.Confidence` — the *mean of per-attempt self-reported* confidences (`meanConfidence` in `Vote.Answer`, fed through `Coping.Answer` into `rules.Explain(inner.Confidence, kase.Stakes)`). Self-reported confidence is exactly the **overconfident, poorly-calibrated** signal §2.3 warns about. Semantic-entropy confidence is the **better-calibrated replacement or blend** — and it drops in at the *same single scalar* the table already consumes. **No new cope Action; no schema change to the rule table.** Map straight onto existing Actions:

| SE confidence | stakes | existing `cope.Action` | effect |
|---|---|---|---|
| high (clusters collapse to ~1; unanimous) | any | `Ship` (≥0.85) | answer trusted |
| mid (some disagreement) | low/med | `ShipWithCaveat` | qualify |
| **low (high entropy; answers scatter)** | **high** | **`Escalate`** | **pay for frontier** ← the win |
| low | low/med | `ShipWithCaveat` | low-stakes ships anyway |

Two wiring options, both touching one scalar:

- **Option 1 — Vote computes SE-confidence and returns it as `Answer.Confidence`** (replacing or blending the self-reported mean). Smallest change: `Vote.Answer` already has the `votes` histogram; add `Answer.Confidence = semanticentropy.Confidence(sizesFrom(votes), successes)`. The whole existing `Coping` → `cope.RuleTable.Decide` path then runs unchanged — it just receives a better-calibrated number. **Recommended.** Optionally keep both via a blend `α·SE + (1−α)·self_report` and a `vote.semantic_entropy` trace event (`H`, `clusters`, `conf`, `n`) so calibration (`pkg/benchmark/calibration`) can score SE-confidence vs. self-reported head-to-head — the measurement that decides the blend weight.
- **Option 2 — a parallel gate in `Coping.Answer`** that consults SE-confidence *alongside* `inner.Confidence` (e.g., escalate if *either* the self-report *or* SE says low on high stakes). More plumbing (Coping must see the per-attempt answers, which Vote currently reduces away), and it edges toward a second confidence axis the table wasn't designed for. **Defer** unless the blend in Option 1 proves insufficient.

**Why this sharpens the locked path's residual cost.** §2.8 showed the locked path's only non-zero cost is the rare `delegate`/escalation to frontier (~5% of requests in the Phase-1 model). A *better-calibrated* escalation trigger means escalating **exactly** the high-entropy-high-stakes cases and *not* the falsely-confident ones — tightening that ~5% toward the cases that actually need frontier. Semantic entropy makes the one expensive decision in the locked path more precise, for free (Vote already sampled N). That is strictly additive value with no cost added.

### 2.11.5 Lock verdict — Thread B

| Lock | Verdict |
|---|---|
| **uniform-node / evaluate→execute** | **UNAFFECTED** — operates entirely in the bench-orchestrator + cope layer, not the AP workflow. No node, no playbook, no envelope change. |
| **cope three Actions** (`Ship`/`ShipWithCaveat`/`Escalate`/`Refuse`) | **PRESERVED** — SE-confidence feeds the *existing* confidence input of `RuleTable.Decide`; **no new Action invented** (the loop's explicit requirement). |
| **judge/verifier philosophy** (gate before output consumed) | **PRESERVED & reinforced** — SE is one more pre-consumption gate, same philosophy as inhibitor/critique/cope. |
| **no-weight-updates** | **PRESERVED** for the v0 exact-match clusterer (pure arithmetic). The v1 NLI/embedding clusterer uses a *frozen* model = retrieval/measurement infra, not a trained weight (same argument as §2.10.2). |
| **stdlib-first** | **PRESERVED for v0** (entropy over the existing vote histogram is arithmetic). v1 semantic clustering isolates its dependency in a `pkg/semanticentropy/nli` subpackage, exactly as `pkg/trace/otel` isolates the OTel dep. |

**Net: fully lock-compatible. It changes nothing structural — it adds one better-calibrated scalar to a decision the system already makes.** The loop's hypothesis ("likely fully lock-compatible; say so if true") is **confirmed**: zero tension. The only judgment call is blend weight (SE vs. self-reported), and that is an empirical question `pkg/benchmark/calibration` already answers.

---

## 2.12 Falsification experiment / decision procedure (ITERATION 5 — converts the doc into a decision procedure)

The doc has reached three conclusions worth *running against the harness*: §2.8 settled cost on arithmetic (dense loses); §2.10 and §2.11 produced two buildable grafts whose *quality* value is asserted, not measured. **This section makes the doc falsifiable.** It states the null hypotheses (the doc's own conclusions are the nulls to attack), the exact arms, metrics, the existing telemetry each reads, the decision thresholds, the minimal instrumentation each arm needs, the calibration scoring math, and an ordered run plan producing keep/cut decisions for all three sub-designs.

**Framing discipline (the §2.5 kill-or-proceed mirror):** the existing architecture is always the **null**. A graft earns its keep only by clearing a pre-stated threshold *above measurement noise*; "no significant difference" = **cut**, never "keep because it didn't hurt." The bench/doc note ~5–10 pp variance at 20 cases is the binding constraint on every threshold below — addressed per-hypothesis under "statistical power."

### 2.12.0 Statistical power — the constraint every threshold inherits

At n=20 GPQA cases, a pass-rate's 95% Wilson half-width is ≈ ±0.21 at p≈0.5 (the worst case) — far too wide to call a 5 pp effect. Two consequences that shape every arm below:

- **Sample size floor.** Pass-rate-delta arms need **n ≥ 100** for a real (not noise) read on a ~10 pp effect; GPQA Diamond has 198 cases (`cmd/bench/cases/gpqa_diamond.json`), so run the **full set** (`--limit 0`) for any pass-rate decision. MMLU-Pro (`--benchmark mmlu-pro`) is the larger second corpus if more power is needed.
- **Prefer paired / within-case metrics over between-arm pass-rate deltas.** The router's disagreement counter and the SE-vs-self-report calibration head-to-head are both **computed on the *same cases in the same run*** — they sidestep between-run variance entirely (no second run, no re-sampled case order). That is deliberate: the highest-power signals here are *within-run paired* comparisons, and the experiment is designed so the keep/cut decision rests on those, with the noisier between-arm pass-rate/cost deltas as confirmation, not as the primary gate.
- **Determinism lever.** `gpqa.Loader` placement is SHA-256-deterministic per case ID, and sibling dispatch is seedable (`runner.Config.Rand`); fix the seed so the *only* difference between a null run and a treatment run is the graft. This removes case-order and answer-placement noise from the between-arm comparison.

### 2.12.1 H0-cost — "dense mid-tier orchestration provides no cost-quality improvement over the existing Vote/Tree on local-cheap substrate"

This is the §2.8 conclusion restated as a null to *attack with the harness* rather than only with arithmetic. The arithmetic said dense-mid is ~5–6× the local path on cost while matching its quality; the experiment checks whether a hosted-mid orchestrator buys any *quality* that would justify the cost on a real benchmark.

- **Arms (all on the full GPQA set, fixed seed):**
  - **Null (locked path):** local Vote-5 on the cheap substrate.
    `cmd/bench --benchmark gpqa --cases cmd/bench/cases/gpqa_diamond.json --limit 0 --vote-n 5 --orchestrator-substrate ollama --orchestrator-model qwen2.5:7b --frontier-substrate anthropic --frontier-model claude-sonnet-4-6 --price 'orchestrator-vote-5-qwen2.5:7b=0' --frontier-price 6.60 --report-out scratch/exp/h0cost-null.json --stream-out scratch/exp/h0cost-null.jsonl`
  - **Treatment (dense mid-tier):** the *same Vote-5* with a hosted mid-tier substrate (the only "dense" reading §2.8 left standing — it is literally `--orchestrator-substrate` swapped).
    `cmd/bench … --orchestrator-substrate anthropic --orchestrator-model claude-haiku-4-5 --price 'orchestrator-vote-5-claude-haiku-4-5=2.20' --frontier-price 6.60 --report-out scratch/exp/h0cost-dense.json …`
  - Optional third arm: `--tree` on each substrate (the decompose+recompose shape) to check whether dense's value, if any, shows up in Tree rather than Vote.
- **Metrics & existing telemetry read:** `AggregateStats.PassRate` and `AggregateStats.AvgScore` (quality); `TotalActualUSD` + `SavingsRatio` (cost) — all already printed by `printReport` and dumped to `--report-out`. No instrumentation needed; this arm is **pure existing harness.**
- **Decision threshold:**
  - **Cut dense** (null holds) if `PassRate(dense) − PassRate(null) ≤ +5 pp` (within n≥100 noise) — i.e. dense buys no quality, so its 5–6× cost premium (already established §2.8) is unjustified. *Expected outcome given v1's "vote on cheap substrate ≈ frontier."*
  - **Needs-more-data / operational-only** if dense shows `+5..+10 pp` quality: real but marginal; dense becomes a *documented operational option* for operators without local GPUs (the §2.8.5(2) verdict), not a cost win. Re-run on MMLU-Pro to confirm the lift replicates.
  - **Revisit the lock** only if `> +10 pp` AND that lift is unreachable by raising local `--vote-n` at equal-or-lower cost — a bar §2.8 predicts dense cannot clear.
- **Statistical power:** full 198-case GPQA; the cost columns are exact (token-counted, not sampled) so the *cost* half has zero variance — only the pass-rate half carries the ±noise, and the ≤+5 pp cut threshold is set at the noise floor for n≈198.

### 2.12.2 H0-router — "the §2.10 kNN playbook-hint router does not improve Evaluate's playbook picks; its hints, when they disagree with Evaluate, are no better than Evaluate alone"

The router (§2.10) is **not yet built** — so this arm requires the minimal instrumentation in §2.12.4(A). The null is sharp and *within-run paired*: the router earns nothing if Evaluate would already pick what the hint suggests (zero disagreement → router is inert), OR if, on the cases where they disagree, the hint is no more often *right* than Evaluate's own pick.

> **⚠ CORRECTED BY THE §2.12.9 STRESS-TEST (iteration 9): GPQA is the WRONG test for this hypothesis.** The "< 5% disagreement on GPQA → cut on inertness" gate below is **structurally guaranteed, not measured** — on GPQA the curation cycle writes every exemplar under one `--curate-action` (so the kNN vote is a constant `process` hint) AND the `Tree` arm hard-pins every child AP's Evaluate to `process` (`tree.go:156`), so a near-zero disagreement is a foregone conclusion that proves nothing about the mechanism. **The router's fair test is a heterogeneous-playbook workload over an unconstrained Evaluate-per-AP walk, which does not exist in the harness today (§2.12.9.2–.3).** Read the GPQA arm below ONLY as a wiring sanity check (the hint fires, the counter increments), explicitly inert-by-construction — never as the keep/cut decision. The real gate lives in §2.12.9.

- **Two-stage test, both paired within one run:**
  1. **Disagreement rate (the cheap gate).** Run with `runner.Config.Router` wired (advisory only — never skips Evaluate, per the §2.10.4 guardrail). Read the new `router.evaluate_overrode_hint` counter (§2.12.4-A): the fraction of APs where Evaluate's pick ≠ the router's `Hint.Playbook`. **If disagreement ≈ 0%**, the hint is inert and the router earns nothing it could be cut for. *Caveat (§2.12.9): on GPQA this fires for free — the single-action store and child-pinned `Tree` Evaluate force it — so a near-zero disagreement there is **not** a meaningful inertness read. The disagreement rate is only informative over a heterogeneous-playbook store + an unconstrained Evaluate walk (§2.12.9.3).*
  2. **Hint-correctness on disagreements (only if disagreement is non-trivial).** On the subset where hint ≠ Evaluate, compare downstream `Verdict.Pass` for (a) the run where the hint *biased* Evaluate vs. (b) the null run where Evaluate ran cold (same fixed seed → same cases). The paired question: *did biasing toward the hint flip more cases right-ward than wrong-ward?*
- **Arms (the GPQA arms below are a WIRING SANITY CHECK ONLY per §2.12.9 — inert by construction; the real keep/cut arm is the heterogeneous-playbook walk in §2.12.9.3):**
  - **Null:** `cmd/bench --benchmark gpqa --limit 0 … --report-out scratch/exp/h0router-null.json` (no `--router`).
  - **Treatment:** identical + `--router --router-store scratch/exp/router-store` (new flag, §2.12.4-A) pointing at an exemplar store **pre-populated by a prior curation run** (`--curate-store-dir`, §2.12.5) so `RetrieveAcross` has neighbors to vote over. *A cold/empty store makes the router abstain on every AP — that is a valid null result ("no substrate → no hints → inert"), but to actually test the mechanism the store must be warm.*
- **Metrics & telemetry read:** `router.evaluate_overrode_hint` count and `router.hint` events (new, §2.12.4-A); `PassRate`/per-case `Verdict.Pass` from `--report-out` (existing). Disagreement rate is the **primary** metric (paired, zero between-run variance); pass-rate delta is **confirmatory only**.
- **Decision threshold (applied on the §2.12.9.3 heterogeneous-playbook walk, NOT on GPQA):**
  - **Cut** if disagreement `< 5%` (router is inert — Evaluate already agrees) OR if, on disagreements, biasing toward the hint does not net-flip cases right-ward beyond noise (`Δ right-flips − wrong-flips ≤ 0` across the disagreement subset). *Only a cut measured on the heterogeneous walk counts — a GPQA < 5% is structurally forced (§2.12.9.1) and is not a valid cut.*
  - **Keep / promote to Option-B bias** if disagreement is material (`≥ 15%`) AND hint-biased Evaluate net-flips cases right-ward (`> +3 pp` on the disagreement subset, or `> +5 pp` overall on the multi-playbook workload). Only then does the additive top-level `HintPlaybook` envelope field (§2.9.5 Option B, corrected in §4d.0 to a sibling of `NeedsVerification`, NOT inside the nil `*Evaluate`) earn its schema change.
  - **Needs-more-data** in between.
- **Statistical power:** the disagreement rate is a per-AP proportion over many APs — extremely tight CI, no power problem *given a workload that actually varies the playbook*. The binding constraint is not sample size but **workload heterogeneity**: on a single-action store the rate is a degenerate constant (§2.12.9.1), so CI tightness is irrelevant. The hint-correctness delta lives only on the disagreement subset; this is exactly why the heterogeneous-playbook walk (§2.12.9.3) is required before any *keep or cut* decision — GPQA can produce neither.

### 2.12.3 H0-SE — "§2.11 semantic entropy is no better-calibrated than the existing self-reported Answer.Confidence and does not improve Coping's escalate decisions"

The strongest-prior graft and the one most cleanly measured, because **both confidence signals can be computed on the *same Vote run*** (SE from the `votes` histogram, self-report from `meanConfidence`) and scored head-to-head with zero between-run variance. This is the single highest-power test in the experiment.

- **Single run, two confidence columns.** Instrument Vote to emit *both* `Answer.Confidence` (unchanged, self-reported mean) and a new `Answer.SEConfidence = semanticentropy.Confidence(sizesFromVotes, successes)` (§2.12.4-B). One GPQA run produces, per case: `{pass, self_conf, se_conf, stakes}`. No second run.
- **Calibration scoring (head-to-head, computed offline from `--report-out`):** for each confidence column independently, compute the three standard reliability metrics over the existing `calibration.DefaultBands` (and a finer 10-bin grid):
  - **Expected Calibration Error (ECE)** — `Σ_b (n_b/N)·|acc(b) − conf(b)|`, the weighted gap between bin accuracy and bin mean-confidence (Naeini et al., AAAI 2015; Guo et al., ICML 2017 "On Calibration of Modern Neural Networks"). Lower = better-calibrated.
  - **Brier score** — `(1/N)·Σ_i (conf_i − pass_i)²`, the mean-squared error of the probability forecast (Brier 1950). Lower = better; decomposes into calibration + refinement (Murphy 1973).
  - **Reliability-curve slope** — pass-rate per band vs. band mean-confidence; the existing `calibration.FormatTable` already prints pass-rate-per-band, so the curve is `Row.PassRate()` vs. `Row.MeanConfidence` per column. Steeper monotone slope = more informative (the §3.3 "slopes up = useful, flat = noise" test, now applied to *two* columns side by side).
  - These are computed by a tiny offline scorer reading the `--report-out` JSON (§2.12.4-B notes where it lives) — **no new bench run** to score, just the two confidence columns already in the JSON.
- **Escalation-quality test (the decision that actually spends money).** Run `cmd/bench --cope --stakes rotate` twice (or once with both columns, §2.12.4-B): once with `cope.RuleTable.Decide` fed `self_conf`, once fed `se_conf`. The cope table escalates *low-confidence + high-stakes* cases to the frontier. The paired question on the **high-stakes subset**: does SE escalate **strictly fewer false-confident cases** (cases the local Vote got *wrong* but self-report called confident → not escalated → shipped wrong) at **equal-or-better recall** of the genuinely-hard cases?
  - Metric: on high-stakes cases, partition by `{escalated?, locally-correct?}`. The win condition is SE moving wrong-and-not-escalated cases (the dangerous quadrant) into escalated, without inflating escalate-count on already-correct cases (wasted frontier spend). Read from `--report-out`: `answer.cope_action` × `verdict.pass` × `case.stakes`, all already in the JSON.
- **Decision threshold:**
  - **Build SE (replace or blend)** if SE's **ECE is lower by ≥ 0.03** (a meaningful calibration gain; ECE deltas of 0.03–0.05 are the scale at which temperature-scaling papers claim wins) AND its Brier is no worse AND it does not *increase* false-confident high-stakes ships. If all three hold, ship `Answer.Confidence = SE` (Option 1, §2.11.4); if SE wins calibration but the escalation test is mixed, ship the **blend** `α·SE + (1−α)·self` and tune α on the same JSON.
  - **Cut SE** if ECE-delta `< 0.01` (SE no better than self-report — the MCQ case where the exact-match histogram and the self-report mean carry the same information) AND escalation behavior is unchanged. *Possible on GPQA specifically, because for MCQ the vote histogram is already a decent confidence proxy.*
  - **Needs-more-data** in between, or whenever SE wins calibration but the free-form workload (where SE's meaning-clustering should matter far more than on MCQ) hasn't been run — defer to the §2.12.6 free-form arm before deciding the v1 NLI/embedding clusterer.
- **Statistical power:** calibration metrics are computed over all 198 cases × however many attempts; ECE/Brier on n≈198 with a 3-band (or 10-bin) split is stable, and because **both columns are scored on the identical case set**, the *paired* ECE/Brier delta has far tighter CI than either absolute value. The escalation-quality quadrant counts are smaller (high-stakes subset only under `--stakes rotate` ≈ 1/3 of cases ≈ 66) — flag as the lower-power half; run `--stakes high` (all cases high) for a full-power escalation read if the rotate-subset is ambiguous.

### 2.12.4 Minimal instrumentation list (the only code the experiment needs)

Each item is the *smallest* change that makes its hypothesis measurable. None touches a lock (all argued in §2.10.5 / §2.11.5); all are additive.

**(A) Router (H0-router) — the larger change, because the router is unbuilt.** Per §2.10.3–2.10.4:
1. `pkg/exemplar`: add `RetrieveAcross(ctx, prompt, k) ([]Neighbor, error)` to `Store` + `FileStore` (cross-action BM25 kNN, reusing `bm25.go`). One method, backward-compatible.
2. `pkg/router` (new, stdlib-only): `Router` interface + `ExemplarRouter{Store, K, MinConfidence, MinMargin}` per the §2.10.3 sketch.
3. `pkg/runner`: optional `Config.Router router.Router`; before `adapter.Evaluate`, compute the hint and (if non-empty) seed it as steering text / `env.Evaluate.HintPlaybook` bias — **never skip Evaluate.**
4. **Two trace events** (the experiment's primary metric): `router.hint` (`playbook, confidence, margin, k`) on every non-abstain hint, and `router.evaluate_overrode_hint` (`hint_playbook, evaluate_playbook`) whenever Evaluate's pick ≠ the hint. A counter over the latter / former = the disagreement rate. These are the *only* new telemetry H0-router needs.
5. `cmd/bench`: `--router` (bool) + `--router-store DIR` (the exemplar store the router reads) + `--router-k`/`--router-min-confidence`/`--router-min-margin` (optional tunables). Wires `runner.Config.Router` for the Tree orchestrator (the only bench arm that walks the runner; Vote/Single don't call Evaluate, so the router is exercised via `--tree`). **Note this scoping consequence:** H0-router is only measurable on the **Tree** arm (or the §2.12.6 multi-playbook workload) — Vote bypasses Evaluate entirely (it pins `PlaybookProcess`). The run plan accounts for this.

**(B) Semantic entropy (H0-SE) — tiny, because Vote already has the histogram.** Per §2.11.3–2.11.4:
1. `pkg/semanticentropy` (new, stdlib-only): `Clusterer` interface, `ExactMatch` clusterer, `Entropy(sizes)`, `Confidence(sizes, n)` per the §2.11.3 sketch.
2. `pkg/benchmark`: add `Answer.SEConfidence float64` (additive; `omitempty` in `answerJSON`) so a run carries *both* confidence columns for the head-to-head. (Alternatively, overwrite `Answer.Confidence` behind a `--se-confidence` flag — but carrying both in one run is higher-power and is the recommended path.)
3. `pkg/benchmark/orchestrator.Vote`: after the tally, set `Answer.SEConfidence = semanticentropy.Confidence(sizesFrom(votes), successes)` where `sizesFrom(votes)` is the histogram values. ~3 lines; the histogram already exists. Emit a `vote.semantic_entropy` trace event (`H, clusters, conf, n`).
4. `pkg/cope` / `orchestrator.Coping`: a `--cope-confidence-source self|se|blend` flag selecting which column feeds `RuleTable.Decide`. Default `self` (unchanged behavior); `se`/`blend` are the treatment arms. One field on `Coping`, one switch.
5. **Offline calibration scorer** (new, tiny — `cmd/calib` or a `calibration.Score(report, column)` helper): reads `--report-out` JSON, computes ECE / Brier / reliability-slope for a named confidence column over `calibration.DefaultBands` (+ a 10-bin grid). Reuses `calibration.pickBand`. This is measurement-layer, not runtime — it can even be a one-off script, but a `calibration.ECE`/`calibration.Brier` pair next to `Compute` is the clean home and is independently useful.

**(C) Pricing correctness (prerequisite for honest cost columns).** Fix the stale `cmd/phase1` `defaultPricing` (Haiku `0.80/4.00`→`1.00/5.00`, Opus `15/75`→`5.00/25.00`) flagged in §2.8.1 / open #2 — one-line correctness fix so any phase1 cost read in the run plan is honest.

### 2.12.5 The two-run curation cycle (only the router needs it)

H0-router's treatment arm needs a *warm* exemplar store (else the router abstains on every AP — a valid but uninteresting null). The harness already supports the populate→consume cycle in two commands (`cmd/bench` `--curate-store-dir` then `--use-store`):

- **Run 0 (populate):** a GPQA (or multi-playbook) bench with `--curate-store-dir scratch/exp/router-store --stream-out scratch/exp/run0.jsonl --curate-action process` (and, for the multi-playbook workload, additional curate-actions per playbook). This writes per-action exemplars the router's `RetrieveAcross` will vote over.
- **Run 1 (consume):** the H0-router treatment arm with `--router --router-store scratch/exp/router-store`. The router now has neighbors. (Distinct from `--use-store`, which prepends exemplars to the *prompt*; the router instead *votes their action labels into a hint* — same store, different read, §2.10.1.)

This reuses the locked curation cycle wholesale; the only new piece is the router's *read* of the store (`RetrieveAcross`), already in §2.12.4-A.

### 2.12.6 The free-form / multi-playbook companion workload (required before any *keep*)

GPQA is MCQ — `process`-dominated and exact-match-clusterable. It can *cut* both grafts (router on inertness, SE on histogram-equals-self-report) but cannot fairly *keep* them, because neither graft's hypothesized strength (multi-playbook disagreement for the router; meaning-clustering of paraphrases for SE) is exercised by single-answer MCQ. So a *keep* decision requires a second workload where playbook choice actually varies and answers are free-form:

- **Phase-1 email drafting** (`cmd/phase1` + `cases.json`) is the in-repo free-form workload. It exercises `plan`/`process`/`compose` (multi-playbook → router disagreement is structurally possible) and produces free-form drafts (paraphrase clustering → SE's NLI/embedding clusterer, §2.11.2 v1, actually matters). Scoring is the existing rubric grader (`pkg/grader.AnthropicGrader`), so "pass" is rubric-threshold rather than exact-match.
- This is where the **v1 clusterer** (`pkg/semanticentropy/nli`) and the **embedding-router** (§2.10.2) earn-or-fail their dependency: only if the exact-match/BM25 v0 is *measured insufficient here* does the frozen-embedding dependency (open #4) get adopted. The experiment thus also resolves the dependency-posture question by measurement, exactly as open #4 demands.

### 2.12.7 Ordered run plan (produces all three keep/cut decisions)

Each step names the real command shape and the artifact it writes; per the project convention (`oscillitron/CLAUDE.md` "Recording scored runs"), every scored run distills into `scratch/bench-results-<YYYY-MM-DD>.md`.

| # | Purpose | Command shape (real flags) | Artifact | Decides |
|---|---|---|---|---|
| 0 | **Pricing fix** (prereq) | edit `cmd/phase1` `defaultPricing`; `go test ./...` | — | honest cost columns |
| 1 | **H0-cost null** | `cmd/bench --benchmark gpqa --limit 0 --vote-n 5 --orchestrator-substrate ollama --orchestrator-model qwen2.5:7b --frontier-substrate anthropic --frontier-model claude-sonnet-4-6 --price 'orchestrator-vote-5-qwen2.5:7b=0' --frontier-price 6.60 --report-out scratch/exp/h0cost-null.json --stream-out scratch/exp/h0cost-null.jsonl` | `h0cost-null.json` | H0-cost (null) |
| 2 | **H0-cost dense** | same, `--orchestrator-substrate anthropic --orchestrator-model claude-haiku-4-5 --price 'orchestrator-vote-5-claude-haiku-4-5=2.20'` → `--report-out scratch/exp/h0cost-dense.json` | `h0cost-dense.json` | H0-cost: cut/ops/revisit per §2.12.1 |
| 3 | **SE instrumentation + run** (build §2.12.4-B first) | `cmd/bench --benchmark gpqa --limit 0 --vote-n 5 --cope --stakes rotate --orchestrator-substrate ollama --orchestrator-model qwen2.5:7b --frontier-substrate anthropic --frontier-model claude-sonnet-4-6 --report-out scratch/exp/se-both.json` (Vote now emits both `confidence` + `se_confidence`) | `se-both.json` | — |
| 4 | **SE calibration score** (offline) | `calibration.Score(se-both.json, "confidence")` vs `…, "se_confidence")` → ECE/Brier/slope per column | `scratch/exp/se-calibration.md` | H0-SE calibration half |
| 5 | **SE escalation A/B** | rerun step 3 twice with `--cope-confidence-source self` then `=se` (or read both from the dual-column run); compare cope_action×pass×stakes on high-stakes subset; add `--stakes high` if rotate-subset ambiguous | `se-escalation.md` | H0-SE escalation half → build/blend/cut per §2.12.3 |
| 6 | **Router populate** (build §2.12.4-A first) | `cmd/bench --tree --benchmark gpqa --limit 0 … --curate-store-dir scratch/exp/router-store --stream-out scratch/exp/run0.jsonl` | `router-store/` | warms the store |
| 7 | **Router null** | `cmd/bench --tree --benchmark gpqa --limit 0 … --report-out scratch/exp/h0router-null.json` (no `--router`) | `h0router-null.json` | H0-router baseline |
| 8 | **Router treatment** | same `--tree …` + `--router --router-store scratch/exp/router-store --report-out scratch/exp/h0router-treat.json` | `h0router-treat.json` + `router.*` trace events | disagreement rate → cut-on-inertness or proceed |
| 9 | **Multi-playbook keep-gate** (only if steps 8 / 5 say "proceed/keep") | repeat 6–8 and 3–5 on the Phase-1 free-form workload (`cmd/phase1` + rubric grader) to exercise router disagreement + SE paraphrase-clustering | `scratch/exp/freeform-*.md` | the *keep* decisions + open #4 dependency posture |
| 10 | **Findings** | distill all of the above | `scratch/bench-results-<DATE>.md` | the record |

Steps 1–2 (H0-cost) and 3–5 (H0-SE) are independent and can run in either order; both are pure-or-near-pure existing harness. Steps 6–9 (H0-router) gate on building §2.12.4-A and are the largest lift — hence the build-plan order in §7 puts SE first.

### 2.12.8 Falsification outcome table (what the doc CONCLUDES per result region)

| Hypothesis | Result region | Doc concludes |
|---|---|---|
| **H0-cost** | `PassRate(dense) − PassRate(null) ≤ +5 pp` | **Cut dense as a cost play.** §2.8 vindicated by measurement; dense stays a documented *operational* option only. |
| | `+5..+10 pp`, replicates on MMLU-Pro | **Operational-only.** Document hosted-mid as the no-local-GPU path; never claim a cost win. |
| | `> +10 pp` AND unreachable by higher local `--vote-n` at ≤ cost | **Revisit** the cheap-local-orchestrator lock (expected: never reached). |
| **H0-router** | disagreement `< 5%` OR no net right-flips | **Cut the router.** Evaluate already agrees; the kNN hint is inert. (Likely on MCQ.) |
| | disagreement `≥ 15%` AND `> +3 pp` right-flips on disagreements (or `> +5 pp` on multi-playbook) | **Build it**, promote to Option-B `HintPlaybook` bias; embeddings only if BM25 measured too lexical (open #4). |
| | in between | **Needs-more-data**; run the §2.12.6 multi-playbook workload before deciding. |
| **H0-SE** | ECE-delta `≥ 0.03`, Brier no worse, no extra false-confident ships | **Build SE** as `Answer.Confidence` (or blend); the better-calibrated escalation trigger sharpens the locked path's residual ~5% frontier spend. |
| | ECE-delta `< 0.01`, escalation unchanged | **Cut SE on MCQ; defer to free-form.** The exact-match histogram already carries the self-report's information for MCQ. |
| | ECE wins but free-form unrun | **Needs-more-data**; the §2.12.6 free-form arm decides the v1 NLI/embedding clusterer + open #4. |

---

### 2.12.9 STRESS-TEST: is the §2.12.2 router-inertness prediction even a fair test? (ITERATION 9)

The entire Thread A build slot (§4d) rests on one untested prediction in §2.12.2: *the kNN playbook-hint router is **inert on GPQA MCQ** — disagreement < 5%, so it's cut before the embedding question is even reached.* This is the single most load-bearing unverified assumption in the doc: it both (a) justifies building only the BM25 v0, and (b) is the *first* router measurement the run plan (§2.12.7 steps 6–8) produces. If that prediction is true for the wrong reason — if GPQA is *rigged* to force inertness rather than *measuring* it — then step 8 produces a number that looks like a clean kill but proves nothing, and a build session could waste a PR + a multi-hour run generating a non-result. This subsection reasons the prediction through against the **real code** and corrects the gate.

#### 2.12.9.1 WHY the router is inert on GPQA — and the reason is structural, not empirical

Walk the actual mechanism. The router votes the `Action` labels of the k nearest exemplars into a hint, then the runner compares that hint against what Evaluate picks. For the hint and Evaluate to *disagree*, **two independent kinds of diversity must both exist**: (i) the neighbor pool must carry **more than one distinct `Action` label** (else the majority vote always returns the same playbook — no routing signal, exactly the degenerate single-intent case the semantic-router literature describes: nearest-neighbor routing produces differentiation only when routes are genuinely distinct), and (ii) Evaluate must **sometimes pick something other than that label**. On GPQA via the harness, *both* collapse to a constant — and not because Evaluate happens to agree with good hints, but because the code forecloses variation:

- **The exemplar store is single-action by construction.** Curation writes every exemplar under one `cfg.Action` (`pkg/curation/curation.go:250` — `Action: cfg.Action`), and the bench sets that from a single scalar flag `--curate-action` (default **`process`**; `cmd/bench/main.go:85`). A GPQA curation run (§2.12.7 step 6) therefore produces a store where **every exemplar is tagged `process`**. `RetrieveAcross` ranks across "all actions," but there is only one action file. So the kNN majority vote returns `process` for *every* query, with confidence 1.0 and margin 1.0 — a maximally confident hint that is the same on every AP. The "router" has no label diversity to route over; it is a constant function.
- **The `Tree` arm hard-pins Evaluate to `process` on every child.** The router is only measurable on the `--tree` arm (Vote/Single bypass Evaluate — §2.12.4-A). But `treeAdapter.Evaluate` (`pkg/benchmark/orchestrator/tree.go:156–163`) **never calls the inner adapter's Evaluate for a child AP** — `if env.ParentID != nil` it stamps `Playbook: PlaybookProcess` and returns. The root is forced to `plan`-or-`process` (any other pick is overwritten to `process`, L174–177). So across an entire GPQA Tree run, Evaluate emits `plan` exactly once (the root) and `process` everywhere else — and even the root's steering text is ignored for the constrained pick.

Compose these and the disagreement rate is **mechanically near-zero**: a constant `process` hint vs. an Evaluate that is `process` on every child and `plan`-or-`process` on the root. The only APs that *could* disagree are roots where Evaluate picks `plan` while the hint says `process` — a single AP per case, and even there the "disagreement" is an artifact of the store being unable to contain a `plan` exemplar, not a substantive routing judgment. **§2.12.2's "< 5% disagreement → cut on inertness" will fire, but it will fire for free.** The number is determined before the run starts. That is not a measurement; it is a tautology dressed as one.

This is the precise sense in which the iteration-8 handoff's worry was right: the inertness prediction is *true*, but true by construction. A cut-on-inertness from a GPQA Tree run is **uninformative** — it cannot distinguish "the kNN hint mechanism is worthless" (the conclusion §2.12.2 wants to license) from "this workload + this harness path cannot exercise the mechanism at all" (the actual situation).

#### 2.12.9.2 WHERE the router would actually earn its keep — and whether GPQA is the right test at all

A retrieval-router earns its keep exactly when the workload is **heterogeneous in the routed dimension**: different inputs genuinely want different playbooks, so the neighbor pool carries mixed `Action` labels and the majority vote produces a *query-dependent* hint. This is the standard finding in the routing literature — routers buy a cost/quality improvement over a single fixed policy only when the query distribution is heterogeneous enough that score distributions across the routing targets overlap and "which target is best" actually varies per query ([emergentmind, LLM routers](https://www.emergentmind.com/topics/llm-routers); [Difficulty-Aware Agentic Orchestration, arXiv:2509.11079](https://arxiv.org/pdf/2509.11079)). When the workload is homogeneous — every query wants the same target — routing provides no differentiation and collapses to that constant policy, the same degeneracy the semantic-router/intent literature calls out for single-intent corpora (nearest-neighbor routing only differentiates when the routes are genuinely distinct; [Zep intent router](https://blog.getzep.com/building-an-intent-router-with-langchain-and-zep/), [vLLM semantic-router blog](https://vllm.ai/blog/2025-11-19-signal-decision)).

GPQA is the homogeneous extreme: **every case is the same shape** (a hard graduate-level MCQ), so every case wants the *same* playbook (`process` — answer the question), and the harness enforces exactly that. **GPQA is therefore rigged against the router** — not maliciously, but because MCQ-single-answer is the one workload guaranteed to give the router nothing to route over. Using it as the *primary* router test (§2.12.7 steps 6–8, ahead of the free-form arm) inverts the experiment's own logic: §2.12.6 already concedes "GPQA alone can only *cut* the router (on inertness), never *keep* it." The stress-test sharpens that to: **on GPQA the router can't even be cut *meaningfully* — a < 5% disagreement there is a foregone conclusion, so it neither keeps nor honestly cuts.**

The router's fair test is a workload where playbook choice genuinely varies across inputs. Concretely:

- **The Phase-1 email corpus (`cmd/phase1/cases.json`) is the closest in-repo fit.** Its cases are heterogeneous *by design* (decline / clarify / firm-with-late-vendor / redirect / acknowledge-and-defer / ambiguous-urgency / scope-creep-pushback / graceful-no — see the Phase-1 lock in `../CLAUDE.md`). Different cases plausibly want different playbooks (`plan` to decompose a multi-part reply, `process` to draft a single body, `compose` to fold drafts, `critique` to check tone), so a warm multi-action store would carry mixed labels and the kNN vote would be query-dependent — the condition under which a disagreement rate is *informative*.
- **A mixed benchmark** (GPQA ∪ a decomposition-heavy set, e.g. multi-hop or agentic tasks) would also work, but is more infrastructure than the question warrants.

But here is the second, sharper problem the stress-test surfaces: **even the Phase-1 workload, as the harness stands today, cannot exercise the router** — for the *same* two reasons. (i) `cmd/phase1` does not walk the runner's Evaluate-per-AP path at all (it runs a fixed N-draft → synth → critique pipeline in `cmd/phase1/main.go`, not the `plan→emit_subtree→process` runner tree), so there is no `adapter.Evaluate` call for a router to seed. (ii) Even if it did, curation still writes one `--curate-action` at a time, so producing a *multi-action* store requires multiple curation passes (one per playbook) writing into per-action files — which the current single-scalar `--curate-action` flag supports only by running curation N times with different values, and only if the cold-path selection actually labels cases by playbook. **No existing harness path produces a heterogeneous-playbook exemplar store AND walks Evaluate-per-AP over heterogeneous inputs.** That harness must be *constructed* before the router has a fair test — and it is more than a flag flip.

#### 2.12.9.3 The minimal heterogeneous harness the router's fair test requires (if Thread A is ever built)

The smallest thing that gives the router a real disagreement rate, scoped honestly:

1. **A multi-action warm store.** Run curation once per playbook against a workload whose cases span playbooks — i.e., a labeled corpus where cold-path selection writes `plan` exemplars to `plan.json`, `process` to `process.json`, etc. Minimal version: hand-seed a small (~20–40 exemplar) multi-action store directly (the store is JSON-per-action, `FileStore.Dir/<action>.json` — writable without a curation run at all), so `RetrieveAcross` has mixed labels to vote over. This sidesteps the single-`--curate-action` limitation entirely for a *test* store.
2. **An Evaluate-per-AP walk over heterogeneous inputs.** The `Tree` arm is wrong for this (it pins children to `process`). Either: (a) add a thin bench/eval driver that runs the *unconstrained* runner (real `adapter.Evaluate` on every AP, no `treeAdapter` pin) over a heterogeneous input set, OR (b) run the demo (`cmd/oscillitron`) — whose stub/Hermes adapter *does* call Evaluate per AP — over a handful of mixed-shape tasks with `--router` wired, reading the `router.evaluate_overrode_hint` counter. Option (b) is near-zero new code (the runner hook + counter already exist per §4d) and is the cheapest honest probe of whether the hint *ever* disagrees with a real Evaluate.
3. **Only then** is the §2.12.2 disagreement-rate gate meaningful: on this store + this walk, does the kNN hint disagree with Evaluate ≥ 15% of the time, and when it does, does biasing toward it net-flip outcomes right-ward?

#### 2.12.9.4 CONSEQUENCE — the build order changes; the router's fair test is redefined

The stress-test forces three corrections:

- **The router's fair test is a heterogeneous-playbook workload over an Evaluate-per-AP walk — NOT GPQA via the Tree arm.** GPQA's < 5% disagreement is structurally guaranteed (single-action store + child-pinned Evaluate) and proves nothing. Cutting the router on a GPQA Tree run would be a **false negative dressed as a clean kill.** §2.12.2 and §4a are corrected below to say this.
- **Do NOT spend a router PR + a GPQA run as the first router datum.** The §2.12.7 step 6–8 "GPQA populate → null → treatment" sequence should be **struck as the router's primary test** — it can stay only as a documented sanity check that the wiring runs (the hint fires, the counter increments), explicitly labeled as inert-by-construction, never as the keep/cut decision.
- **Reorder: Thread B + H0-cost first (already the plan), and gate Thread A behind the heterogeneous harness, not behind a GPQA inertness run.** Thread A's build slot now reads: *build §4d's `RetrieveAcross` + `pkg/router` + runner hook (all still correct and lock-clean), but the experiment that decides keep/cut is §2.12.9.3's multi-action-store + unconstrained-Evaluate-walk, not §2.12.7 steps 6–8.* If no one is willing to construct that harness, **Thread A should not be built at all** — there is no workload in the repo today on which it can be fairly evaluated, which is itself a strong signal that the router is solving a problem the current workloads don't have.

**Net verdict of the stress-test:** the §2.12.2 inertness prediction is *correct but vacuous* on GPQA. GPQA is the wrong test — it is rigged toward inertness by the single-action curation flag and the `Tree` child-pin, so a cut there is uninformative. The router's only honest test is a heterogeneous-playbook workload that **does not exist in the harness yet** and must be built (minimally: a hand-seeded multi-action store + an unconstrained-Evaluate walk via `cmd/oscillitron` or a thin new driver). The build-order consequence is concrete: **Thread A's keep/cut moves off GPQA entirely**, and absent a builder for the heterogeneous harness, **Thread A's correct status is "spec'd, not built — and not buildable-with-a-fair-test until the workload exists."** This de-risks the largest unbuilt graft by refusing to generate a non-result.

---

## 3. Mapping onto Oscillitron

How each mechanism could slot in, what it changes, and which locks it tensions. **Tensions are flagged, not resolved.**

### 3.1 Cascade routing → Oscillitron's `delegate` gate is already a 2-stage cascade
The locked `delegate` escalation (critic failed past retry budget → frontier) **is** a cascade escalation step, and `verify_grounded`/`critique` are the post-hoc scorers. The cascade literature suggests Oscillitron could:
- Replace/augment the binary delegate gate with a **learned escalation scorer** (FrugalGPT-style DistilBERT) instead of relying only on critic pass/fail + confidence threshold. *Tension:* the project is **no-weight-updates** and stdlib-first; a trained scorer is a new artifact type and a dependency. It would live in the substrate (data) layer, not as model weights, so it's *compatible in spirit* but new in kind. **FLAG: needs a decision.**
- AutoMix's POMDP-from-~50-examples is attractive given Oscillitron's small-data reality, but adds a learned-policy artifact. **FLAG.**
- **No lock conflict** with the cascade *shape* — it matches the existing escalation philosophy (gate before output is consumed).

### 3.2 Learned cost-quality router → tensions the uniform-node + cheap-Evaluate lock
A RouteLLM-style pre-generation router that picks the model tier per query would sit *upstream* of the AP workflow. But Oscillitron LOCKED **uniform node** (one workflow at every level) and **cheap-local-first Evaluate picks a playbook, not a model**. A learned model-router is a *different decision* (which model) than Evaluate's (which playbook).
- It could slot in as a new pre-Evaluate stage: "which substrate tier handles this AP?" That is **net-new structure** the uniform-node lock was designed to avoid. **FLAG: direct tension with uniform-node + "specialists are substrate, not nodes."**
- The cheaper, lock-compatible version: a **kNN router over the existing per-action exemplar store** (`pkg/exemplar`, BM25 today) — but routing a **playbook hint for Evaluate**, NOT a substrate tier (a tier router re-opens the dense-inversion cost question §2.8 closed). **Now a concrete interface sketch — §2.10 (iteration 4):** `RetrieveAcross` (one additive store method) → `pkg/router.ExemplarRouter` → an advisory `Hint{Playbook, Confidence, Margin}` seeded into Evaluate via an optional `runner.Config.Router`, never replacing it. kNN-over-BM25 first (zero dep); frozen embeddings only if measured too lexical (argued as permitted retrieval infra, not a weight). **Zero hard lock tension; one soft dependency tension deferred behind a measurement gate.**

### 3.3 Confidence/difficulty signals → already partially built; calibration is the open risk
Oscillitron already has `pkg/notice` (effective confidence, calibrated downgrades), `pkg/verifier` (Wilson-bounded happiness), `pkg/benchmark/calibration` (confidence-band pass-rate), and stakes (`pkg/stakes`). The research says:
- A **difficulty predictor up front** (cheap, single-pass) could feed stakes/attempt-scaling and the dense-vs-cheap routing decision — strong fit with `pkg/stakes.AttemptScale`. **Low tension; promising.**
- **Reliable confidence costs multiple samples** (semantic entropy / self-consistency). Oscillitron's Vote orchestrator *already* produces N samples — semantic-entropy-over-the-vote is nearly free *given Vote*. **Now a concrete interface sketch — §2.11 (iteration 4):** discrete (black-box, no-logprob) semantic entropy over the cluster histogram Vote already builds (`votes map[string]int`), `conf = 1 − H/ln(N)`, fed into `cope.RuleTable.Decide` as the existing confidence scalar — **no new cope Action, no schema change.** v0 clusterer is exact-match (free, arithmetic); v1 swaps in NLI/embedding clustering for free-form workloads, dependency isolated in a subpackage. **Fully lock-compatible — zero tension.** Sharpens the ~5% delegate rate that dominates the locked path's residual cost.
- *Caution:* the calibration literature says raw confidence is overconfident. `pkg/notice`'s multiplicative downgrades are a heuristic Platt-ish correction; worth validating against the calibration table.

### 3.4 Model-as-tool dispatch → THE new direction; maximal tension with cost locks
This is the framing the loop is exploring. Mapping:
- HuggingGPT's **plan → select-model → execute → recompose** maps almost 1:1 onto Oscillitron's `plan` (emit_subtree) → dispatch → `process` leaves → `compose`/recomposer. The structural bones already exist.
- The difference is **who runs the orchestration**: Oscillitron locks it to **cheap-local Evaluate**; the dense framing puts a **frontier orchestrator** at the root. **FLAG: this is the head-on collision with "evaluate is cheap-local-first; frontier reserved for delegate + verify_judge" (LOCKED 2026-05-19) and with the whole cost thesis. §2.8 resolves it: frontier-orchestrator-per-request is dominated on cost — it costs 5–6× MORE than a single frontier call. The lock holds.**
- Model-cards-as-tools maps onto **per-action playbook substrate**: each playbook/specialist becomes a "tool" the orchestrator can call. This is *compatible* with "specialists are substrate" — the specialists are still data, just surfaced as tool schemas. **Low tension on the substrate side. This is the one piece of the dense idea that survives §2.8 with value intact (a packaging/ergonomics change, not a cost change).**
- **Reconciliation, now resolved (§2.8):** if "dense" is adopted at all, the orchestrator must be a **mid-tier** model (Haiku / GPT-4o-mini / Qwen-72B), never the frontier — and it is a quality/operational play layered on cheap leaves, not a cost win over the locked local path. Frontier stays at `delegate`/`verify_judge` as locked. The cost-viability question is **answered, not open**.
- **Calling-protocol, now specified (§2.9, iteration 3):** a model-as-tool *call* IS a `plan`→`emit_subtree` sub-AP; a model-as-tool *result* IS a `process`/`compose` `return_result` bubbling the scope channel; the child `Envelope.ID` IS the `tool_use_id` correlating call↔result. **No new playbook** (`plan`+`process`+`compose` cover it; `delegate`/`dispatch` would be redundant or break the three-Category lock). **Uniform-node PRESERVED** for the stateless one-shot-plan shape (the orchestrator is just the root AP running `plan`, not a privileged node). **TENSIONED and DEFERRED** for the stateful re-planning loop (a persistent context-holding orchestrator violates "siloed/dissolves-on-return" and re-introduces the mid-tree result→parent feedback edge the dataflow cut — that's a Hermes-substrate concern under wrap-not-fork, not an orchestration-layer one). Mid-tree sub-inference is **consistent** with the tools-execute-after-verified-output lock (that lock gates side-effecting connectors, not inference) — with a guardrail that the protocol must not smuggle connector-tools into the tree.

### 3.5 MoE routing → vocabulary only; load-balancing maps to the VRAM governor
No structural transfer (wrong granularity, requires joint training Oscillitron forbids). But **load-balancing + capacity** concepts map cleanly onto the existing `pkg/vram.Governor` + `runner.MaxConcurrency` (which already do capacity-bounded dispatch). **Top-k** maps onto sibling fan-out. No lock conflict; nothing to build, just confirms existing design is on a sound analogy.

### 3.6 Semantic/embedding routing → cheapest graft; reuses exemplar substrate
Embed the AP input, match against per-action route exemplars, pick a substrate tier or playbook bias — a near-free routing signal. Reuses `pkg/exemplar` infrastructure (swap BM25 for embeddings, or add an embedding index). **Low tension** — it's substrate-layer, no new node type, no weight updates. The main cost is introducing an embedding model dependency (currently stdlib-first). **FLAG: dependency decision.**

### 3.7 LLM-as-router vs classifier → the cost-of-routing tension is THE project risk
This directly stress-tests the dense framing. If the dense orchestrator is a frontier call on every request, the routing-cost math (§2.7) says the savings evaporate. The lock-compatible posture is **cheap router first** (Evaluate is already cheap-local) **with frontier only on escalation** — which is *exactly what Oscillitron already locked*. So the research **validates the existing locks** and puts the burden of proof on the dense-orchestrator inversion. **FLAG: the dense framing must show a measured cost win over the existing cheap-Evaluate path before any lock is reconsidered.** The honest framing for the loop: the new direction is a *hypothesis to falsify*, and the existing architecture is the null.

---

## 4. Open questions (carry forward)

**Retired in iteration 2:**
- ~~**#1 Is "dense" the frontier or a mid-tier?**~~ **RESOLVED (§2.8): mid-tier, never frontier — and even then it's a quality/ops play on cheap leaves, not a cost win.** Frontier-orchestrator-per-request costs 5–6× a single frontier call and is dominated by the locked local path. See §2.8.5.
- ~~**#7 Cost-model the inversion head-to-head.**~~ **DONE (§2.8): arithmetic on Phase-1 (email) and GPQA (Vote/Tree) with verified May-2026 pricing. Ordering under every regime: `local-cheap (locked) < hosted-mid < frontier-single < frontier-orchestrator`. Dense never wins on cost.**

**Retired in iteration 3:**
- ~~**The models-as-tools calling protocol on the AP envelope.**~~ **RESOLVED (§2.9): reuse `plan`+`process`+`compose` — NO new playbook, NO new node type.** A tool-call = `plan`→`emit_subtree` sub-AP; a tool-result = `process`/`compose` `return_result` on the scope channel; child `Envelope.ID` = `tool_use_id`. Uniform-node **PRESERVED** for the stateless one-shot-plan shape; the **stateful re-planning loop is TENSIONED and DEFERRED** (persistent context-holding orchestrator violates siloed/dissolves-on-return + re-adds the mid-tree feedback edge — a Hermes-substrate concern). Tools-execute-after-verified-output lock is **honored** (it gates connectors, not sub-inference) with a no-connector-tools-in-the-tree guardrail. Specialist pinning, if ever needed: Option A (prompt steering, zero schema) first; Option B (optional additive `SubAPSeed.PlaybookHint` *bias*) only if measured.

**Retired in iteration 4:**
- ~~**Cheapest viable router graft (the iteration-3 handoff #1).**~~ **RESOLVED (§2.10): buildable interface sketch committed.** A kNN router over `pkg/exemplar` routes a **playbook hint** (not a substrate tier — a tier router re-opens the §2.8 cost question). One additive `exemplar.Store.RetrieveAcross(prompt, k)` (cross-action BM25 kNN) + a new stdlib-only `pkg/router.ExemplarRouter` producing advisory `Hint{Playbook, Confidence, Margin}` + an optional `runner.Config.Router` that *seeds* Evaluate (never skips it — hard-skip forbidden as a back-door node-type). kNN-over-BM25 first (zero dep); frozen embeddings deferred behind a measurement gate (argued as permitted retrieval infra, not a weight). **Lock verdict: zero hard tension; one soft dependency tension deferred.**
- ~~**Semantic entropy over Vote samples (the iteration-3 handoff #2).**~~ **RESOLVED (§2.11): buildable interface sketch committed.** Discrete (black-box, no-logprob) semantic entropy `H = −Σ(n_c/N)ln(n_c/N)` over the cluster histogram Vote already builds; `conf = 1 − H/ln(N)` returned as `Answer.Confidence` and fed into the *existing* `cope.RuleTable.Decide` confidence input — **no new cope Action, no schema change.** v0 clusterer is `ExactMatch` (free, arithmetic, = the existing vote histogram for MCQ); v1 swaps an NLI/embedding `Clusterer` isolated in `pkg/semanticentropy/nli` for free-form workloads. **Lock verdict: fully lock-compatible, zero tension.** Sharpens the ~5% delegate rate dominating the locked path's residual cost.

**Retired in iteration 5:**
- ~~**The falsification experiment (the iteration-4 handoff #1).**~~ **RESOLVED (§2.12): the doc is now a decision procedure.** Three null hypotheses, each the doc's own conclusion turned into a target to attack — **H0-cost** (dense mid-tier buys no quality over local Vote; cut at `≤ +5 pp` pass-rate, full-GPQA, pure existing harness), **H0-router** (kNN hint is inert / no better than Evaluate; cut at `< 5%` disagreement via the new `router.evaluate_overrode_hint` counter, *paired within-run*), **H0-SE** (semantic entropy no better-calibrated than self-report; build at `ECE-delta ≥ 0.03` with no extra false-confident high-stakes ships, scored offline from `--report-out`). Per-hypothesis arms, real `cmd/bench`/`cmd/phase1` flags, the minimal additive instrumentation list (§2.12.4 — router: `RetrieveAcross` + `pkg/router` + `Config.Router` + two trace events + `--router*` flags; SE: `pkg/semanticentropy` + `Answer.SEConfidence` + ~3 lines in Vote + `--cope-confidence-source` + an offline ECE/Brier scorer), the two-run curation cycle for the router's warm store, a required free-form companion workload (Phase-1) before any *keep*, an ordered 10-step run plan, and a falsification outcome table. Calibration methodology cited (Guo ICML 2017 ECE; Brier 1950; Murphy 1973; Naeini AAAI 2015; Nixon CVPR-W 2019).

**Retired in iteration 6:**
- ~~**The PR-ready Thread B spec (the iteration-5 handoff).**~~ **RESOLVED (§4b): build-plan Phase 1 is now a diff-level, TDD-ordered, single-PR spec checked against the real source.** Full `pkg/semanticentropy` source; the lockstep `Answer.SEConfidence` + `answerJSON` diff; the `Vote` population using the *vote total* as N (not `successes`); the `calibration.Score`/`ECE`/`Brier`/reliability-slope offline scorer with cited methodology; the `--cope-confidence-source self|semantic-entropy` flag on `Coping` (zero `pkg/cope` change). Three iter-4/5 code-vs-doc mismatches corrected in §4b.0 (direct struct conversion → lockstep; N=Σ votes not `successes`; source-switch on `Coping` not `pkg/cope`). 8-step TDD table + explicit not-in-scope list. The remaining work is *execution* (a coding session opening the PR + the §2.12.7 run), not design.

**Still open / carried forward:**
1. **Fix stale Phase-1 pricing — VERIFIED + spec'd as a standalone PR (§4c.1).** `cmd/phase1/main.go` `defaultPricing` (lines 58–62) has Haiku at `0.80/4.00` (should be `1.00/5.00`) and the **`claude-opus-4-7`** entry at `15.00/75.00` (should be `5.00/25.00`); Sonnet `3.00/15.00` is already correct. Current rates confirmed June-2026 (WebSearch, §5 Pricing): Haiku 4.5 $1/$5, Sonnet 4.6 $3/$15, Opus 4.7 *and* 4.8 $5/$25. **Correction to iter-2/6 prose:** the stale Opus entry is keyed `claude-opus-4-7`, NOT `claude-opus-4-8` — the fix corrects the rate on the existing key, it does not rename it. **Blast radius:** only the Haiku rate is in phase1's *default* cost ratio (default orchestrator = Haiku, default frontier = Sonnet); the Opus rate is dead code unless an operator passes `--frontier-model claude-opus-4-7`. `cmd/bench` is unaffected (it takes blended `--price`/`--frontier-price`, never reads this map). Ship as a **patch PR on `v1.0.0`** (versioning lock), independent of the experiment. Full before/after diff + PR shape in §4c.1.
2. **Calibration validation:** are `pkg/notice`'s multiplicative confidence downgrades actually improving calibration, measured against `pkg/benchmark/calibration`? **Now subsumed by H0-SE** (§2.12.3): the offline ECE/Brier/reliability-slope scorer measures self-reported (notice-adjusted) confidence head-to-head against SE-confidence on the *same* run. Whichever wins the calibration metrics answers this directly.
3. **Dependency posture:** the embedding-clusterer (§2.11 v1) and embedding-router (§2.10 v1) both want a non-stdlib frozen embedding model. Both argued as *permitted retrieval/measurement infra* under no-weight-updates, but both trip stdlib-first. **Now gated on the §2.12.6 free-form arm**: the BM25/exact-match v0 is adopted unless *measured insufficient* there (router too lexical on paraphrased prompts; SE over-counting on free-form drafts). The experiment produces that measurement.
4. **Adversarial/robustness note for later:** cascades amplify jailbreak success (~79% increase in one study); a dense orchestrator over cheap tools inherits this. Flag for the security pass.

---

## 4a. Build plan / next phase (all four priorities addressed)

With the cost story falsified by arithmetic (§2.8), the calling protocol specified (§2.9), two grafts sketched (§2.10–2.11), and the experiment designed (§2.12), the doc's open design work is **done**. What remains is *build order*. The recommendation, ordered by leverage-per-risk (cheapest + zero-lock-tension + highest-power-measurement first):

**Phase 1 — Thread B (semantic entropy). Cheapest, zero lock tension, highest-power test. Do first. → NOW SPEC'D TO DIFF-LEVEL IN §4b (iteration 6); the checklist below is realized there with real signatures + TDD order + corrections.**
- [ ] `pkg/semanticentropy`: `Clusterer` interface + `ExactMatch` + `Entropy(sizes)` + `Confidence(sizes, n)` (stdlib-only; §2.11.3; **full source in §4b.1**).
- [ ] `pkg/benchmark`: additive `Answer.SEConfidence` (+ `omitempty` JSON) — carry both confidence columns in one run.
- [ ] `pkg/benchmark/orchestrator.Vote`: set `Answer.SEConfidence` from the existing `votes` histogram (~3 lines) + `vote.semantic_entropy` trace event.
- [ ] `calibration.ECE` / `calibration.Brier` (+ reliability-slope) next to `Compute`; offline scorer reading `--report-out`.
- [ ] `orchestrator.Coping` + `--cope-confidence-source self|se|blend`.
- [ ] **Run steps 3–5** of §2.12.7 → the H0-SE keep/cut. *Rationale: Vote already paid for the N samples; this is one new package + ~5 lines of wiring + an offline scorer, touches no lock, and its head-to-head metric is the tightest-CI test in the whole experiment. Highest expected value, lowest cost.*

**Phase 2 — H0-cost confirmation. No new code; pure existing harness.**
- [ ] Step 0 pricing fix (§2.12.4-C) — one line.
- [ ] **Run steps 1–2** of §2.12.7 (local Vote-5 vs hosted-Haiku Vote-5 on full GPQA) → confirm/deny the arithmetic. *Rationale: zero build cost — it's two `cmd/bench` invocations with different `--orchestrator-substrate`. Run it alongside Phase 1.*

**Phase 3 — Thread A (router), behind its gate. Largest lift; build only after Phase 1. ⚠ CORRECTED BY §2.12.9 (iteration 9): the keep/cut test is NO LONGER a GPQA run.**
- [ ] `pkg/exemplar.RetrieveAcross` (cross-action BM25 kNN, reusing `bm25.go`). *(Spec'd correct to diff level in §4d — unchanged.)*
- [ ] `pkg/router` (stdlib-only `ExemplarRouter`, §2.10.3 / §4d.2).
- [ ] `runner.Config.Router` (advisory seed, never skip Evaluate) + `router.hint_produced` / `router.evaluate_overrode_hint` trace events (§4d.3).
- [ ] `cmd/bench` `--router`/`--router-store`/`--router-k`/… (§4d.4).
- [ ] **Do NOT run §2.12.7 steps 6–8 (GPQA) as the keep/cut test.** Per §2.12.9, GPQA forces a near-zero disagreement by construction (single-action store + `Tree` child-pin) — a cut there is a false negative. The GPQA run, if done at all, is a *wiring sanity check only* (hint fires, counter increments), labeled inert-by-construction.
- [ ] **The real keep/cut arm is §2.12.9.3: a hand-seeded multi-action exemplar store + an unconstrained Evaluate-per-AP walk** (via `cmd/oscillitron` with `--router`, or a thin new driver — NOT the `Tree` arm). Build the BM25 v0 only; defer embeddings. **If no one will build that harness, do NOT build Thread A** — there is no in-repo workload on which it can be fairly evaluated, which is itself the signal that the router solves a problem the current workloads don't have.

**Phase 4 — Free-form keep-gate (only if Phase 1 or 3 returns "keep/needs-more-data").**
- [ ] **Run step 9** on the Phase-1 email workload — but note (§2.12.9.2) that `cmd/phase1` as it stands does NOT walk the runner's Evaluate-per-AP path, so the router needs the §2.12.9.3 harness here too; SE's free-form clusterer arm is unaffected. This is where open #3 (dependency posture) is resolved by measurement: the frozen-embedding dependency is adopted iff the BM25/exact-match v0 is measured insufficient.

**Dense-as-packaging-layer (§2.4 models-as-tools surfacing) — defer indefinitely.** §2.8 proved it's not a cost win and §2.9 proved it needs no new primitives (it's the locked `plan→process→compose` tree). Build it only if a concrete *operator* need appears (an operator who wants their per-action specialists surfaced as tool schemas to a hosted-mid orchestrator). It's an ergonomics wrapper over existing primitives, not a research deliverable — it earns its slot from a user, not from the loop.

**The stateful re-planning loop (§2.9.4) stays deferred** — a Hermes-substrate concern under wrap-not-fork, outside orchestration-layer scope. Not on this plan.

---

## 4b. Thread B — PR-ready spec (semantic entropy as a confidence signal) (ITERATION 6)

This is the **diff-level, TDD-ordered implementation spec** for build-plan Phase 1 (§4a), scoped to **one PR off `main`** (per the locked PR workflow: branch from `origin/main`, one PR, no stacking, TDD). Every signature below was checked against the **actual current source** (read this iteration); §4b.0 flags the three places where the iteration-4/5 sketches were *wrong about the real code* and corrects them. The PR's deliverable is the *instrumentation that makes H0-SE (§2.12.3) runnable* — it does **not** run the experiment (that's §2.12.7 steps 3–5, a separate scored-run session).

### 4b.0 Code-vs-doc mismatches found this iteration (CORRECTIONS)

Reading the real source surfaced three things the iter-4/5 sketches got wrong. All three are now corrected in the spec below; flagging them explicitly so the doc stays honest:

1. **`answerJSON` is a direct struct conversion, not a tagged mirror that can drift.** `pkg/benchmark/report.go:93` does `Answer: answerJSON(or.Answer)` — a Go **type conversion**, which is only legal when `answerJSON` and `Answer` have **identical field names, types, and order** (only struct tags may differ). Iter-5 §2.12.4-B / §4a said "additive `Answer.SEConfidence` (+ `omitempty` in `answerJSON`)" as if they were independent — they are **not**. Adding `SEConfidence float64` to `Answer` **forces** adding `SEConfidence float64` to `answerJSON` at the **same ordinal position** (after `Confidence`, before `CopeAction`), or the conversion stops compiling. The spec below adds both, in lockstep. *(This is a compile-time coupling, so a test isn't even needed to catch it — but the PR must touch both structs in the same commit.)*

2. **The SE normalization base is the vote total, NOT `successes`.** Iter-5 §2.12.4-B(3) wrote `Answer.SEConfidence = semanticentropy.Confidence(sizesFrom(votes), successes)`. But in the real `Vote.Answer` (`vote.go:173–208`), `successes` counts every attempt that produced text — **including attempts whose extraction was empty** (`extracted == ""`), which are explicitly *excluded* from the `votes` histogram (`vote.go:191–207`, the "Failed extraction — don't count as a vote" branch). So `successes ≥ Σ votes`, and using `successes` as `N` would under-count agreement and **inflate** entropy on substrates with extraction failures (the comment notes 35 such firings on phi4-mini's 198-case run). **Correction:** `N` must be `Σ_c n_c` = the sum of the histogram values = the number of *counted votes*, computed alongside the histogram. The spec adds a `votesTotal` accumulator and passes that. `Confidence(sizes, n)` is also defensive: if `n != Σ sizes` it recomputes `n` from `sizes` (treats the passed `n` as a hint, never a contradiction) — see §4b.1.

3. **`Coping` reads `inner.Confidence`, so the `--cope-confidence-source` switch lives on `Coping`, not on `cope.RuleTable`.** `coping.go:98` calls `rules.Explain(inner.Confidence, kase.Stakes)`. The rule table itself is a pure `Decide(conf, stakes)` lookup and must stay confidence-source-agnostic (it just gets a scalar). The selection of *which* scalar (`Confidence` vs `SEConfidence` vs a blend) belongs one layer up, in `Coping.Answer`, as a `Coping.ConfidenceSource` field. Iter-5 §2.12.4-B(4) said "`pkg/cope` / `orchestrator.Coping`: a `--cope-confidence-source` flag … One field on `Coping`" — the `pkg/cope` half is wrong (no cope change), the `Coping` half is right. Corrected: **zero change to `pkg/cope`**; one field + one switch on `Coping`.

### 4b.1 New package `pkg/semanticentropy` (full intended source, stdlib-only)

```go
// Package semanticentropy computes discrete (black-box) semantic
// entropy over a set of sampled answers and maps it to a [0,1]
// confidence the cope dispatcher can consume. "Discrete" = it needs
// only the output strings and a clustering function — no logprobs,
// no model internals (Farquhar et al., Nature 2024; the frequency-
// based variant in arXiv:2510.09256). That matches Vote exactly:
// Vote has N text answers and no logit access.
//
// v0 ships the ExactMatch clusterer, which for MCQ / extracted-
// canonical workloads (GPQA) IS meaning-clustering — identical
// extracted letters are the same meaning. Free-form workloads need a
// semantic clusterer (NLI / embedding cosine); that lives in the
// pkg/semanticentropy/nli subpackage (v1), dependency-isolated like
// pkg/trace/otel. This package stays stdlib-only.
package semanticentropy

import "math"

// Clusterer groups answers into meaning-clusters and returns the
// cluster sizes. Order is irrelevant — entropy is symmetric over the
// cluster distribution. Empty/blank answers are the clusterer's
// responsibility to drop (a failed extraction is not a meaning).
type Clusterer interface {
	Cluster(answers []string) (sizes []int)
}

// ExactMatch is the v0 clusterer: byte-identical strings cluster
// together. For MCQ/extracted-canonical answers this is exact
// meaning-clustering, and it produces the same histogram Vote's
// formatVoteDistribution already builds.
type ExactMatch struct{}

// Cluster implements Clusterer. Blank answers ("") are dropped —
// a failed extraction is not a cluster (mirrors Vote's tally rule).
func (ExactMatch) Cluster(answers []string) []int {
	counts := map[string]int{}
	for _, a := range answers {
		if a == "" {
			continue
		}
		counts[a]++
	}
	sizes := make([]int, 0, len(counts))
	for _, n := range counts {
		sizes = append(sizes, n)
	}
	return sizes
}

// Entropy returns the discrete Shannon entropy over the cluster
// distribution, in nats (natural log):
//
//	H = −Σ_c (n_c/N)·ln(n_c/N)     where N = Σ_c n_c
//
// Edge cases:
//   - len(sizes) == 0  → 0   (no answers; no spread to measure)
//   - len(sizes) == 1  → 0   (full agreement = certain)
//   - any size ≤ 0     → that cluster is skipped (defensive; a
//     well-formed clusterer never emits non-positive sizes)
func Entropy(sizes []int) float64 {
	total := 0
	for _, n := range sizes {
		if n > 0 {
			total += n
		}
	}
	if total == 0 {
		return 0
	}
	var h float64
	for _, n := range sizes {
		if n <= 0 {
			continue
		}
		p := float64(n) / float64(total)
		h -= p * math.Log(p)
	}
	return h
}

// Confidence maps cluster sizes to a [0,1] confidence:
//
//	conf = 1 − H/ln(N)            (N = number of answers = Σ sizes)
//
// Normalizing by the maximum possible entropy ln(N) (the all-
// singletons / total-disagreement case) makes the value comparable
// across different N — important because Vote's N is stakes-scaled
// (stakes.AttemptScale: Low=1, Medium=N, High=2N).
//
// The n parameter is the caller's count of answers. It is treated as
// a HINT: if it disagrees with Σ sizes (e.g. the caller passed
// `successes` but the histogram dropped empty extractions — see
// §4b.0 correction #2), the function trusts Σ sizes. Pass n only so
// a caller that knows the true sample count can be explicit; the
// function never lets a wrong n corrupt the math.
//
// Edge cases (all return 0 = "no signal", which cope.Decide reads as
// mid-band → ShipWithCaveat, the safe default):
//   - N < 2  → 0   (can't measure spread from < 2 answers; ln(N)≤0)
//   - single cluster (H == 0, full agreement) → 1.0 (max confidence)
func Confidence(sizes []int, n int) float64 {
	total := 0
	for _, s := range sizes {
		if s > 0 {
			total += s
		}
	}
	// Trust the histogram over the hint (correction #2).
	if total != n {
		n = total
	}
	if n < 2 {
		return 0
	}
	h := Entropy(sizes)
	maxH := math.Log(float64(n))
	if maxH <= 0 {
		return 0
	}
	conf := 1 - h/maxH
	// Clamp for float safety (h can be a hair above maxH from rounding).
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	return conf
}
```

The v1 semantic clusterer is **out of scope for this PR** (§4b.6) but its shape is fixed so the v0 `Clusterer` interface is forward-compatible:

```go
// pkg/semanticentropy/nli (v1, SEPARATE PR — not this one). Dep-
// isolated like pkg/trace/otel. Bidirectional-entailment clustering
// per Kuhn ICLR 2023 / Farquhar Nature 2024 for free-form answers.
type EntailmentClusterer struct {
	// Entail reports whether a and b mutually entail (same meaning).
	// Backed by an NLI model, a frontier yes/no call, or embedding
	// cosine ≥ τ. The frozen-model-as-retrieval-infra argument
	// (§2.10.2) covers no-weight-updates for the embedding variant.
	Entail func(ctx context.Context, a, b string) (bidirectional bool, err error)
}
```

### 4b.2 The `Answer` diff (additive `SEConfidence`; existing `Confidence` untouched)

**Two structs change in lockstep** (correction #1). In `pkg/benchmark/benchmark.go`, add to `Answer` immediately after `Confidence` (line 100):

```go
	Confidence float64
	// SEConfidence is the v0 semantic-entropy confidence (0.0–1.0)
	// computed by the Vote orchestrator from its own answer
	// histogram: conf = 1 − H/ln(N) over the meaning-cluster
	// distribution (pkg/semanticentropy). ADDITIVE and PARALLEL to
	// Confidence — it does NOT replace the self-reported mean. Both
	// columns are carried in one run so the H0-SE experiment
	// (§2.12.3) can score them head-to-head. Zero = "not computed"
	// (orchestrators other than Vote, or N<2). Which column the cope
	// dispatcher reads is selected by Coping.ConfidenceSource.
	SEConfidence float64
	// CopeAction ...   (unchanged, stays last)
	CopeAction string
```

And the **required matching change** in `pkg/benchmark/report.go` `answerJSON` (line 172), same ordinal position, so the `answerJSON(or.Answer)` conversion at line 93 keeps compiling:

```go
type answerJSON struct {
	Raw          string  `json:"raw"`
	Extracted    string  `json:"extracted"`
	Calls        int     `json:"calls"`
	TokensUsed   int     `json:"tokens_used"`
	Confidence   float64 `json:"confidence,omitempty"`
	SEConfidence float64 `json:"se_confidence,omitempty"` // NEW
	CopeAction   string  `json:"cope_action,omitempty"`
}
```

**The `Vote.Answer` population.** In `pkg/benchmark/orchestrator/vote.go`, add a `votesTotal` accumulator next to the existing `votes` histogram (so we have the correct `N` per correction #2), then set `SEConfidence` on both return paths. Concretely:

- After the tally loop, the histogram is `votes map[string]int` (vote.go:170) and each `votes[extracted]++` (line 207) already runs only on counted votes. Add one line in that same branch — or derive the total after the loop:

```go
	// After the tally loop (votes is fully populated):
	sizes := make([]int, 0, len(votes))
	votesTotal := 0
	for _, n := range votes {
		sizes = append(sizes, n)
		votesTotal += n
	}
	seConf := semanticentropy.Confidence(sizes, votesTotal) // 0 when len(votes)<2 / N<2

	trace.Info(tracer, ctx, "vote.semantic_entropy",
		slog.Float64("h", semanticentropy.Entropy(sizes)),
		slog.Int("clusters", len(sizes)),
		slog.Float64("conf", seConf),
		slog.Int("n", votesTotal),
	)
```

- Then in the **majority-pick return** (vote.go:259–265) add the field:

```go
	return benchmark.Answer{
		Raw:          strings.Join(rawParts, "\n---\n"),
		Extracted:    bestKey,
		Calls:        successes,
		TokensUsed:   totalTokens,
		Confidence:   meanConfidence(confidenceSum, confidenceCount),
		SEConfidence: seConf, // NEW — parallel column, self-report untouched
	}, nil
```

- The **all-extractions-empty return** (vote.go:225–231) has `len(votes) == 0` → `sizes` empty → `Confidence(nil, 0)` returns 0; set `SEConfidence: 0` there explicitly (or let it default). No `vote.semantic_entropy` event on that path (nothing to measure) is fine; emitting one with `clusters: 0` is also acceptable for log uniformity.

Variable names cited are the **real** ones: `votes` (vote.go:170), `successes` (line 173), `confidenceSum`/`confidenceCount` (lines 177–178), `bestKey` (line 235), `totalTokens` (line 172), `rawParts` (line 171). The `meanConfidence(confidenceSum, confidenceCount)` self-report path is **unchanged** — both columns coexist.

A new import in vote.go: `"github.com/jrlmx2/oscillitron/pkg/semanticentropy"`.

### 4b.3 Offline calibration scorer: `calibration.ECE` + `calibration.Brier` (+ reliability slope)

**Home: a new file `pkg/benchmark/calibration/score.go`** (next to `calibration.go`), because the existing package already (a) owns the band machinery (`pickBand`, `DefaultBands`), (b) reads `Answer.Confidence` from a `Report`, and (c) is imported by `cmd/bench`'s `printReport`. Adding a sibling file is the clean home and keeps the scorer independently unit-testable. **Justification for "a helper, not a `cmd/` tool":** the scorer reads the same `benchmark.Report` the calibration table already consumes, runs in-process at the end of a bench (no separate binary, no re-parse of `--report-out`), and the print site already exists (`printReport`, cmd/bench/main.go:922). A standalone `cmd/calib` would duplicate report-loading for zero benefit. The functions are *also* callable offline against a loaded `--report-out` JSON (deserialize → `Report` → `Score`) for after-the-fact analysis, so both modes are served by one helper.

A **confidence-field selector** is needed because we score *two* columns (`Confidence` and `SEConfidence`) from the same report. Signature:

```go
// pkg/benchmark/calibration/score.go
package calibration

import (
	"math"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// confOf selects which confidence column to score. The two real
// columns on benchmark.Answer.
type confOf func(benchmark.Answer) float64

// SelfReported reads Answer.Confidence (the self-reported mean).
func SelfReported(a benchmark.Answer) float64 { return a.Confidence }

// SemanticEntropy reads Answer.SEConfidence (the v0 SE column).
func SemanticEntropy(a benchmark.Answer) float64 { return a.SEConfidence }

// CalibrationScore bundles the three reliability metrics for one
// (orchestrator, confidence-column) pair.
type CalibrationScore struct {
	OrchestratorName string
	Column           string  // "confidence" | "se_confidence"
	N                int      // cases scored (column > 0, no err)
	ECE              float64  // Expected Calibration Error (lower=better)
	Brier            float64  // Brier score (lower=better)
	ReliabilitySlope float64  // OLS slope of pass-rate vs band mean-conf (steeper=more informative)
}

// Score computes ECE / Brier / reliability-slope for one confidence
// column across every orchestrator in the report, using `bands` for
// the ECE binning (nil → DefaultBands; a finer 10-bin grid is the
// recommended cross-check per Nixon CVPR-W 2019).
//
// Methodology:
//   - ECE (Guo et al., ICML 2017; Naeini et al., AAAI 2015):
//       ECE = Σ_b (n_b/N)·|acc(b) − conf(b)|
//     the weighted gap between per-bin accuracy and per-bin mean
//     confidence over the bands.
//   - Brier (Brier 1950): (1/N)·Σ_i (conf_i − pass_i)²  with
//     pass_i ∈ {0,1}. Decomposes into calibration+refinement
//     (Murphy 1973) — the reliability curve is the calibration half.
//   - ReliabilitySlope: ordinary-least-squares slope of
//     band-pass-rate (y) on band-mean-confidence (x); positive +
//     steep ⇒ the column ranks correctness well. This reuses the
//     existing per-band Count/Passes/MeanConfidence that Compute
//     already produces — see §3.3 "slopes up = useful, flat = noise".
//
// Cases with column value ≤ 0 (not reported / not computed) and
// cases with a non-nil Err are excluded, identical to Compute's rule.
func Score(report benchmark.Report, sel confOf, column string, bands []Band) []CalibrationScore {
	if bands == nil {
		bands = DefaultBands
	}
	// ... per-orchestrator accumulation, mirroring Compute's walk:
	//   for each cr in report.Cases / or in cr.Results:
	//     if or.Err != nil { continue }
	//     conf := sel(or.Answer); if conf <= 0 { continue }
	//     pass := 0.0; if or.Verdict.Pass { pass = 1.0 }
	//     brierSum += (conf - pass)^2 ; N++
	//     b := pickBand(conf, bands); bin[b].{n,passes,confSum}++
	//   ECE  = Σ_b (bin.n/N)·|bin.passes/bin.n − bin.confSum/bin.n|
	//   Brier = brierSum / N
	//   slope = olsSlope(binMeanConf[], binPassRate[])  (Count-weighted)
	return nil // (full body in the implementing PR; math specified above)
}
```

**Invocation: a print in `cmd/bench`'s `printReport`, right after the existing calibration table** (cmd/bench/main.go:922). Add a `FormatScores` renderer and call it for both columns:

```go
	// after the existing calibration.FormatTable line (main.go:922):
	fmt.Fprintln(w, "--- "+strings.TrimRight(calibration.FormatScores(
		calibration.Score(r, calibration.SelfReported,   "confidence",    nil),
		calibration.Score(r, calibration.SemanticEntropy, "se_confidence", nil),
	), "\n"))
```

`FormatScores(...[]CalibrationScore)` prints one row per (orchestrator, column) with `N / ECE / Brier / slope`, so the **head-to-head** (self-report vs SE) is visible in the bench's own stdout — and re-derivable offline from `--report-out` by loading the JSON back into a `Report`. This is the §2.12.3 calibration half, computed with **no second bench run**.

### 4b.4 The `--cope-confidence-source` flag (`self | semantic-entropy`, default `self`)

Per correction #3: **zero change to `pkg/cope`.** One field on `Coping`, one switch in `Coping.Answer`, one flag in `cmd/bench`.

**(a) `pkg/benchmark/orchestrator/coping.go`** — add a field + select the scalar before `rules.Explain`:

```go
type Coping struct {
	NameStr  string
	Inner    benchmark.Orchestrator
	Frontier benchmark.Orchestrator
	Rules    cope.RuleTable
	// ConfidenceSource selects which Answer column feeds the rule
	// table: "" / "self" → inner.Confidence (self-reported, current
	// behavior); "semantic-entropy" → inner.SEConfidence (the SE
	// column Vote computes). A wrong/unknown value falls back to
	// self (safe default). Blend is deferred — see §4b.6.
	ConfidenceSource string
	Tracer           trace.Tracer
}
```

In `Answer`, replace the single `decision := rules.Explain(inner.Confidence, kase.Stakes)` (coping.go:98) with:

```go
	conf := inner.Confidence
	if c.ConfidenceSource == "semantic-entropy" {
		conf = inner.SEConfidence
	}
	decision := rules.Explain(conf, kase.Stakes)
	trace.Info(tracer, ctx, "coping.decision",
		slog.String("case", kase.ID),
		slog.String("orchestrator", c.NameStr),
		slog.String("action", string(decision.Action)),
		slog.Float64("confidence", decision.Confidence),
		slog.String("confidence_source", c.ConfidenceSource), // NEW — which column drove it
		slog.String("stakes", string(decision.Stakes)),
		slog.String("rationale", decision.Rationale),
	)
```

Everything downstream (the `switch decision.Action` block) is **unchanged** — it already routes on the Action, agnostic to which scalar produced it. The escalation A/B (§2.12.3) is then just two runs differing only in `--cope-confidence-source`.

**(b) `cmd/bench/main.go`** — declare the flag next to the existing cope flags (after `copeLow`, main.go:128), matching the exact pattern:

```go
	copeConfSrc = flag.String("cope-confidence-source", "self", "v3.4+/SE: which confidence column the cope dispatcher reads. 'self' = Answer.Confidence (self-reported mean, current behavior); 'semantic-entropy' = Answer.SEConfidence (Vote's discrete semantic entropy, 1−H/ln(N)). Default 'self' preserves behavior; 'semantic-entropy' is the H0-SE treatment arm (§2.12.3).")
```

Properties fallback, mirroring the `copeHigh`/`copeLow` block (main.go:277–289):

```go
	if !flagPassed("cope-confidence-source") {
		*copeConfSrc = props.String("bench.cope.confidence_source", *copeConfSrc)
	}
```

Thread it into the `Coping` construction (main.go:426–436), one line:

```go
		orchAsBenchmarkOrch = orchestrator.Coping{
			NameStr:          "cope-" + voteOrch.NameStr,
			Inner:            voteOrch,
			Frontier:         frontierOrch,
			Rules:            cope.RuleTable{HighConfidence: *copeHigh, LowConfidence: *copeLow, EscalateAllowed: true},
			ConfidenceSource: *copeConfSrc, // NEW
			Tracer:           tracer,
		}
```

### 4b.5 TDD task order (one PR off `main`, test-first)

Branch `feat/semantic-entropy` from `origin/main` (fresh fetch first, per the PR-workflow lock). Each step: **write the test, watch it fail, implement, watch it pass.** Steps are ordered so the package lands before its consumers (compile dependency order).

| # | Test file (write first) | Assertion(s) | Then implement |
|---|---|---|---|
| 1 | `pkg/semanticentropy/semanticentropy_test.go` — `TestExactMatch_Cluster` | `Cluster(["A","A","B"])` → sizes summing to 3, two clusters {2,1}; `Cluster(["A","","A"])` drops the blank → {2}; `Cluster([])` → empty | `ExactMatch.Cluster` (§4b.1) |
| 2 | same file — `TestEntropy` | `Entropy([])==0`; `Entropy([5])==0` (one cluster); `Entropy([1,1])==ln(2)` within 1e-9; `Entropy([3,1])` == −(.75·ln.75+.25·ln.25) | `Entropy` (§4b.1) |
| 3 | same file — `TestConfidence` | unanimous `Confidence([5],5)==1.0`; all-disagree `Confidence([1,1,1,1,1],5)==0.0` within 1e-9; `Confidence([],0)==0`, `Confidence([3],1)`/`N<2`==0; **mismatch guard:** `Confidence([3,1],99)` ignores the bad n and uses Σ=4 (correction #2) | `Confidence` (§4b.1) |
| 4 | `pkg/benchmark/report_test.go` — extend `TestWriteJSON_FieldNamesAreSnakeCase` (or add `TestAnswerJSON_HasSEConfidence`) | a `Report` whose `Answer.SEConfidence=0.7` serializes with `"se_confidence":0.7`; an `Answer` with `SEConfidence=0` **omits** the field (`omitempty`); the existing `confidence`/`cope_action` keys still present | add `SEConfidence` to `Answer` (benchmark.go) **and** `answerJSON` (report.go) in lockstep (§4b.2, correction #1) |
| 5 | `pkg/benchmark/orchestrator/vote_test.go` — `TestVote_SetsSEConfidence` | a stub adapter returning 3 identical extracted answers → `Answer.SEConfidence==1.0`; returning 3 distinct → `SEConfidence==0.0`; an empty-extraction attempt is excluded from the SE base (N counts votes, not `successes`) — assert `SEConfidence` matches `Confidence(histogram, votesTotal)`, **not** `…(…, successes)` | the `votesTotal`/`sizes`/`seConf` block + both return-path field sets + `vote.semantic_entropy` event (§4b.2) |
| 6 | `pkg/benchmark/calibration/score_test.go` — `TestScore_ECE_Brier_Slope` | a hand-built `Report` with known {conf, pass} pairs → assert ECE matches the `Σ_b (n_b/N)|acc−conf|` hand-calc; Brier matches `mean((conf−pass)²)`; a monotone-up reliability curve yields positive slope, a flat one ≈0; both `SelfReported` and `SemanticEntropy` selectors read their respective columns | `Score`, `SelfReported`, `SemanticEntropy`, `olsSlope`, `FormatScores` (§4b.3) |
| 7 | `pkg/benchmark/orchestrator/coping_test.go` — `TestCoping_ConfidenceSource_SelectsColumn` | with `ConfidenceSource="semantic-entropy"`, a low `SEConfidence` + high `Confidence` + high stakes → `Escalate` (proves SE column drove it); default `""`/`"self"` → routes on `Confidence` exactly as today (existing tests still green); unknown value falls back to self | the `conf`-selection lines + `confidence_source` trace attr (§4b.4a) |
| 8 | (manual / build) `go build ./... && go test ./... -race` green; `gofmt -l` clean (pre-commit hook) | whole suite passes under `-race`; new flag parses | wire `--cope-confidence-source` + props fallback + `Coping` threading in `cmd/bench/main.go` (§4b.4b) and the `FormatScores` print (§4b.3); `cmd/bench` has no unit tests, so this is build-verified |

Commit, push, open **one PR** against `main`, report the URL, **stop** and wait for merge (PR-workflow lock — no stacking; the router PR, §4a Phase 3, branches only *after* this merges).

### 4b.6 What this PR explicitly does NOT do

- **No semantic (non-exact-match) clustering.** Only `ExactMatch` ships. `pkg/semanticentropy/nli.EntailmentClusterer` (NLI / mutual-entailment / embedding-cosine) is a **separate later PR**, gated on the §2.12.6 free-form arm measuring exact-match insufficient. Its interface shape is fixed (§4b.1) so v0 is forward-compatible, but no code.
- **No embedding dependency.** The frozen-embedding question (open #3 / §2.10.2) is untouched — stdlib-only stays intact. Embeddings enter only if the free-form arm measures BM25/exact-match too lexical.
- **No blend.** The `α·SE + (1−α)·self` blend (§2.11.4 Option 1) is **not** built; `--cope-confidence-source` offers `self | semantic-entropy` only. Blend is added *after* the calibration head-to-head (§2.12.3) says SE wins calibration but the escalation A/B is mixed — i.e., it's a measured follow-up, not a speculative feature. (When added, it's a third enum value + an `α` field on `Coping`.)
- **No Thread A (router).** `RetrieveAcross`, `pkg/router`, `runner.Config.Router`, `--router*` — all §4a Phase 3, a *different PR after this one merges*.
- **No experiment RUN.** This PR is the *instrumentation* that makes H0-SE runnable. Running §2.12.7 steps 3–5 (the GPQA bench with both columns + the calibration/escalation reads) and distilling to `scratch/bench-results-<DATE>.md` is a separate scored-run session — the keep/cut datum, not the code.
- **No `Single`/`Tree` SE.** Only `Vote` computes `SEConfidence` (it's the only orchestrator with an N-answer histogram). `Single` (1 answer → N<2 → 0) and `Tree` (recompose, not a vote) leave `SEConfidence=0`; that's correct ("no SE signal"), not a gap to fill.
- **No `pkg/cope` change.** Correction #3: the rule table stays a pure `Decide(conf, stakes)`; the column selection lives entirely in `Coping`.

---

## 4c. H0-cost — PR-ready run checklist (ITERATION 7)

This is the **copy-paste-runnable decision checklist** for H0-cost (§2.12.1 / §2.12.7 steps 0–2): *"dense mid-tier orchestration buys no quality over the existing local-cheap Vote/Tree path."* Per the §4a build order, H0-cost is **"no new code; pure existing harness"** — the only code change is a one-line-class pricing-correctness fix in `cmd/phase1`. Everything else is two `cmd/bench` invocations and arithmetic over the resulting JSON. This converts the cheapest hypothesis into a procedure that runs the moment the fix lands, producing the *second* real keep/cut datum (after Thread B's) and closing the H0-cost arm with measurement, not arithmetic.

Every flag, field, and source line below was checked against the **actual current source** read this iteration: `cmd/phase1/main.go` (`defaultPricing`, `report()`), `cmd/bench/main.go` (the real flags + the `smallModelSubstrings`/`resolveSubstrate` auto-route allowlist), and `pkg/benchmark/report.go` (`aggregateStatsJSON` snake_case JSON tags). **§4c.0 flags and corrects two errors the doc carried from iterations 2 and 6.**

### 4c.0 Correction — the pricing-bug claim was REAL but the model key was wrong

Iterations 2 and 6 asserted `cmd/phase1` `defaultPricing` has "stale Haiku ($0.80/$4 → should be $1/$5) and Opus ($15/$75 → should be $5/$25)" rates. **Verified against the real file (`cmd/phase1/main.go` lines 58–62):**

```go
// Haiku/Sonnet pricing (mid-2026). Numbers per million tokens.
var defaultPricing = map[string]cost.Pricing{
	"claude-haiku-4-5-20251001": {InputUSDPerMTok: 0.80, OutputUSDPerMTok: 4.00},
	"claude-sonnet-4-6":         {InputUSDPerMTok: 3.00, OutputUSDPerMTok: 15.00},
	"claude-opus-4-7":           {InputUSDPerMTok: 15.00, OutputUSDPerMTok: 75.00},
}
```

**The bug is real on the rates** — confirmed against current published Anthropic pricing (June 2026, WebSearch cross-check, see §5 Pricing): Haiku 4.5 = **$1.00/$5.00**, Sonnet 4.6 = **$3.00/$15.00** (already correct), Opus 4.7 *and* 4.8 = **$5.00/$25.00**. So Haiku is stale (`0.80/4.00`) and Opus is stale (`15.00/75.00`).

**But the doc's prose was wrong on the Opus model key.** Iteration 2's §2.8.1 note and iteration 6's handoff both implied "Opus 4.8." The real map key is **`claude-opus-4-7`**, not `claude-opus-4-8`. The fix corrects the *rate* on the existing `claude-opus-4-7` entry — it does **not** rename the key (renaming would silently drop pricing for any operator who already passes `--frontier-model claude-opus-4-7`). Whether to *also add* a `claude-opus-4-8` entry is a separate, optional choice (Opus 4.8 is the current default Opus; both bill at $5/$25); the minimal correctness fix touches only the two stale rates.

**Blast-radius note (why the Haiku fix is the load-bearing one for phase1):** `cmd/phase1` `report()` prices only `orchModel` and `frontierModel`. The defaults (`pkg/adapter/anthropic`) are orchestrator = `claude-haiku-4-5-20251001` and frontier = `claude-sonnet-4-6`. So **on the default phase1 path the Haiku rate is in the cost ratio and the Opus rate is dead code** — Opus only matters if an operator passes `--orchestrator-model`/`--frontier-model claude-opus-4-7`. The Haiku correction directly moves phase1's reported cost ratio (orchestrator side ×1.25 in, ×1.25 out); the Opus correction is correctness-for-completeness. **Note also `cmd/bench` is unaffected by this map** — bench takes blended rates via `--price NAME=RATE` / `--frontier-price RATE` (it never reads `cmd/phase1`'s `defaultPricing`), so the H0-cost *bench* runs in §4c.2 do not depend on the fix landing. The fix is a prerequisite only for any *phase1* cost read; it's listed as step-0 to keep every cost column in the experiment honest, and because it's the cheapest possible correctness win.

### 4c.1 Step 0 — the literal pricing fix (one standalone trivial PR)

**File:** `oscillitron/cmd/phase1/main.go`. **Two edited lines (Haiku + Opus); Sonnet untouched.**

Before:
```go
"claude-haiku-4-5-20251001": {InputUSDPerMTok: 0.80, OutputUSDPerMTok: 4.00},
"claude-sonnet-4-6":         {InputUSDPerMTok: 3.00, OutputUSDPerMTok: 15.00},
"claude-opus-4-7":           {InputUSDPerMTok: 15.00, OutputUSDPerMTok: 75.00},
```

After:
```go
"claude-haiku-4-5-20251001": {InputUSDPerMTok: 1.00, OutputUSDPerMTok: 5.00},
"claude-sonnet-4-6":         {InputUSDPerMTok: 3.00, OutputUSDPerMTok: 15.00},
"claude-opus-4-7":           {InputUSDPerMTok: 5.00, OutputUSDPerMTok: 25.00},
```

This is a **few-character, two-line** edit (four numeric literals changed). **Ship it as a standalone trivial PR**, not riding the experiment: per the versioning lock it's a **patch on the released `v1.0.0` major** (`vMAJOR.MINOR.PATCH` = "non-functional / correctness change on a released major"). It touches no behavior, no schema, no test fixtures that assert these literals (grep `0.80`/`15.00` in `cmd/phase1` test files before committing to confirm; none reference `defaultPricing` directly). The PR is: branch `fix/phase1-pricing` off `origin/main`, edit, `go build ./... && go test ./...`, `gofmt -l` clean, commit, push, open one PR against `main`, stop and wait for merge (PR-workflow lock — no stacking). The H0-cost *bench* runs (§4c.2) can proceed in parallel since they don't read this map.

### 4c.2 Step 1–2 — the two bench arms (real flags, real models)

Both arms run the **full 198-case GPQA set** (`--limit 0`), same fixed everything, differing in **exactly one variable: `--orchestrator-substrate` + `--orchestrator-model`** (the dense-vs-local axis §2.8 left as the only live "dense" reading). Frontier baseline, vote-N, benchmark, and cases are identical across arms so the pass-rate delta isolates the substrate.

First, ensure the experiment output dir exists and the GPQA cases are present:

```sh
cd oscillitron
mkdir -p scratch/exp                              # holds the two --report-out / --stream-out artifacts
export ANTHROPIC_API_KEY=sk-...                   # required: frontier baseline (both arms) + dense orchestrator (arm 2)
ls cmd/bench/cases/gpqa_diamond.json              # MUST exist — operator-downloaded; see §4c.4 + cmd/bench/cases/README.md
```

**Arm 1 — Null (locked path): local Vote-5 on cheap substrate.**
`qwen2.5:7b` is on the `smallModelSubstrings` auto-route allowlist (`cmd/bench/main.go`: `"qwen2.5:7b"`), so `--orchestrator-substrate ollama` is what `auto` would pick anyway — stated explicitly here for an unambiguous run record. Local price is `0` (owned hardware, ~$0 marginal). Frontier blended rate `6.60` = Sonnet 4.6's 70/30 blend (0.7·3 + 0.3·15).

```sh
go run ./cmd/bench \
  --benchmark gpqa \
  --cases cmd/bench/cases/gpqa_diamond.json \
  --limit 0 \
  --vote-n 5 \
  --orchestrator-substrate ollama \
  --orchestrator-model qwen2.5:7b \
  --frontier-substrate anthropic \
  --frontier-model claude-sonnet-4-6 \
  --price 'orchestrator-vote-5-qwen2.5:7b=0' \
  --frontier-price 6.60 \
  --report-out scratch/exp/h0cost-null.json \
  --stream-out scratch/exp/h0cost-null.jsonl
```

**Arm 2 — Treatment (dense mid-tier): the same Vote-5 with a hosted mid-tier substrate.**
Literally `--orchestrator-substrate anthropic --orchestrator-model claude-haiku-4-5` swapped in; everything else identical. Haiku blended rate `2.20` = 0.7·1 + 0.3·5 (post-fix rates). The orchestrator price-key must match the Vote orchestrator's generated name (`orchestrator-vote-<N>-<model>`, from `cmd/bench` `voteOrch.NameStr`), i.e. `orchestrator-vote-5-claude-haiku-4-5`.

```sh
go run ./cmd/bench \
  --benchmark gpqa \
  --cases cmd/bench/cases/gpqa_diamond.json \
  --limit 0 \
  --vote-n 5 \
  --orchestrator-substrate anthropic \
  --orchestrator-model claude-haiku-4-5 \
  --frontier-substrate anthropic \
  --frontier-model claude-sonnet-4-6 \
  --price 'orchestrator-vote-5-claude-haiku-4-5=2.20' \
  --frontier-price 6.60 \
  --report-out scratch/exp/h0cost-dense.json \
  --stream-out scratch/exp/h0cost-dense.jsonl
```

**Optional Arm 3 — `--tree` on each substrate** (decompose+recompose shape, to check whether dense's value, if any, surfaces in Tree rather than Vote): append `--tree` to each command above and re-`--report-out` to `…-null-tree.json` / `…-dense-tree.json`. Tree adds the `tree-<model>` orchestrator line to the report. Defer unless the Vote arms land in the ambiguous `+5..+10 pp` band.

**Two operational cautions** (honest about what blocks a clean run):
- The `--price` *key* must exactly equal the orchestrator's auto-generated `NameStr`. If you mistype it, the cost columns silently read `$0.00` (no error) — the `aggregates[].total_actual_usd` field stays zero. Verify the name in the report's `aggregates[].orchestrator_name` after run 1 and reuse it verbatim.
- Cost is the *exact* (token-counted) half and has zero variance; only `pass_rate` carries noise. So even if a `--price` key is fumbled, the **pass-rate delta — the primary H0-cost gate — is unaffected** (it's pure `Verdict.Pass`, independent of pricing). The pricing is for the secondary cost-confirmation column.

### 4c.3 Step 2 (cont.) — the exact JSON fields + decision arithmetic

Each `--report-out` JSON is a `reportJSON` (`pkg/benchmark/report.go`) with an `aggregates` array of `aggregateStatsJSON`. The **real snake_case field names** (verified against the `json:` tags):

| Quantity | JSON path | Notes |
|---|---|---|
| Orchestrator name | `aggregates[].orchestrator_name` | match the `orchestrator-vote-5-…` line, **not** the `frontier-…` line |
| **Pass rate (primary gate)** | `aggregates[].pass_rate` | `float64`, 0–1; the Verdict.Pass fraction |
| Avg score | `aggregates[].avg_score` | secondary quality read |
| Total tokens | `aggregates[].total_tokens` | exact, for sanity |
| **Actual cost** | `aggregates[].total_actual_usd` | own tokens × own `--price` rate |
| Frontier counterfactual | `aggregates[].total_frontier_usd` | own tokens × `--frontier-price` |
| Savings vs frontier | `aggregates[].savings_ratio` | `1 − actual/frontier` |

Pull the orchestrator (not frontier) line from each file. With `jq`:

```sh
jq -r '.aggregates[] | select(.orchestrator_name|startswith("orchestrator-vote"))
       | "\(.orchestrator_name)  pass_rate=\(.pass_rate)  actual=$\(.total_actual_usd)  savings=\(.savings_ratio)"' \
   scratch/exp/h0cost-null.json scratch/exp/h0cost-dense.json
```

**Decision arithmetic (fill-in-the-blanks; the §2.12.1 thresholds at the n≈198 noise floor):**

```
NULL  (ollama qwen2.5:7b Vote-5):  pass_rate_null  = ______   actual_usd_null  = $______
DENSE (anthropic Haiku   Vote-5):  pass_rate_dense = ______   actual_usd_dense = $______

Δ_pp = (pass_rate_dense − pass_rate_null) × 100  =  ______ pp

DECISION (per §2.12.1 / §2.12.8):
  Δ_pp ≤ +5 pp ........... CUT dense as a cost play.  §2.8 vindicated by measurement;
                           dense stays a documented OPERATIONAL option only (no-local-GPU path),
                           never a cost win.  [EXPECTED outcome, given v1's "vote on cheap ≈ frontier."]
  +5 < Δ_pp ≤ +10 pp ..... OPERATIONAL-ONLY (needs-more-data). Real but marginal lift;
                           re-run on MMLU-Pro (--benchmark mmlu-pro) to confirm it replicates
                           before documenting hosted-mid as anything more than an ops convenience.
  Δ_pp > +10 pp AND
  unreachable by raising
  local --vote-n at ≤cost  REVISIT the cheap-local-orchestrator lock. (§2.8 predicts never reached;
                           before concluding this, run NULL again at --vote-n 9 and compare cost.)

COST CONFIRMATION (exact, zero-variance — secondary):
  actual_usd_dense / actual_usd_null  =  ______   (§2.8 predicts dense ≈ 5–6× the local path)
  This is CONFIRMATION, not the gate. The pass-rate Δ_pp decides; the cost ratio only shows
  the premium dense charges for whatever quality it bought.
```

**Noise-floor caveat (§2.12.0, binding on the read):** at n=198 a pass-rate's 95% Wilson half-width is ≈ ±0.07 near p≈0.5, so a Δ_pp inside roughly ±7 pp is *not distinguishable from noise* on a single full-GPQA run. The `≤ +5 pp` cut threshold is set deliberately inside that floor — a sub-5-pp "lift" is treated as no lift (cut), never as a marginal keep. Do **not** read a sub-noise delta as a dense win; "no significant difference" = cut (the §2.12 framing discipline: the existing architecture is always the null). Determinism is pinned for both arms by GPQA's SHA-256 case placement and seedable sibling dispatch — fix the seed so the *only* difference between arms is the substrate.

### 4c.4 Operator prerequisites — what blocks a run today

Honest about what an operator must have on hand before either command runs:

1. **GPQA Diamond cases on disk.** `cmd/bench` never network-fetches; it reads `--cases cmd/bench/cases/gpqa_diamond.json`, which is **operator-downloaded and `.gitignore`'d (never committed — dataset is HF-gated/operator-licensed).** Per `cmd/bench/cases/README.md`: `huggingface-cli login` → `huggingface-cli download Idavidrein/gpqa --repo-type dataset --local-dir /tmp/gpqa` → run the README's Python snippet to convert the CSV to the loader's JSON shape → place at `cmd/bench/cases/gpqa_diamond.json`. **If absent, the bench errors at load.** This is the single most likely blocker.
2. **An Anthropic API key** (`export ANTHROPIC_API_KEY=sk-...`). Required by **both** arms for the frontier baseline (`--frontier-substrate anthropic`), and by **arm 2** additionally for the dense Haiku orchestrator (`--orchestrator-substrate anthropic`). Arm 2 makes 5 Haiku calls/case × 198 cases ≈ ~990 orchestrator calls plus the per-case frontier + grader + goal-derive calls — budget for real API spend.
3. **A local Ollama serving `qwen2.5:7b`** for arm 1 (`--orchestrator-substrate ollama`, default URL `http://127.0.0.1:11434`). `ollama pull qwen2.5:7b` and `ollama serve` running. **If Ollama isn't up, arm 1's orchestrator calls fail** (the frontier baseline still uses Anthropic). Arm 2 needs no local model. Note the VRAM governor is auto-managed (`DefaultVRAMModel` under `MaxConcurrencyCeiling=8`) when the `--model-*` flags are omitted — Vote-5 on a 7B model is the exact N×resident OOM case the auto-governor guards, so omitting the flags is safe (it falls to serial under memory pressure), but passing accurate `--model-layers/--model-kv-hidden/--model-kv-dtype-bytes/--model-context-size` for qwen2.5:7b gives tighter throughput.
4. **`go` toolchain ≥ 1.26** (already required to build the repo).

### 4c.5 Step 3 — the findings-file skeleton (per the "Recording scored runs" convention)

Both `oscillitron/CLAUDE.md` ("Recording scored runs") and the parent "Default behaviors" require any scored run to distill into `scratch/bench-results-<YYYY-MM-DD>.md` — **not** be read off the terminal once and lost to session history. Ready-to-fill skeleton (matching the existing findings format: config header → results table → key finding → decision):

```markdown
# Bench results — H0-cost (dense mid-tier vs local-cheap Vote) — <YYYY-MM-DD>

## Config
- Hypothesis: H0-cost (§2.12.1) — "dense mid-tier orchestration buys no quality over local Vote/Tree."
- Benchmark: GPQA Diamond, full set (--limit 0, n=198), fixed seed.
- Vote-N: 5.  Frontier baseline: claude-sonnet-4-6 (single call), --frontier-price 6.60.
- Pricing fix (step 0): cmd/phase1 defaultPricing Haiku 0.80/4.00→1.00/5.00, Opus 15/75→5/25 — landed in PR #___ (merged <date>) / N/A for bench runs.
- Arm NULL:  ollama qwen2.5:7b, --price …=0,    report=scratch/exp/h0cost-null.json
- Arm DENSE: anthropic claude-haiku-4-5, --price …=2.20, report=scratch/exp/h0cost-dense.json
- Run at: <RFC3339>   Operator: <name>

## Results
| Arm   | orchestrator_name                          | pass_rate | avg_score | total_tokens | total_actual_usd | savings_ratio |
|-------|--------------------------------------------|-----------|-----------|--------------|------------------|---------------|
| NULL  | orchestrator-vote-5-qwen2.5:7b             |   ___     |   ___     |    ___       |   $0.00 (local)  |     ___       |
| DENSE | orchestrator-vote-5-claude-haiku-4-5       |   ___     |   ___     |    ___       |   $___           |     ___       |
| (frontier ref) | frontier-claude-sonnet-4-6        |   ___     |   ___     |    ___       |   $___           |     —         |

Δ_pp (dense − null) = ___ pp.   cost ratio dense/null = ___×.

## Key finding
<1–3 sentences: did dense buy quality above the ±7 pp n=198 noise floor? what did it cost?>

## Decision (per §2.12.1 / §2.12.8)
- [ ] CUT dense as cost play (Δ_pp ≤ +5 pp) — §2.8 vindicated; dense = documented ops-only option.
- [ ] OPERATIONAL-ONLY (+5..+10 pp) — re-run MMLU-Pro to confirm replication before documenting.
- [ ] REVISIT lock (>+10 pp AND unreachable by higher local --vote-n at ≤cost) — re-run NULL at --vote-n 9 first.

## Feeds back into dense-router-design.md
- §2.12.8 outcome row: <which region>.   Open #1 (carried) — H0-cost arm: <closed / status>.
```

### 4c.6 What this checklist explicitly does NOT do

- **No experiment beyond H0-cost.** H0-SE (§2.12.3, needs the §4b Thread-B PR merged first) and H0-router (§2.12.2, needs the unbuilt §2.12.4-A router) are separate runs. This checklist is the H0-cost arm only — the one that needs no new code.
- **No new bench code.** The two arms are pure existing-harness `cmd/bench` invocations. The *only* code touched anywhere in this checklist is the §4c.1 pricing literals in `cmd/phase1`, and even that is independent of the bench runs (bench doesn't read `cmd/phase1`'s map).
- **No MMLU-Pro run unless the result lands in `+5..+10 pp`.** The replication run is conditional, named in the decision arithmetic, not run by default.

---

## 4d. Thread A — PR-ready spec (kNN playbook-hint router) (ITERATION 8)

This is the §4b treatment applied to the router (build-plan **Phase 3**, §4a): the §2.10 interface *sketch* checked against the **real** current source and corrected to diff level, TDD-ordered, scoped to **one PR off `main`**. It is the largest of the three grafts (new package + additive store method + runner wiring + flags + two trace events) and the only one not yet spec'd to this fidelity. Per §4a it builds *after* Thread B merges; this spec is so a build session can open the PR without re-deriving signatures.

### 4d.0 Code-vs-doc mismatches found this iteration (the load-bearing corrections)

Read end-to-end this iteration: `pkg/exemplar/exemplar.go` (the real `Store` interface + `Retrieve` signature + the `var _ Store = (*FileStore)(nil)` compile assertion), `pkg/exemplar/bm25.go` (`buildBM25Index`/`score`/`tokenize`/`rankedHit` — the machinery to reuse), `pkg/runner/runner.go` (the exact `adapter.Evaluate` call site at L416 inside `resolve`, and `Config`), `pkg/session/envelope.go` (the `Evaluate` struct + `Payload` the hint reads/writes), `pkg/trace/trace.go` (the `Tracer.Event(ctx, level, name, ...attr)` emit shape), `pkg/benchmark/orchestrator/tree.go` (the `runner.Config{}` construction the bench `--tree` arm uses), and `cmd/bench/main.go` (the flag + props-fallback pattern). Four corrections to the iter-4 §2.10.3 sketch, all real:

1. **`RetrieveAcross` must NOT go on the `Store` interface (the sketch put it there — §2.10.3 comment "ONE additive method on the store interface").** The real `Store` is consumed by `pkg/curation`, `pkg/adapter/curated`, and is pinned by `var _ Store = (*FileStore)(nil)` (exemplar.go:388). Adding a method to the *interface* forces **every** implementer to grow it and would break any external `Store` impl — exactly the "ask before changing a consumed interface" caution. And a **free function over `Store.Retrieve` is impossible**: `Retrieve(ctx, action, prompt, k)` *requires the action up front* and only loads/scores that one action's file (exemplar.go:192–259) — it cannot rank across actions. **Correction:** add `RetrieveAcross` as a method on the **concrete `*FileStore` only**, plus a tiny optional **`AcrossRetriever` interface** (one method) that the router type-asserts. `Store` stays byte-for-byte unchanged; the compile assertion stays green; `curation`/`curated` are untouched. This is the central correction iteration 8 makes.

2. **`Neighbor.Exemplar.Action` is a `string`, but `Hint.Playbook` is a `session.Playbook`.** The sketch's `votes[session.Playbook(n.Exemplar.Action)]` conversion is correct but load-bearing: `Exemplar.Action` is a free `string` (exemplar.go:47) that the curation driver *happens* to populate with `session.Playbook` values ("process", "plan", …). The router must **filter to the five valid playbooks** and treat any unrecognized `Action` string as a non-vote (a corrupt/legacy store shouldn't be able to hint a playbook the adapter can't run). The sketch silently trusted every label; the real code needs a `validPlaybook` guard.

3. **The advisory hook slots inside `runner.resolve`, immediately before `r.cfg.Adapter.Evaluate(ctx, env)` (runner.go:416), NOT "before Evaluate" in the abstract.** That call site is reached on *every* AP. The hint is computed there, stamped onto a copy of `env`, and that copy is passed to `Evaluate`. Two real constraints the sketch glossed: (a) the runner already holds a governor lease at that point (L401) — the router read is pure-CPU BM25, no lease needed, so it must run *outside*/before any adapter call cost; fine, it's local. (b) `resolve` runs under `r.mu` concurrency when `MaxConcurrency>1`, but the router only *reads* the store (FileStore is internally mutex-safe) and writes only to the local `env` copy — no `r.mu` interaction. The trace events use `trace.Info(r.cfg.Tracer, ctx, …)` — the real sugar, not a bare `Event`.

4. **`session.Evaluate.HintPlaybook` (Option B) is additive-safe but the runner stamps it BEFORE Evaluate runs, and `Evaluate` is a pointer that's nil until the adapter fills it.** `env.Evaluate` is `*Evaluate` and is `nil` on entry to `resolve` (the adapter *produces* it). So Option B can't "set `env.Evaluate.HintPlaybook`" before the call — there's no struct yet. **Correction:** the v0 hook uses **Option A** (steering text appended to `env.Input.Content`, which exists), which is also the recommended-zero-schema path per §2.9.5. Option B's `HintPlaybook` field, if ever built, belongs on the envelope as a **top-level `HintPlaybook session.Playbook` `omitempty`** (a sibling of `NeedsVerification`), read by the adapter's Evaluate prompt — NOT inside the nil `*Evaluate`. This PR builds Option A only; the schema field is explicitly deferred (matches §2.9.5 "build B only if A is measured unreliable").

### 4d.1 `pkg/exemplar` — the additive `RetrieveAcross` on `*FileStore` (no `Store` change)

New file `pkg/exemplar/across.go`. Reuses `buildBM25Index`/`tokenize`/`rankedHit` from `bm25.go` verbatim — the *only* new logic is "iterate every action file, score the query against each corpus, merge the global top-k carrying each hit's `Action`." `Neighbor` carries the whole `Exemplar` (so `.Action`, `.Prompt`, `.Score` are all reachable, per the loop's "real fields" requirement) plus the BM25 `Sim`.

```go
// pkg/exemplar/across.go
package exemplar

import (
	"context"
	"os"
	"sort"
	"strings"
)

// Neighbor is one cross-action BM25 hit: the matched exemplar plus its
// similarity score. Carries the full Exemplar so callers reach .Action
// (the playbook label the router votes on), .Prompt, and .Score.
type Neighbor struct {
	Exemplar Exemplar // .Action, .Prompt, .Output, .Score all reachable
	Sim      float64  // BM25 score of the query against this exemplar's Prompt
}

// AcrossRetriever is the optional cross-action retrieval capability.
// FileStore implements it; the Store interface deliberately does NOT
// embed it (adding a method to Store would force every implementer to
// grow it and break pkg/curation / pkg/adapter/curated, which consume
// Store). Consumers that need it type-assert: store.(AcrossRetriever).
type AcrossRetriever interface {
	// RetrieveAcross ranks exemplars across ALL action files by BM25
	// similarity of the query to each exemplar's Prompt, returning the
	// global top-k with their action labels. Same k1/b/tokenizer as
	// Retrieve. Read-only: unlike Retrieve, it does NOT update
	// LastRetrievedAt (the router is a sidecar read, not a warm-path
	// surfacing — bumping LRU on every routed AP would distort GC).
	RetrieveAcross(ctx context.Context, prompt string, k int) ([]Neighbor, error)
}

// RetrieveAcross implements AcrossRetriever on *FileStore.
func (s *FileStore) RetrieveAcross(ctx context.Context, prompt string, k int) ([]Neighbor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if k <= 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		// Mirror GC's wrapping; a missing dir is an operator setup error.
		return nil, err
	}

	queryTokens := tokenize(prompt)
	var all []Neighbor
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		action := strings.TrimSuffix(entry.Name(), ".json")
		corpus, err := s.loadActionLocked(action)
		if err != nil {
			return nil, err
		}
		if len(corpus) == 0 {
			continue
		}
		// Reuse the exact bm25.go machinery. One index per action file
		// (same as Retrieve does for its single action).
		idx := buildBM25Index(corpus)
		for i := range corpus {
			sim := idx.score(queryTokens, i)
			if sim <= 0 {
				continue // no lexical overlap — not a neighbor
			}
			all = append(all, Neighbor{Exemplar: corpus[i], Sim: sim})
		}
	}
	if len(all) == 0 {
		return nil, nil
	}

	// Global top-k by Sim desc, then curation Score desc, then AddedAt
	// desc — the same tiebreaker order Retrieve uses.
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].Sim != all[b].Sim {
			return all[a].Sim > all[b].Sim
		}
		if all[a].Exemplar.Score != all[b].Exemplar.Score {
			return all[a].Exemplar.Score > all[b].Exemplar.Score
		}
		return all[a].Exemplar.AddedAt.After(all[b].Exemplar.AddedAt)
	})
	if k < len(all) {
		all = all[:k]
	}
	return all, nil
}

// Compile-time check: FileStore satisfies the optional capability.
var _ AcrossRetriever = (*FileStore)(nil)
```

Why this is genuinely additive, not a rewrite: it touches **no existing function**, the `Store` interface is unchanged (`var _ Store = (*FileStore)(nil)` still compiles), and it reuses `bm25.go` (`buildBM25Index`, `idx.score`, `tokenize`) **unmodified**. The one judgment call — *not* updating `LastRetrievedAt` — is documented and defensible (the router is a sidecar; bumping LRU on every routed AP would let routing traffic distort the GC's "least recently retrieved" signal, evicting genuinely-cold exemplars).

### 4d.2 `pkg/router` — the advisory `ExemplarRouter` (stdlib-only, new package)

New package `pkg/router` (note: the *old* `pkg/router`/`pkg/router/rule` were deleted under the call-tree refactor per `oscillitron/CLAUDE.md` "Deleted" — this is a clean-slate, differently-shaped package, no resurrection of routing edges). Two files: `router.go` (interface + types + `validPlaybook`) and `exemplar_router.go` (the kNN impl).

```go
// pkg/router/router.go
package router

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Hint is the advisory playbook suggestion. The zero Hint
// (empty Playbook) means "no opinion" — the router abstained
// (empty corpus, no neighbors, or below the confidence/margin
// floor) and Evaluate proceeds cold.
type Hint struct {
	Playbook   session.Playbook // top-voted playbook among the k neighbors
	Confidence float64          // winning_votes / total_votes ∈ [0,1]
	Margin     float64          // (top1 − top2) vote share — ambiguity guard
	K          int              // neighbors actually found (corpus may be < k)
}

// IsEmpty reports whether the router abstained (no playbook opinion).
func (h Hint) IsEmpty() bool { return h.Playbook == "" }

// Router produces an advisory playbook hint from an AP's input.
//
// ADVISORY ONLY. A consumer MAY seed Evaluate with the hint (as
// steering text or, later, a bias field); it MUST NOT skip Evaluate.
// Skipping Evaluate on a hint re-introduces a declared brain-function
// by the back door — the precise thing the uniform-node + every-AP-
// evaluates locks forbid (see §2.9.5 Option B caveat, §2.10.4
// guardrail). Abstention (empty Hint) is the safe default.
type Router interface {
	Hint(ctx context.Context, in session.Payload) (Hint, error)
}

// validPlaybooks is the set the adapter's Evaluate can actually run.
// A neighbor whose Action string isn't one of these is NOT counted —
// a corrupt or legacy store must not be able to hint a playbook the
// adapter can't execute. (Exemplar.Action is a free string; the
// curation driver populates it with session.Playbook values, but the
// router can't assume the store is clean.)
var validPlaybooks = map[session.Playbook]bool{
	session.PlaybookPlan:           true,
	session.PlaybookProcess:        true,
	session.PlaybookCritique:       true,
	session.PlaybookVerifyGrounded: true,
	session.PlaybookCompose:        true,
}

func validPlaybook(p session.Playbook) bool { return validPlaybooks[p] }
```

```go
// pkg/router/exemplar_router.go
package router

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// ExemplarRouter is the kNN-over-BM25 playbook-hint router. It reads
// the per-action exemplar store via the optional AcrossRetriever
// capability, votes the action labels of the k nearest neighbors, and
// returns the majority playbook — abstaining when the vote is weak or
// ambiguous. No model call, no weight update, no node type: a pure
// stateless read over the substrate (the kNN-router mechanism,
// arXiv:2505.12601).
type ExemplarRouter struct {
	// Store is the exemplar substrate. Must implement AcrossRetriever
	// (FileStore does); a Store that doesn't is treated as "always
	// abstain" rather than an error (the router is a best-effort
	// sidecar, never load-bearing).
	Store exemplar.Store
	// K neighbors to poll. <=0 defaults to 8.
	K int
	// MinConfidence: winning vote share below this → abstain.
	// 0 defaults to 0.5 (simple majority).
	MinConfidence float64
	// MinMargin: (top1−top2) share below this → abstain (tie guard).
	// 0 defaults to 0.15.
	MinMargin float64
}

func (r ExemplarRouter) k() int {
	if r.K <= 0 {
		return 8
	}
	return r.K
}
func (r ExemplarRouter) minConfidence() float64 {
	if r.MinConfidence <= 0 {
		return 0.5
	}
	return r.MinConfidence
}
func (r ExemplarRouter) minMargin() float64 {
	if r.MinMargin <= 0 {
		return 0.15
	}
	return r.MinMargin
}

// Hint implements Router.
func (r ExemplarRouter) Hint(ctx context.Context, in session.Payload) (Hint, error) {
	across, ok := r.Store.(exemplar.AcrossRetriever)
	if !ok {
		return Hint{}, nil // store can't do cross-action retrieval → abstain
	}
	nbrs, err := across.RetrieveAcross(ctx, in.Content, r.k())
	if err != nil {
		return Hint{}, err // surface real errors; caller treats as abstain
	}
	if len(nbrs) == 0 {
		return Hint{}, nil // cold/empty store → abstain (Evaluate runs cold)
	}

	// Majority vote over VALID neighbor playbook labels. Sim-weighting
	// is a deliberate non-goal in v0 (plain counts keep the kNN-router
	// baseline honest and the math inspectable); swap += 1 for += n.Sim
	// only if a measured case wants it.
	votes := map[session.Playbook]float64{}
	total := 0.0
	for _, n := range nbrs {
		pb := session.Playbook(n.Exemplar.Action)
		if !validPlaybook(pb) {
			continue
		}
		votes[pb]++
		total++
	}
	if total == 0 {
		return Hint{}, nil // all neighbors had invalid labels → abstain
	}

	top1, top2 := winnerRunnerUp(votes)
	h := Hint{
		Playbook:   top1.key,
		Confidence: top1.val / total,
		Margin:     (top1.val - top2.val) / total,
		K:          len(nbrs),
	}
	if h.Confidence < r.minConfidence() || h.Margin < r.minMargin() {
		return Hint{}, nil // weak or ambiguous → abstain (cheap-local default)
	}
	return h, nil
}

type kv struct {
	key session.Playbook
	val float64
}

// winnerRunnerUp returns the top-two vote entries (runner-up is the
// zero kv when only one playbook got votes). Deterministic tie-break
// by playbook name so equal-vote runs are reproducible.
func winnerRunnerUp(votes map[session.Playbook]float64) (top1, top2 kv) {
	for k, v := range votes {
		cur := kv{key: k, val: v}
		switch {
		case cur.val > top1.val || (cur.val == top1.val && cur.key < top1.key):
			top2 = top1
			top1 = cur
		case cur.val > top2.val || (cur.val == top2.val && cur.key < top2.key):
			top2 = cur
		}
	}
	return top1, top2
}

var _ Router = ExemplarRouter{}
```

### 4d.3 Runner wiring — the advisory pre-Evaluate hook (Option A steering, never a skip)

One additive `Config` field and ~12 lines inside `resolve`, immediately before the `adapter.Evaluate` call (runner.go:416). The hint is appended to a **copy** of `env.Input.Content` as a steering line; the adapter's Evaluate prompt reads the steered content and still emits its own `{playbook, rationale, confidence}` — it may agree or override. **No hard skip** (the §2.10.4 guardrail / §2.9.5 "hard-skip forbidden" verdict — a skip would re-declare a node type).

```go
// pkg/runner/runner.go — additive Config field (near VerifierPolicy):

	// Router optionally produces an ADVISORY playbook hint consulted
	// before Evaluate on every AP. The hint is seeded into Evaluate's
	// input as steering text — it NEVER skips or replaces Evaluate
	// (hard-skip is forbidden: it would re-declare a brain-function the
	// uniform-node + every-AP-evaluates locks forbid; see §2.10.4).
	// Nil = no routing (every AP evaluates cold, the current behavior).
	// Best-effort: a Router error is traced and ignored, never fails
	// the AP — routing is a prior, not a dependency.
	Router router.Router
```

```go
// pkg/runner/runner.go — inside resolve(), immediately BEFORE
//   evalEnv, err := r.cfg.Adapter.Evaluate(ctx, env)   (currently L416)

	// Advisory routing hint (Option A: steering text into the input).
	// Pure-CPU BM25 read; runs before the adapter call, holds no lease.
	if r.cfg.Router != nil {
		hint, herr := r.cfg.Router.Hint(ctx, env.Input)
		if herr != nil {
			// Best-effort: trace and proceed cold. A routing read must
			// never be able to fail a working AP.
			trace.Error(r.cfg.Tracer, ctx, "router.hint_error",
				slog.String("ap_id", string(env.ID)),
				slog.String("err", herr.Error()),
			)
		} else if !hint.IsEmpty() {
			env.Input.Content += "\n\n[playbook-hint: " + string(hint.Playbook) +
				" (confidence=" + strconv.FormatFloat(hint.Confidence, 'f', 2, 64) +
				", k=" + strconv.Itoa(hint.K) + ")]"
			r.hintForAP[env.ID] = hint.Playbook // remember for the override-detect
			trace.Info(r.cfg.Tracer, ctx, "router.hint_produced",
				slog.String("ap_id", string(env.ID)),
				slog.String("playbook", string(hint.Playbook)),
				slog.Float64("confidence", hint.Confidence),
				slog.Float64("margin", hint.Margin),
				slog.Int("k", hint.K),
			)
		}
	}

	// Evaluate step. (existing code, unchanged.)
	evalEnv, err := r.cfg.Adapter.Evaluate(ctx, env)
	// ... existing error handling ...
	r.lockedStateInc(&r.state.EvaluateCount)
	env = evalEnv

	// Disagreement detection — the H0-router PRIMARY metric. Compare
	// the hint (if any) against what Evaluate actually picked.
	if r.cfg.Router != nil && env.Evaluate != nil {
		if hinted, ok := r.takeHintForAP(env.ID); ok && hinted != env.Evaluate.Playbook {
			r.lockedStateInc(&r.state.RouterHintOverrides)
			trace.Info(r.cfg.Tracer, ctx, "router.evaluate_overrode_hint",
				slog.String("ap_id", string(env.ID)),
				slog.String("hint_playbook", string(hinted)),
				slog.String("evaluate_playbook", string(env.Evaluate.Playbook)),
			)
		}
	}
```

Supporting additions on the `runner` struct + `RunState`:

```go
// runner struct: a small map remembering the hint per in-flight AP so
// the override-detect (after Evaluate) can compare. Guarded by r.mu
// (resolve runs concurrently under MaxConcurrency>1). Initialized in
// Run() alongside subtree.
	hintForAP map[session.ID]session.Playbook

// runner methods (locked helpers, mu discipline in one place):
func (r *runner) takeHintForAP(id session.ID) (session.Playbook, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pb, ok := r.hintForAP[id]
	delete(r.hintForAP, id)
	return pb, ok
}
// (the write in resolve also takes r.mu — fold into a lockedSetHint helper
//  to match the existing lockedStateInc/lockedSetSubtree discipline.)

// RunState: the H0-router counters.
	// RouterHintsProduced counts non-abstain hints the router emitted.
	RouterHintsProduced int
	// RouterHintOverrides counts APs where Evaluate's pick differed from
	// a non-abstain hint — the disagreement numerator. The §2.12.2
	// disagreement rate is RouterHintOverrides / RouterHintsProduced.
	RouterHintOverrides int
```

(`RouterHintsProduced` is incremented in the `!hint.IsEmpty()` branch via `lockedStateInc`; omitted above for brevity. New imports in runner.go: `strconv`, and `"github.com/jrlmx2/oscillitron/pkg/router"`.)

Trace events match the real `trace.Info(r.cfg.Tracer, ctx, name, slog.X(...))` sugar exactly (trace.go:22). The disagreement rate the experiment reads is `RouterHintOverrides / RouterHintsProduced` — a within-run paired proportion, zero between-run variance (§2.12.2).

### 4d.4 `cmd/bench` flags — `--router*`, default OFF, wired only on the `--tree` arm

The router is only exercised where the runner walks Evaluate — i.e. the **Tree** orchestrator. Vote pins `PlaybookProcess` and Single makes one call; neither calls `Evaluate`, so the router is inert there by construction (§2.12.4-A note). The flags follow the exact `cmd/bench` pattern (declaration block + `flag.Visit` props-fallback, matching `--cope`/`--tree`):

```go
// cmd/bench/main.go — flag declarations (near treeEnable/treeMaxDepth):
	routerEnable   = flag.Bool("router", false, "Thread A (§4d): wrap the Tree orchestrator's runner with the kNN playbook-hint router (advisory — seeds Evaluate, never skips it). Off by default (preserves current cold-Evaluate behavior). Requires --tree and --router-store. Measurable only on the --tree arm (Vote/Single bypass Evaluate).")
	routerStore    = flag.String("router-store", "", "Thread A: exemplar.FileStore directory the router reads (cross-action BM25 kNN over the per-action exemplar files). Warm it first with a --curate-store-dir run. Empty + --router → router abstains on every AP (valid 'no substrate' null).")
	routerK        = flag.Int("router-k", 8, "Thread A: neighbors the router polls per AP (kNN k).")
	routerMinConf  = flag.Float64("router-min-confidence", 0.5, "Thread A: winning vote-share floor; below → abstain.")
	routerMinMargin = flag.Float64("router-min-margin", 0.15, "Thread A: (top1−top2) vote-share floor; below → abstain (tie guard).")
```

Construction (where the `Tree{}` orchestrator is built, after the governor + adapter are ready):

```go
	if *routerEnable {
		if !*treeEnable {
			log.Fatal("--router requires --tree (Vote/Single bypass Evaluate; the router is inert there)")
		}
		rt := router.ExemplarRouter{
			Store:         &exemplar.FileStore{Dir: *routerStore}, // empty Dir → abstains
			K:             *routerK,
			MinConfidence: *routerMinConf,
			MinMargin:     *routerMinMargin,
		}
		tree.Router = rt // new optional field on orchestrator.Tree, forwarded to runner.Config.Router
	}
```

This requires a one-line additive field on `orchestrator.Tree` (`Router router.Router`) forwarded into its `runner.Config{}` (tree.go:96) — the real construction site read this iteration. Default-zero `Tree.Router` (nil) preserves current behavior exactly. The props-fallback entries (`bench.router`, `bench.router.store`, …) mirror the existing `--cope`/`--tree` props keys in the `flag.Visit` block.

### 4d.5 TDD task order — ONE PR off `main` (branch, build, test, stop)

Per the locked PR workflow (branch from `origin/main`, one PR, no stacking, TDD). Each step names the test file + the assertion that must fail first, then pass.

| # | Test (write first) | Asserts | Then implement |
|---|---|---|---|
| 1 | `pkg/exemplar/across_test.go` `TestRetrieveAcross_RanksByBM25AcrossActions` | seed a `FileStore` with `process.json` + `plan.json`; a query lexically matching a `process` prompt returns that exemplar first with `Neighbor.Exemplar.Action=="process"` and `Sim>0` | `across.go` `Neighbor` + `RetrieveAcross` |
| 2 | `…_test.go` `TestRetrieveAcross_EmptyStoreAndKLE0` | empty dir → `nil, nil`; `k<=0` → `nil, nil`; missing dir → error | edge cases in `RetrieveAcross` |
| 3 | `…_test.go` `TestRetrieveAcross_DoesNotBumpLastRetrievedAt` | after `RetrieveAcross`, on-disk `LastRetrievedAt` is unchanged (vs. `Retrieve`, which bumps it) | confirm the read-only contract |
| 4 | `…_test.go` `TestStoreInterfaceUnchanged` | `var _ Store = (*FileStore)(nil)` still compiles AND `var _ AcrossRetriever = (*FileStore)(nil)` | the no-`Store`-change correction (§4d.0-1) |
| 5 | `pkg/router/exemplar_router_test.go` `TestHint_MajorityVoteAndConfidence` | a store where 6/8 neighbors are `process` → `Hint{Playbook:process, Confidence:0.75, Margin≈0.5}` | `router.go` types + `ExemplarRouter.Hint` |
| 6 | `…_test.go` `TestHint_AbstainsBelowMarginAndConfidence` | a 4-process/4-plan split → empty `Hint` (margin 0 < 0.15); below-`MinConfidence` → empty | the abstain guards |
| 7 | `…_test.go` `TestHint_IgnoresInvalidActionLabels` | a store with `Action:"garbage"` neighbors → those don't count; all-invalid → abstain | `validPlaybook` guard (§4d.0-2) |
| 8 | `…_test.go` `TestHint_NonAcrossStoreAbstains` | a `Store` that doesn't implement `AcrossRetriever` → empty `Hint`, no error | the type-assert fallback |
| 9 | `pkg/runner/runner_test.go` `TestRouter_SeedsEvaluateNeverSkips` | stub adapter records the `env.Input.Content` it saw; with a Router wired, the content carries `[playbook-hint: …]` AND `Evaluate` is still called (EvalCalls incremented) | the §4d.3 hook before `adapter.Evaluate` |
| 10 | `…_test.go` `TestRouter_OverrideCounterAndTrace` | stub Evaluate picks a playbook ≠ the hint → `RunState.RouterHintOverrides==1`, `RouterHintsProduced==1`; matching pick → overrides==0 | the disagreement detect + counters |
| 11 | `…_test.go` `TestRouter_NilRouterUnchanged` | nil `Config.Router` → `env.Input.Content` untouched, zero counters (current behavior preserved) | the nil-guard |
| 12 | `…_test.go` `TestRouter_HintErrorIsBestEffort` | a Router returning an error → AP still resolves, `router.hint_error` traced, Evaluate still runs | best-effort guard |
| 13 | `pkg/benchmark/orchestrator/tree_test.go` `TestTree_ForwardsRouterToRunner` | `Tree{Router: …}` → the runner sees `Config.Router` set (assert via a recording Router whose Hint is called) | the `Tree.Router` field + forward |
| 14 | (manual / `cmd/bench` smoke) | `--router` without `--tree` → fatal; with `--tree` + empty `--router-store` → runs, every hint abstains | the flag wiring + fatal guard |

Steps 1–4 (exemplar) and 5–8 (router) are independent and can interleave; 9–12 depend on 5; 13–14 depend on 9 + 13. Single PR, ~14 tests, all stdlib.

### 4d.6 What this PR does NOT do (scope fence)

- **No embeddings.** BM25-over-`bm25.go` only (the v0 path, §2.10.2). The frozen-embedding upgrade is deferred behind the §2.12.6 free-form measurement gate (open #3) — built *only if* BM25-kNN is measured too lexical on paraphrased prompts.
- **No model-tier / substrate routing.** The router emits a *playbook* hint, never a model tier (a tier router re-opens the §2.8 cost question the doc closed). Frontier stays at `delegate`/`verify_judge`.
- **No `session.Evaluate.HintPlaybook` schema field (Option B).** v0 ships Option A (steering text into `env.Input.Content`) only. Option B's top-level `HintPlaybook` envelope field (NOT inside the nil `*Evaluate` — §4d.0-4) is built only if Option A is measured unreliable (§2.9.5).
- **No hard skip of Evaluate, ever.** The hint is a prior seeded into Evaluate; Evaluate still runs and still owns the pick on every AP (the §2.10.4 / §2.9.5 guardrail).
- **No `Store` interface change.** `RetrieveAcross` is a `*FileStore` method + the optional `AcrossRetriever` interface; `curation`/`curated` are untouched (§4d.0-1).
- **No experiment RUN, no curation cycle, no Vote/Single wiring.** This PR builds the router *inert behind a default-off flag*; the H0-router run (§2.12.7 steps 6–8: warm the store, null vs. treatment, read the disagreement rate) and the keep-gate (the §2.12.6 multi-playbook workload) are separate, gated on this PR merging.

### 4d.7 Lock re-verification (against the real code, post-correction)

| Lock | Verdict on the as-spec'd PR |
|---|---|
| **uniform-node** | PRESERVED — no node type; the router is a stateless read; output is steering text appended to `env.Input.Content`, not structure. |
| **specialists-are-substrate** | PRESERVED — reads the per-action exemplar store (the substrate), votes its action labels. |
| **evaluate→execute, every AP** | PRESERVED — the hook runs *before* `adapter.Evaluate` and Evaluate still runs on every AP (tests 9, 11 enforce it). Hard-skip is structurally absent. |
| **no-weight-updates** | PRESERVED — BM25 trains nothing; `RetrieveAcross` reuses fixed IDF constants. |
| **stdlib-first** | PRESERVED — `across.go` reuses `bm25.go`; `pkg/router` imports only `context`, `pkg/exemplar`, `pkg/session`. Zero new deps. |
| **ask-before-changing-consumed-interface** | HONORED — `Store` is untouched; the correction (§4d.0-1) exists precisely to avoid widening a consumed interface. |
| **PR workflow** | HONORED — one PR off `main`, TDD-ordered (§4d.5), no stacking. |

**Net: zero hard lock tension (unchanged from §2.10.5), and the one interface-widening risk the iter-4 sketch carried is eliminated by the `*FileStore`-method-plus-optional-interface correction.**

---

## 5. Sources

Cascade / cascading:
- FrugalGPT — https://arxiv.org/abs/2305.05176
- AutoMix — https://arxiv.org/abs/2310.12963
- Cascade-Aware Training — https://arxiv.org/abs/2406.00060
- GATEKEEPER — https://arxiv.org/abs/2502.19335
- Speculative cascades (Google) — https://research.google/blog/speculative-cascades-a-hybrid-approach-for-smarter-faster-llm-inference/
- Decision-theoretic cascade characterization — https://arxiv.org/html/2605.06350
- Unified routing & cascading — https://arxiv.org/abs/2410.10347

Learned cost-quality routers:
- RouteLLM — https://arxiv.org/pdf/2406.18665 ; blog https://www.lmsys.org/blog/2024-07-01-routellm/ ; code https://github.com/lm-sys/RouteLLM
- Hybrid LLM (Ding et al., ICLR 2024) — https://arxiv.org/pdf/2404.14618
- RouterDC (NeurIPS 2024) — https://arxiv.org/pdf/2409.19886
- BEST-Route (Microsoft) — https://arxiv.org/abs/2506.22716
- kNN beats learned routers — https://arxiv.org/pdf/2505.12601
- Dynamic routing & cascading survey — https://arxiv.org/pdf/2603.04445

Confidence / difficulty / calibration:
- Confidence tokens (Self-REF) — https://arxiv.org/abs/2410.13284
- Uncertainty-based on-device routing — https://arxiv.org/pdf/2502.04428
- Semantic entropy — https://arxiv.org/html/2411.02381v1
- LLM-perceived difficulty via hidden reps — https://arxiv.org/pdf/2509.12886
- Difficulty-Aware Agentic Orchestration (DAAO) — https://arxiv.org/html/2509.11079v3
- MUSE multi-LLM uncertainty — https://arxiv.org/html/2507.07236

Model-as-tool / orchestrator:
- HuggingGPT / JARVIS — https://arxiv.org/pdf/2303.17580
- Resource-efficient multimodal routing — https://arxiv.org/html/2511.06441
- LangGraph supervisor vs swarm — https://dev.to/focused_dot_io/multi-agent-orchestration-in-langgraph-supervisor-vs-swarm-tradeoffs-and-architecture-1b7e
- MAS-Orchestra — https://arxiv.org/pdf/2601.14652
- To Call or Not to Call (tool-calling calibration) — https://arxiv.org/html/2605.00737v1
- Multi-turn multi-agent vs single LLM — https://arxiv.org/pdf/2509.23537

Models-as-tools calling protocol (iteration 3 — primary sources):
- Anthropic "Building Effective Agents" (orchestrator-workers: central LLM dynamically decomposes, delegates to workers, synthesizes; subtasks not pre-defined) — https://www.anthropic.com/research/building-effective-agents
- Anthropic "How we built our multi-agent research system" (lead agent spawns subagents with {objective, output_format, tool/source guidance, boundaries}; isolated context windows; results bubble back; lead persists plan to Memory across calls; beat single-agent Opus 4 by 90.2% internal) — https://www.anthropic.com/engineering/multi-agent-research-system
- Anthropic cookbook `orchestrator_workers.ipynb` (structured-output decompose into {type, description} per subtask; worker invoked per subtask; results collected into a list + synthesized — NOT function-calling tools) — https://github.com/anthropics/anthropic-cookbook/blob/main/patterns/agents/orchestrator_workers.ipynb
- Anthropic tool-use schema (`{name, description, input_schema}` JSON Schema; `tool_use`/`tool_result` content blocks correlated by `tool_use_id`; `tool_choice` to force a tool) — https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/implement-tool-use ; advanced tool use — https://www.anthropic.com/engineering/advanced-tool-use
- AOrchestra (automating sub-agent creation for orchestration) — https://arxiv.org/pdf/2602.03786

Semantic entropy + kNN-routing (iteration 4 — primary sources):
- Kuhn, Sun, Gal "Semantic Uncertainty: Linguistic Invariances for Uncertainty Estimation in NLG" (ICLR 2023; sample N, cluster by bidirectional NLI entailment, entropy over meaning-clusters) — https://arxiv.org/abs/2302.09664
- Farquhar, Kossen, Kuhn, Gal "Detecting hallucinations in large language models using semantic entropy" (Nature 2024; meaning-space entropy; discrete/black-box frequency-based variant needs no logprobs) — https://www.nature.com/articles/s41586-024-07421-0 ; OATML blog — https://oatml.cs.ox.ac.uk/blog/2024/06/19/detecting_hallucinations_2024.html
- Semantic Entropy Probes (cheap single-pass approximation; arXiv:2406.15927) — https://arxiv.org/abs/2406.15927
- Discrete semantic entropy in a black-box VLM setting (frequency over NLI clusters, no internals) — https://arxiv.org/pdf/2510.09256
- kNN beats learned routers (non-parametric neighbor voting matches/beats trained parametric routers; majority-vote over k nearest by embedding/lexical similarity) — https://arxiv.org/pdf/2505.12601
- kNN-LM "Generalization through Memorization" (datastore of context→target, L2/cosine retrieval, no further training — the substrate-swap-not-retrain shape) — https://arxiv.org/pdf/1911.00172

Calibration-metric methodology (iteration 5 — for the H0-SE scoring in §2.12.3):
- Guo, Pleiss, Sun, Weinberger "On Calibration of Modern Neural Networks" (ICML 2017; defines ECE as the binned weighted gap between accuracy and confidence; reliability diagrams; temperature scaling — the canonical modern ECE reference) — https://arxiv.org/abs/1706.04599
- Naeini, Cooper, Hauskrecht "Obtaining Well Calibrated Probabilities Using Bayesian Binning" (AAAI 2015; the binned-ECE estimator) — https://ojs.aaai.org/index.php/AAAI/article/view/9602
- Brier "Verification of Forecasts Expressed in Terms of Probability" (Monthly Weather Review 1950; the Brier score = mean-squared probability error) — https://journals.ametsoc.org/view/journals/mwre/78/1/1520-0493_1950_078_0001_vofeit_2_0_co_2.xml
- Murphy "A New Vector Partition of the Probability Score" (1973; Brier decomposition into reliability + resolution + uncertainty — why a reliability curve is the right diagnostic) — https://journals.ametsoc.org/view/journals/apme/12/4/1520-0450_1973_012_0595_anvpot_2_0_co_2.xml
- Nixon et al. "Measuring Calibration in Deep Learning" (CVPR-W 2019; adaptive binning / pitfalls of fixed-bin ECE — motivates the 10-bin-grid cross-check alongside the 3 DefaultBands) — https://arxiv.org/abs/1904.01685

MoE routing:
- Mixtral of Experts — https://arxiv.org/pdf/2401.04088
- MoE explained (HF) — https://huggingface.co/blog/moe
- Switch Transformer — https://nn.labml.ai/transformers/switch/index.html
- CITER token-level routing — https://arxiv.org/pdf/2502.01976

Semantic / embedding routing:
- Aurelio semantic-router — https://docs.aurelio.ai/semantic-router/user-guide/concepts/overview
- vLLM semantic router (Red Hat) — https://developers.redhat.com/articles/2025/09/11/vllm-semantic-router-improving-efficiency-ai-reasoning

LLM-as-router vs classifier / cost-of-routing:
- Survey on routing strategies — https://arxiv.org/pdf/2502.00409
- Morph "What is an LLM router?" — https://www.morphllm.com/llm-router
- Ask a strong judge when reward model uncertain — https://arxiv.org/pdf/2510.20369
- Routing cost math (overpay 40–85%) — https://www.mindstudio.ai/blog/best-ai-model-routers-multi-provider-llm-cost-011e6
- ModernBERT fine-tune — https://www.philschmid.de/fine-tune-modern-bert-in-2025
- When efficiency backfires (cascade adversarial) — https://arxiv.org/html/2605.17288v1

Pricing (iteration 2, verified May/June 2026):
- Claude API pricing (Opus 4.8 $5/$25, Sonnet 4.6 $3/$15, Haiku 4.5 $1/$5; caching + batch levers) — https://www.metacto.com/blogs/anthropic-api-pricing-a-full-breakdown-of-costs-and-integration
- Claude API pricing cross-check — https://www.cloudzero.com/blog/claude-api-pricing/
- GPT-4o-mini $0.15/$0.60; GPT-4.1-mini $0.40/$1.60; GPT-4o $2.50/$10 — https://devtk.ai/en/models/gpt-4o-mini/ ; https://pecollective.com/tools/gpt-4o-pricing/
- Llama-3.3-70B hosted ~$0.59–0.90/Mtok (Together/Fireworks/Groq) — https://tokenmix.ai/blog/fireworks-ai-review ; https://www.eesel.ai/blog/together-ai-pricing
- Qwen-2.5-72B ~$0.35/$0.40 (DeepInfra/Alibaba) — https://costbench.com/software/llm-api-providers/deepinfra/ ; https://costbench.com/software/llm-api-providers/qwen-api/

---

## 6. Iteration log

**Iteration 1 (2026-06-14) — breadth survey.** Created this doc. Established the framing (dense-orchestrator-calls-cheap-tools) and the central cost tension against Oscillitron's cheap-local-first + frontier-only-at-delegate locks. Surveyed seven routing families with load-bearing mechanism detail: (2.1) LLM cascades + escalation scorers (FrugalGPT/AutoMix/CAT/GATEKEEPER/speculative); (2.2) learned cost-quality routers (RouteLLM's four variants, Hybrid LLM, kNN-beats-learned); (2.3) confidence/difficulty signals + calibration (key finding: no free well-calibrated confidence; semantic entropy is best but costs N samples); (2.4) model-as-tool dispatch (HuggingGPT, LangGraph supervisor — the closest analog to the new direction); (2.5) MoE routing (vocabulary transfers, structure doesn't); (2.6) semantic/embedding routing (cheapest graft); (2.7) LLM-as-router vs classifier (the cost-of-routing tradeoff that is the project's core risk). Mapped each onto the locked architecture with tensions flagged in §3. Net finding: the routing-cost research **validates** the existing cheap-Evaluate locks and puts the burden of proof on the dense inversion.

**Most important open tension for iteration 2:** resolve open question #1 — *is the "dense" orchestrator the frontier model or a mid-tier model?* — and build the head-on **blended-cost comparison** (open question #7) of dense-orchestrator-per-request vs the existing cheap-Evaluate + rare-frontier path, using `pkg/cost` on the Phase-1/GPQA workloads. That measurement determines whether the dense framing is even viable before any lock is reconsidered. Secondary thread: the most lock-compatible graft (kNN/semantic router over the existing `pkg/exemplar` store, #2/#6) and the near-free semantic-entropy-over-Vote escalation signal (#3) deserve concrete sub-designs.

**Iteration 2 (2026-06-14) — the cost falsification test.** Built §2.8: the head-on blended-cost comparison with verified May/June-2026 pricing (Opus 4.8 $5/$25, Sonnet 4.6 $3/$15, Haiku 4.5 $1/$5, GPT-4o-mini $0.15/$0.60, hosted-open 70B-class ~$0.35–0.90, local ~$0). Modeled both project workloads with explicit token assumptions drawn from the actual code: Phase-1 email (8 calls: plan+3 drafts+2 synth+critique+revise, ~850 tok/call) and GPQA (Vote-5 = 7,500 tok; Tree ≈ 9,000 tok), using the project's own 70/30 in/out blend from `cmd/phase1` `report()`. **Decision reached: "dense" must mean a mid-tier model, never the frontier.** Frontier-orchestrator-per-request costs **5–6× a single frontier call** ($0.0444 vs $0.0071 on email; $0.0585 vs $0.0117 on GPQA Vote-5) — it loses not just to the locked ~$0 local path but to the frontier single-call it was meant to replace, because orchestration multiplies token volume ~7× and frontier rates pay that multiplier in full. Mid-tier dense ($0.002–0.015/request) is viable and absorbs the orchestration overhead, but is just the *existing* Vote/Tree orchestrators on a hosted substrate — a deployment choice the codebase already exposes (`--orchestrator-substrate`), and still ~5–6× more expensive than the locked local path. **Net: dense loses on cost under every regime; ordering is `local-cheap (locked) < hosted-mid < frontier-single < frontier-orchestrator`. Its only wins are (a) the quality-bound, cost-insensitive niche (§2.8.4) and (b) operational simplicity for operators who can't run local GPUs — and (b) must still ride a mid-tier substrate. The arithmetic vindicates the existing locks.** Retired open questions #1 and #7. Also flagged a correctness bug: `cmd/phase1` `defaultPricing` has stale Haiku ($0.80/$4) and Opus ($15/$75) rates (new #4).

**Iteration 3 (2026-06-14) — the models-as-tools calling protocol.** Built §2.9: a field-by-field specification of how a (mid-tier) orchestrator surfaces focused specialists as callable tools, mapped onto the locked AP envelope. Did real primary-source research: Anthropic's tool-use schema (`{name, description, input_schema}`; `tool_use`/`tool_result` correlated by `tool_use_id`; `tool_choice`-force), Anthropic's orchestrator-workers cookbook (structured-output decompose into `{type, description}` per subtask, workers invoked per subtask, results collected + synthesized — *not* function-calling tools), and Anthropic's production research system (subagents spawned dynamically with `{objective, output_format, guidance, boundaries}`, isolated contexts, results bubble back, **lead agent persists plan to Memory across many subagent calls**). Read `pkg/session/envelope.go` to ground the mapping in real fields. **Decisions reached:** (1) **REUSE `plan`+`process`+`compose` — no new playbook, no new node type.** A tool-call = `plan`→`emit_subtree` sub-AP; a tool-result = `process`/`compose` `return_result` on the scope channel; child `Envelope.ID` = `tool_use_id`. `delegate`/`dispatch` stays cut — it would be redundant with `plan` or break the locked three-Category model (no fourth Category a tool-call/result needs). (2) **Uniform-node verdict: PRESERVED for the stateless one-shot-plan shape** (the orchestrator is the root AP running `plan`, not a privileged node — proven via the worked Phase-1 email-decline example end to end). (3) **TENSIONED and DEFERRED for the stateful re-planning loop**: a persistent context-holding orchestrator (the native-function-calling / Anthropic-Research shape) violates "APs are siloed to one brain-function / the call tree dissolves on return" and re-introduces the mid-tree result→parent feedback edge the locked dataflow deliberately cut — argued both sides (it's what the strongest real systems do and is more powerful for sequential-dependent decomposition; but it's a new structural node-kind and re-prices the orchestrator) and routed it to the Hermes substrate under wrap-not-fork rather than the orchestration layer. (4) **Tools-execute-after-verified-output lock is HONORED** — that lock gates side-effecting *connectors*, not sub-*inference*; the whole call tree is already mid-tree inference, so models-as-tools adds no side effect, with an explicit guardrail that connector-tools must never enter the tree (a model-as-tool's `tool_result` is always a `Payload` proposal, never a connector receipt). (5) Specialist *pinning* (the `tool_choice:force` analog), if ever needed: **Option A** (encode the specialist hint in `SubAPSeed.Input`/`OutputSchema`, child's Evaluate still picks — zero schema change) first; **Option B** (optional additive `SubAPSeed.PlaybookHint` *bias*, never a hard Evaluate-skip) only if Option A is measured too unreliable. Retired the iteration-2 open question #1.

**Iteration 4 (2026-06-14) — the two lock-compatible interface sketches.** Built §2.10 and §2.11, grounded in the actual source of `pkg/exemplar` (BM25 `Retrieve`/`Store`/`FileStore`), `pkg/benchmark/orchestrator.Vote` (the `votes map[string]int` histogram + `meanConfidence`), `pkg/cope` (`RuleTable.Decide(conf, stakes)` → 4 Actions) and `pkg/benchmark/orchestrator.Coping` (which feeds `inner.Confidence` into `rules.Explain`). Did real research on semantic entropy (Kuhn ICLR 2023 / Farquhar Nature 2024 — meaning-space entropy via NLI-clustered samples; the **discrete black-box variant** that needs no logprobs is the exact fit for Vote's text-only N samples) and kNN-routing (arXiv:2505.12601 — non-parametric neighbor voting beats learned routers; kNN-LM substrate-swap-not-retrain shape). **Decisions reached:**

(1) **Thread A — kNN playbook-hint router (§2.10).** It routes a **playbook hint for Evaluate**, NOT a model/substrate tier (a tier router re-opens the §2.8 cost question; frontier stays at `delegate`). Interface: one additive `exemplar.Store.RetrieveAcross(ctx, prompt, k) []Neighbor` (cross-action BM25 kNN, reusing `bm25.go`'s index machinery) + a new stdlib-only `pkg/router` with `Router.Hint(ctx, session.Payload) (Hint, error)` and `ExemplarRouter{Store, K, MinConfidence, MinMargin}` doing majority-vote-over-neighbor-actions with a margin abstain. Slots as an **advisory** `runner.Config.Router` consulted *before* Evaluate, **seeding** Evaluate (a `[playbook-hint:…]` steering line or an additive `env.Evaluate.HintPlaybook` *bias*) — **never skipping** Evaluate (hard-skip forbidden as a back-door node-type, per §2.9.5 Option B caveat). Realizes §2.9.5 Option A automatically. **Lock verdict: uniform-node / specialists-are-substrate / evaluate→execute / no-weight-updates all PRESERVED; stdlib-first PRESERVED for v0 (BM25-kNN) and TENSIONED only if the embedding upgrade is adopted.** The embedding-model question is settled: a *frozen* embedding model is **permitted retrieval infrastructure, not a trained specialization weight** — no-weight-updates governs specialization, and the lock explicitly lists retrieval indexes as a permitted substrate. So embeddings don't break no-weight-updates; they only trip stdlib-first, and are deferred behind a measurement gate.

(2) **Thread B — semantic entropy over Vote (§2.11).** Discrete semantic entropy `H = −Σ(n_c/N)·ln(n_c/N)` over meaning-clusters of Vote's N answers; `conf = 1 − H/ln(N)` (normalized so it's comparable across stakes-scaled N). Interface: a stdlib `pkg/semanticentropy` with a `Clusterer` interface, an `ExactMatch` v0 clusterer (= the histogram Vote *already* builds, free, and genuinely meaning-clustering for MCQ/extracted-canonical), `Entropy(sizes)` and `Confidence(sizes, n)`; a v1 `pkg/semanticentropy/nli.EntailmentClusterer` (bidirectional-entailment / embedding-cosine) for free-form, dependency-isolated like `pkg/trace/otel`. Wires in by setting `Answer.Confidence = semanticentropy.Confidence(...)` in `Vote.Answer`, so the **existing** `Coping → cope.RuleTable.Decide` path runs unchanged and receives a better-calibrated scalar. **No new cope Action invented** (maps onto Ship/ShipWithCaveat/Escalate exactly); high-entropy + high-stakes = `Escalate`. **Lock verdict: fully lock-compatible — zero tension.** It adds one better-calibrated scalar to a decision the system already makes, and sharpens the ~5% delegate rate that dominates the locked path's residual cost (escalate the truly-uncertain, not the falsely-confident). Confirmed the loop's hypothesis that Thread B is purely additive. Retired both iteration-3 handoff open questions; added sources (Kuhn 2302.09664, Farquhar Nature 2024, SE-probes 2406.15927, discrete-SE 2510.09256, kNN-routing 2505.12601, kNN-LM 1911.00172).

**Highest-leverage thing iteration 5 should tackle (the single handoff):** the **falsification experiment** (new open question #1) — both §2.10 and §2.11 are now buildable, so the loop's overdue null-hypothesis test is to *run them against the existing harness with the existing architecture as the null*. Design it concretely: **(a) router** — `cmd/bench` GPQA with vs. without `runner.Config.Router`, success metric = the `router.evaluate_overrode_hint` disagreement rate plus any pass-rate/cost delta; the null is "the hint never changes what Evaluate would already pick → the router earns nothing and is cut." **(b) semantic entropy** — `cmd/bench --cope` with `Answer.Confidence` = SE-confidence vs. the self-reported mean, scored head-to-head by `pkg/benchmark/calibration` (does SE-confidence's pass-rate-per-band slope up steeper? does it escalate strictly fewer false-confident high-stakes cases at equal recall?). Must specify the run commands, the success/kill thresholds (mirroring the §2.5 kill-or-proceed gate's discipline), and how the result feeds open questions #3 (calibration validation) and #4 (dependency posture — embeddings only earn their keep if the BM25/exact-match v0 is *measured* too lexical). This converts two designs into measured keep/cut decisions and is the natural close of the research arc: §2.8 falsified the cost story; iteration 5 falsifies the two surviving grafts' *quality* story.

**Where iteration 5+ should continue:** after the falsification experiment, the residue is the dependency-posture decision (#4, now gated on the experiment's measurement) and the deferred §2.9.4 stateful re-planning loop (a Hermes-substrate concern under wrap-not-fork, out of orchestration-layer scope). The dense-orchestrator inversion itself is fully settled.

**Iteration 5 (2026-06-14) — the falsification experiment + the build plan (the research arc closes).** Built §2.12 (decision procedure) and §4a (build order), grounded in the *actual* harness surface read end-to-end: `cmd/bench/main.go` (real flags — `--vote-n`, `--orchestrator-substrate`, `--cope`, `--stakes`, `--tree`, `--report-out`, `--stream-out`, `--price`/`--frontier-price`, `--curate-store-dir`/`--use-store`), `pkg/benchmark/orchestrator.Vote` (confirmed the `votes map[string]int` histogram + `meanConfidence` self-report — SE's input and its comparator both already exist on one run), `pkg/benchmark.Answer`/`AggregateStats`/`report.go` (`Confidence`, `CopeAction`, `PassRate`, `TotalActualUSD`, `SavingsRatio` all in the `--report-out` JSON, so the calibration scorer runs offline with no re-run), `pkg/cope.RuleTable.Decide(conf, stakes)` and `pkg/benchmark/calibration` (the existing band machinery the head-to-head extends). **Decisions reached:**

(1) **Three null hypotheses, each the doc's own conclusion turned into a target.** **H0-cost** — dense mid-tier orchestration buys no quality over local Vote-5; arms are two `cmd/bench` GPQA runs differing only in `--orchestrator-substrate` (ollama-qwen vs anthropic-Haiku), full 198-case set, read `PassRate`/`SavingsRatio`, **cut at `≤ +5 pp`** (the n≈198 noise floor), pure existing harness. **H0-router** — the kNN hint is inert or no better than Evaluate; the *primary metric is a within-run paired disagreement rate* via a new `router.evaluate_overrode_hint` counter, **cut at `< 5%` disagreement** (expected on MCQ — single-action `process`-dominated), keep only at `≥ 15%` disagreement AND net right-flips on the disagreement subset. **H0-SE** — semantic entropy no better-calibrated than self-report; *both confidence columns computed on the same Vote run* (SE from the histogram, self-report from `meanConfidence`), scored offline for ECE/Brier/reliability-slope, **build at `ECE-delta ≥ 0.03`** with no extra false-confident high-stakes ships, **cut at `< 0.01`**.

(2) **Statistical power addressed head-on (§2.12.0):** the ±5–10 pp variance at 20 cases forces (a) full-GPQA `--limit 0` for any pass-rate decision, and (b) a deliberate design where the *primary* signals are within-run *paired* comparisons (router disagreement rate; SE-vs-self ECE/Brier on identical cases) that sidestep between-run variance entirely, with the noisier between-arm pass-rate/cost deltas as confirmation only. Determinism is pinned (SHA-256 case placement + seeded sibling dispatch).

(3) **Minimal instrumentation specified to the file (§2.12.4)**, all additive, none touching a lock: SE is `pkg/semanticentropy` + additive `Answer.SEConfidence` + ~3 lines in `Vote` + a `--cope-confidence-source` switch + an offline `calibration.ECE`/`Brier` scorer; the router is `exemplar.RetrieveAcross` + `pkg/router` + `runner.Config.Router` + two trace events + `--router*` flags (and is only measurable on the `--tree` arm, since Vote bypasses Evaluate). The stale-pricing fix is folded in as step-0.

(4) **Build order (§4a):** Thread B (SE) first — cheapest, zero lock tension, tightest-CI test; then H0-cost — no new code, two bench invocations; then Thread A (router) behind its inertness gate (build BM25 v0 only, defer embeddings); then the free-form Phase-1 keep-gate (the *only* place a router-keep or SE-v1-clusterer / open-#4 dependency decision can be fairly made). Dense-as-packaging deferred until an operator asks; the stateful re-planning loop stays a Hermes concern.

(5) **Calibration methodology cited** (Guo ICML 2017 for ECE + reliability diagrams; Brier 1950; Murphy 1973 decomposition; Naeini AAAI 2015 binned-ECE; Nixon CVPR-W 2019 adaptive binning). Retired the iteration-4 handoff open question #1; folded calibration-validation (#2) into H0-SE and dependency-posture (#3) into the free-form arm.

**Highest-leverage thing iteration 6 should tackle (the single handoff):** **write the PR-ready instrumentation spec for Thread B (semantic entropy) — the first build-plan phase — as concrete, diff-level Go.** The doc now has the *experiment design* but the build plan's Phase 1 is the natural next concrete artifact: (a) the full `pkg/semanticentropy` package source (`Clusterer`/`ExactMatch`/`Entropy`/`Confidence` with the `H = −Σ(n_c/N)ln(n_c/N)`, `conf = 1 − H/ln(N)`, N<2→0 edge cases tested), (b) the exact `Vote.Answer` diff that sets `Answer.SEConfidence` from the existing `votes` histogram + the `vote.semantic_entropy` trace event, (c) the additive `Answer.SEConfidence` field + `answerJSON` `omitempty` change, (d) the `calibration.ECE`/`calibration.Brier`/reliability-slope functions next to `Compute` with the offline scorer that reads `--report-out`, and (e) the `--cope-confidence-source self|se|blend` flag + the one field on `Coping`. Spec it as TDD-ordered tasks (test first, per the repo's discipline) so it drops straight into a PR off `main` (one PR, no stacking, per the locked PR workflow). That converts the highest-EV graft from "designed + measured-on-paper" into "buildable this session," and running it (§2.12.7 steps 3–5) produces the first real keep/cut datum the whole five-iteration arc was built to generate. *Secondary, if Thread B is already specced: the same diff-level treatment for the router (Phase 3), whose larger surface (`RetrieveAcross` + `pkg/router` + runner wiring) benefits more from a precise spec but earns its build slot only after SE's result is in.*

**Iteration 6 (2026-06-14) — the PR-ready Thread B spec (design → buildable diff).** Built §4b, grounded in the *actual* current source read end-to-end this iteration: `pkg/benchmark/orchestrator/vote.go` (the real `votes map[string]int` histogram, `successes`, `confidenceSum/Count`, `bestKey`, both return paths), `pkg/benchmark/benchmark.go` `Answer` struct (no JSON tags — serialized via a mirror), `pkg/benchmark/report.go` (`answerJSON` + the `answerJSON(or.Answer)` *direct struct conversion* at line 93), `pkg/benchmark/calibration/calibration.go` (`Compute`/`Band`/`DefaultBands`/`pickBand`/`Row.PassRate`/`FormatTable`), `pkg/cope/cope.go` (`RuleTable.Decide`/`Explain`, 4 Actions), `pkg/benchmark/orchestrator/coping.go` (`rules.Explain(inner.Confidence, …)` at line 98), and `cmd/bench/main.go` (the `--cope`/`copeHigh`/`copeLow` flag-declaration + props-fallback + `Coping{}` construction pattern, plus the `printReport` calibration-table print site). **Delivered:** (1) the full `pkg/semanticentropy` package as compilable-shaped Go — `Clusterer` interface, `ExactMatch` v0 clusterer (drops blanks, mirrors Vote's tally rule), `Entropy(sizes)` in nats with the len-0/len-1/non-positive edge cases, `Confidence(sizes, n)` with `conf = 1 − H/ln(N)`, N<2→0 and single-cluster→1.0, plus a defensive "trust Σ sizes over the passed n" guard; the v1 `nli.EntailmentClusterer` shape fixed but not built. (2) The `Answer.SEConfidence` diff **and** its forced lockstep `answerJSON` twin. (3) The ~10-line `Vote.Answer` population (`votesTotal`/`sizes`/`seConf` + both returns + `vote.semantic_entropy` event). (4) The offline scorer `calibration.Score`/`ECE`/`Brier`/reliability-slope in a new `score.go`, with cited methodology (Guo ICML 2017 ECE; Brier 1950; Murphy 1973; Naeini AAAI 2015; Nixon CVPR-W 2019 for the 10-bin cross-check) and a `confOf` field-selector + `SelfReported`/`SemanticEntropy` selectors so both columns score head-to-head from one report; invoked as a `printReport` line after the existing calibration table (justified over a `cmd/calib` tool: same in-process `Report`, print site already exists). (5) The `--cope-confidence-source self|semantic-entropy` flag (default `self`) wired as a `Coping.ConfidenceSource` field selecting `inner.Confidence` vs `inner.SEConfidence` before `rules.Explain`. (6) An 8-step TDD task table (test file + assertion named per step, package-before-consumer order) tied to the PR-workflow lock (branch from `origin/main`, one PR, stop-and-wait, no stacking), and an explicit NOT-in-scope list (no NLI/embedding clusterer, no embedding dep, no blend, no Thread A, no experiment RUN, no `Single`/`Tree` SE, no `pkg/cope` change).

**Three code-vs-doc mismatches found and CORRECTED (§4b.0):** (1) **`answerJSON` is a direct struct conversion** (`Answer: answerJSON(or.Answer)`, report.go:93) — iter-5 treated `Answer.SEConfidence` and the `answerJSON` change as independent, but the conversion is only legal if both structs have identical field layout, so they **must** change in lockstep at the same ordinal position. (2) **The SE normalization base is the vote total Σ n_c, not `successes`** — iter-5 wrote `Confidence(sizesFromVotes, successes)`, but `successes` counts attempts that produced text *including empty-extraction ones the histogram excludes* (vote.go:191–207), so `successes ≥ Σ votes`; using it under-counts agreement and inflates entropy on weak substrates (35 such firings on phi4-mini's run). Corrected to a `votesTotal` accumulator + a defensive guard in `Confidence`. (3) **The confidence-source switch belongs on `Coping`, not `pkg/cope`** — iter-5 said "`pkg/cope` / `orchestrator.Coping`," but `RuleTable.Decide` is a pure scalar lookup that must stay source-agnostic; `Coping.Answer` reads `inner.Confidence` (coping.go:98), so the column selection is one field + one switch there and **zero `pkg/cope` change**.

**Iteration 7 (2026-06-14) — the H0-cost PR-ready run checklist (the cheapest hypothesis becomes copy-paste-runnable).** Built §4c, grounded in the *actual* current source read end-to-end this iteration: `cmd/phase1/main.go` (`defaultPricing` lines 58–62 + `report()`'s 70/30-blend cost computation + the §2.5 proceed/kill thresholds), `cmd/bench/main.go` (the real flags — `--benchmark`/`--cases`/`--limit`/`--vote-n`/`--orchestrator-substrate`/`--orchestrator-model`/`--frontier-substrate`/`--frontier-model`/`--price`/`--frontier-price`/`--report-out`/`--stream-out`/`--tree` — plus the `smallModelSubstrings` allowlist + `resolveSubstrate` auto-route confirming `qwen2.5:7b` routes to ollama and `--orchestrator-substrate` is the dense/local axis, and the `orchestrator-vote-<N>-<model>` `NameStr` the `--price` key must match), `pkg/benchmark/report.go` (the real `aggregateStatsJSON` snake_case tags: `pass_rate`/`avg_score`/`total_tokens`/`total_actual_usd`/`total_frontier_usd`/`savings_ratio`), `pkg/adapter/anthropic` (default orchestrator = `claude-haiku-4-5-20251001`, default frontier = `claude-sonnet-4-6` — establishing the pricing-fix blast radius), and `cmd/bench/cases/README.md` (the GPQA download recipe). **Delivered:** (a) the literal two-line pricing fix as a before/after diff, scoped as a **standalone v1.0.0 patch PR** (versioning lock) independent of the bench runs; (b) the two `cmd/bench` arms (NULL = local Vote-5 ollama-qwen2.5:7b; DENSE = same on anthropic-Haiku) with every flag filled, `--report-out`/`--stream-out` paths under `scratch/exp/`, the `--price`-key-must-match-`NameStr` caution, and an optional `--tree` arm 3; (c) the exact snake_case JSON fields table + a `jq` puller + fill-in-the-blanks decision arithmetic applying the §2.12.1 thresholds (`≤+5 pp`→cut, `+5..+10`→ops-only+MMLU-Pro, `>+10`→revisit) with the n≈198 ±7 pp Wilson noise-floor caveat made binding on the read; (d) operator prerequisites honest about the single most-likely blocker (GPQA cases are `.gitignore`'d and operator-downloaded) plus the Anthropic-key and local-Ollama requirements and the auto-governor's OOM guard; (e) a ready-to-fill `scratch/bench-results-<date>.md` skeleton (config header → results table → key finding → decision checkboxes → feeds-back-into-doc) per the "Recording scored runs" convention; (f) an explicit NOT-in-scope list.

**Pricing-bug claim VERIFIED and CORRECTED (§4c.0):** the iter-2/6 assertion is **real on the rates** — `cmd/phase1` `defaultPricing` genuinely has Haiku `0.80/4.00` (→`1.00/5.00`) and Opus `15.00/75.00` (→`5.00/25.00`); Sonnet `3.00/15.00` is already correct; all confirmed against published June-2026 Anthropic pricing (Haiku 4.5 $1/$5, Sonnet 4.6 $3/$15, Opus 4.7 & 4.8 $5/$25; WebSearch cross-check, §5). **But the doc's prose carried one error, now corrected:** the stale Opus entry is keyed **`claude-opus-4-7`**, NOT `claude-opus-4-8` as iter-2's §2.8.1 note and iter-6's handoff implied — the fix corrects the *rate* on the existing key, it does not rename it (renaming would silently drop pricing for operators already passing `--frontier-model claude-opus-4-7`). Also clarified the blast radius the prior iterations elided: only the Haiku rate touches phase1's *default* cost ratio (default orchestrator = Haiku); the Opus rate is dead code on the default path, and `cmd/bench` never reads this map at all (it uses blended `--price`/`--frontier-price`), so the H0-cost *bench* runs don't even depend on the fix landing — the fix is a phase1-only correctness prerequisite, shipped as its own trivial PR. Open #1 updated to carry the verified diff + corrected key.

**Highest-leverage thing iteration 8 should tackle (the single handoff):** **the diff-level Thread A (router) PR-ready spec — the same §4b treatment applied to the larger Phase-3 surface.** With Thread B spec'd to diff-level (§4b) and H0-cost made copy-paste-runnable (§4c), the two cheapest/highest-power arms are *executable*; the remaining unbuilt graft is the router, whose larger surface (an additive `exemplar.Store.RetrieveAcross(ctx, prompt, k) []Neighbor` cross-action BM25 kNN reusing `bm25.go`'s index machinery; a new stdlib-only `pkg/router` with `ExemplarRouter{Store, K, MinConfidence, MinMargin}` doing majority-vote-over-neighbor-actions with a margin abstain; an *advisory* `runner.Config.Router` consulted before — never replacing — Evaluate, seeding a `[playbook-hint:…]` steering line or an additive `env.Evaluate.HintPlaybook` *bias*; the two trace events `router.hint` + `router.evaluate_overrode_hint` that ARE the experiment's primary disagreement-rate metric; and the `--router`/`--router-store`/`--router-k`/… `cmd/bench` flags, wired only on the `--tree` arm since Vote/Single bypass Evaluate) benefits most from a precise, source-checked, TDD-ordered spec before a build session opens the PR. Read the *real* `pkg/exemplar/bm25.go` + `exemplar.go` (the `buildBM25Index`/`score`/tokenizer to reuse), `pkg/runner` (where the advisory hook slots relative to `adapter.Evaluate`), and `pkg/session.Envelope.Evaluate` (whether `HintPlaybook` is additive-safe) and correct any iter-4 §2.10.3 sketch that's wrong about the real code, exactly as §4b.0 did for Thread B. *Alternative, if iteration 8 judges the doc build-complete: a consolidation pass / executive summary that flattens the seven-iteration arc into a clean one-page hand-off for a build session — what's spec'd to diff-level (Thread B §4b, H0-cost §4c), what's runnable now, the §2.12.7 run-order, and the one remaining unbuilt graft (router) — so a coder can start without re-reading 1300 lines.*

**Highest-leverage thing iteration 7 tackled (the single handoff, now DONE — see the iteration-7 entry below):** **the PR-ready spec for the H0-cost run (§2.12.1 / §2.12.7 steps 0–2) — it needs NO new code and can run the moment the pricing fix lands, making it the fastest real datum.** Per the §4a build order, H0-cost is "no new code; pure existing harness" — so iteration 7's job is to make it *executable as a checklist*, not to design more: (a) the exact step-0 one-line pricing fix in `cmd/phase1/main.go` `defaultPricing` (Haiku `0.80/4.00`→`1.00/5.00`, Opus `15/75`→`5.00/25.00`) as a literal diff, verified against the real file; (b) the two `cmd/bench` invocations (null = local Vote-5 on ollama-qwen2.5:7b; dense = same on anthropic-Haiku) with every real flag filled in and the `--report-out` paths; (c) the exact fields to read from the resulting JSON (`aggregates[].pass_rate`, `savings_ratio`, `total_actual_usd`) and the §2.12.1 decision arithmetic applied to them (`≤+5 pp` → cut; `+5..+10 pp` → operational-only + MMLU-Pro replication; `>+10 pp` → revisit); (d) the `scratch/bench-results-<DATE>.md` findings-file skeleton the run must distill into (per the "Recording scored runs" convention). This converts the cheapest hypothesis into a copy-paste-runnable procedure and produces the *second* keep/cut datum (after Thread B's), closing the H0-cost arm with measurement, not arithmetic. *Secondary, if H0-cost is already a checklist: the diff-level Thread A (router) spec — `RetrieveAcross` on `exemplar.Store`/`FileStore` (cross-action BM25 kNN reusing `bm25.go`), the `pkg/router.ExemplarRouter`, the `runner.Config.Router` advisory hook + two trace events, and the `--router*` flags — the same §4b treatment applied to the larger Phase-3 surface, which earns its build slot only after Thread B merges and its result is in.*

**Iteration 8 (2026-06-14) — the PR-ready Thread A (router) spec (design → buildable diff; the third and last graft).** Built §4d, grounded in the *actual* current source read end-to-end this iteration: `pkg/exemplar/exemplar.go` (the real `Store` interface, the per-action `Retrieve(ctx, action, prompt, k)` signature, `FileStore`, `loadActionLocked`, and the `var _ Store = (*FileStore)(nil)` compile assertion at L388), `pkg/exemplar/bm25.go` (`buildBM25Index`/`idx.score`/`tokenize`/`rankedHit` — the machinery reused verbatim), `pkg/runner/runner.go` (the exact `adapter.Evaluate(ctx, env)` call site at L416 inside `resolve`, the `Config` struct, the `r.mu` locked-helper discipline, and the governor-lease window), `pkg/session/envelope.go` (the `Evaluate` *pointer* + `Payload`/`Input.Content` the hint reads/writes + `NeedsVerification` as the additive-field precedent), `pkg/trace/trace.go` (the `Tracer.Event` + `trace.Info`/`Error` sugar shape), `pkg/benchmark/orchestrator/tree.go` (the real `runner.Config{}` construction at L96 the `--tree` arm uses), and `cmd/bench/main.go` (the `flag.Bool/String/Int/Float64` + `flag.Visit` props-fallback pattern, matching `--cope`/`--tree`). **Delivered:** (1) `pkg/exemplar/across.go` — `Neighbor{Exemplar, Sim}`, an optional `AcrossRetriever` interface, and `(*FileStore).RetrieveAcross` that iterates every action file, builds one BM25 index per corpus (reusing `bm25.go` unmodified), scores the query, and merges the global top-k by Sim→Score→AddedAt; deliberately does NOT bump `LastRetrievedAt` (sidecar read mustn't distort GC's LRU). (2) `pkg/router` (new, stdlib-only) — `Router.Hint(ctx, session.Payload) (Hint, error)`, `Hint{Playbook, Confidence, Margin, K}` + `IsEmpty`, `ExemplarRouter{Store, K, MinConfidence, MinMargin}` doing majority-vote over valid neighbor playbook labels with confidence+margin abstain, a `winnerRunnerUp` helper with deterministic name tie-break, and a `validPlaybook` guard. (3) The runner hook — additive `Config.Router`, ~12 lines before `adapter.Evaluate` appending an Option-A `[playbook-hint: …]` steering line to a copy of `env.Input.Content` (never a skip), a post-Evaluate disagreement-detect, two trace events (`router.hint_produced`/`router.evaluate_overrode_hint`) matching the real `trace.Info` sugar, a `hintForAP` map + `takeHintForAP` locked helper, and `RunState.RouterHintsProduced`/`RouterHintOverrides` (the disagreement rate = overrides/produced, the §2.12.2 primary metric). (4) The `--router`/`--router-store`/`--router-k`/`--router-min-confidence`/`--router-min-margin` flags (default OFF), a `--router`-requires-`--tree` fatal guard, and a one-line additive `orchestrator.Tree.Router` field forwarded into its `runner.Config`. (5) A 14-step TDD table (test file + failing-assertion named per step, package-before-consumer order) tied to the one-PR-off-`main` workflow lock, and a scope fence (no embeddings, no model-tier routing, no Option-B schema field, no hard skip, no `Store` change, no experiment RUN). (6) A lock re-verification table against the real code post-correction.

**Four code-vs-doc mismatches found and CORRECTED (§4d.0):** (1) **`RetrieveAcross` must NOT go on the `Store` interface** — the iter-4 §2.10.3 sketch put it there ("ONE additive method on the store interface"), but `Store` is consumed by `pkg/curation`/`pkg/adapter/curated` and pinned by `var _ Store = (*FileStore)(nil)`; widening the interface breaks all implementers, and a free function is impossible because `Retrieve` needs the action up front and loads one file. Corrected to a `*FileStore` method + an optional `AcrossRetriever` interface the router type-asserts; `Store` byte-for-byte unchanged. (2) **`Exemplar.Action` is a free `string`** (curation *happens* to write `session.Playbook` values), so the router needs a `validPlaybook` filter — the sketch trusted every label, which would let a corrupt/legacy store hint a playbook the adapter can't run. (3) **The hook slots at runner.go:416 inside `resolve`, immediately before `adapter.Evaluate`** — and `env.Evaluate` is a `*Evaluate` that is **nil on entry** (the adapter *produces* it), so the sketch's "set `env.Evaluate.HintPlaybook`" can't work — there's no struct yet. (4) Consequently **v0 is Option A only** (steering text into `env.Input.Content`, which exists); Option B's field, if ever built, is a top-level `HintPlaybook` envelope field (a sibling of `NeedsVerification`), NOT inside the nil `*Evaluate` — and is explicitly deferred per §2.9.5.

**Highest-leverage thing iteration 9 should tackle (the single handoff — now DONE, see the iteration-9 entry below):** the consolidation / executive-summary pass AND/OR the §2.12.2 router-inertness stress-test. Iteration 9 did both.

**Iteration 9 (2026-06-14) — the executive summary + the router-inertness stress-test; research declared CONVERGED.** Did BOTH high-leverage things the iteration-8 handoff offered, because they were complementary.

*Part 1 — Executive summary (TOP of doc).* Added a one-page summary after the title: the dense verdict in 2–3 sentences (mid-tier only; packaging/ergonomics not cost; locks vindicated), a compact 3-row table of the buildable outcomes (Thread B / H0-cost / Thread A) each with what-it-is + keep/cut gate + lock verdict + PR-section pointer, the §4a build order in one line, a question→section pointer table, and a convergence note. Tight, points to detail rather than duplicating it.

*Part 2 — Stress-test (§2.12.9).* Stress-tested the single most load-bearing untested assumption: the §2.12.2 prediction that the kNN router is inert on GPQA (< 5% disagreement → cut). **Verdict: the prediction is correct but VACUOUS — GPQA is rigged against the router by the harness, not measuring it.** Reasoned it against the real code: (i) curation writes every exemplar under one `--curate-action` (default `process`; `curation.go:250`, `cmd/bench:85`), so a GPQA store is single-action and the kNN majority vote is a constant `process` hint (conf 1.0, margin 1.0) on every AP — no label diversity, the degenerate single-intent case the semantic-router literature describes; (ii) the `Tree` arm (the only router-measurable bench arm) hard-pins every child AP's Evaluate to `process` (`tree.go:156–163`, never calls inner Evaluate for children) and forces the root to `plan`-or-`process`. Composed, the disagreement rate is mechanically near-zero *before the run starts* — a tautology, not a measurement. Did real research on when retrieval-routing helps (workload heterogeneity / overlapping score distributions / distinct routes — emergentmind LLM-routers, arXiv:2509.11079; Zep/vLLM intent-router degeneracy on single-intent corpora). **GPQA is the homogeneous extreme (every case wants `process`), so it can neither keep nor honestly cut the router.** The fair test is a heterogeneous-playbook workload — closest in-repo is the Phase-1 email corpus — BUT a second finding: *no existing harness path produces a multi-action store AND walks Evaluate-per-AP over heterogeneous inputs* (`cmd/phase1` doesn't walk the runner's Evaluate path; `Tree` pins children; curation writes one action at a time). So the router's fair test must be *constructed*: minimally a hand-seeded multi-action JSON store (`FileStore.Dir/<action>.json`, no curation run needed) + an unconstrained Evaluate walk via `cmd/oscillitron --router` or a thin new driver (§2.12.9.3). **Build-order consequence (corrected into §4a Phase 3 + §2.12.2):** Thread A's keep/cut test moves OFF GPQA entirely; the §2.12.7 steps 6–8 GPQA sequence is struck as the keep/cut decision (kept only as an inert-by-construction wiring sanity check); and absent a builder for the heterogeneous harness, **Thread A's honest status is "spec'd, not built, and not fairly-testable on any current workload"** — which is itself the signal that the router solves a problem the present workloads don't have. §4d's code spec is unaffected (still correct + lock-clean); only the *experiment that decides whether to keep it* changed.

**CONVERGENCE DECLARATION (the loop owner should read this).** The research has **converged.** Over 9 iterations the doc went: cost falsified by arithmetic (§2.8) → calling protocol specified with no new primitives (§2.9) → two lock-compatible grafts sketched (§2.10–2.11) → turned into a falsifiable decision procedure (§2.12) → all three buildables spec'd to diff level (§4b/§4c/§4d) → consolidated into a 60-second exec summary → the last load-bearing assumption (router inertness) stress-tested and found to need a workload that doesn't exist. **There is no remaining high-leverage *design* question.** Every open item is now either (a) *execution* — open the PRs (§4b/§4c/§4d) and run §2.12.7 against a real local substrate, which is a coding/ops session, not a research iteration; or (b) *contingent on a measurement that hasn't been taken* (the embedding-dependency posture, gated on the free-form arm) or *on a workload no one has committed to build* (the router's heterogeneous harness). **Iteration 10 has nothing genuinely high-leverage to advance on paper.** A further design iteration would be busywork — re-deriving settled conclusions or polishing prose. The correct next action is to **stop the loop and hand to a build session**, OR, if the loop must continue, to spend it on the ONE thing that is still real-but-deferred: *decide whether anyone will build the §2.12.9.3 heterogeneous harness, because that single decision determines whether Thread A is ever testable* — but that is a prioritization call for the owner, not a research question the loop can answer by thinking harder. **Recommendation: the loop has done its job; stop here and execute.**
