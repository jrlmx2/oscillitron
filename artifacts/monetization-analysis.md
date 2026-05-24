# Oscillitron Monetization Analysis: Preserving the Cost Wedge

**Author:** Claude, for Jim
**Date:** 2026-05-18
**Status:** v1 — exhaustive treatment, intended as decision-input
**Audience:** layered — §0 TL;DR is for skim/external readers; §1–§5 are the working analysis; §6–§8 are anti-patterns, open questions, and an assumption appendix.

---

## §0 — TL;DR

Oscillitron's defining commercial promise is *"production-grade LLM handling at a fraction of frontier cost."* Any monetization that bundles a per-query markup on inference erodes that promise, because frontier prices keep falling and the savings the user perceives must stay large enough to overcome switching cost (typically ≥3× cheaper, empirically). The conclusion of this analysis is that **the durable margin lives in the orchestration layer, the compliance layer, and packaged specialization — never in inference resale**.

Concretely, three patterns survive scrutiny: (1) **BYO-keys + orchestration fee** — Oscillitron never touches the inference bill, the user routes their own cheap-model providers and pays a flat or per-task fee for the brain on top; (2) **regulated-industry vertical packaging with compliance audit hosting** — the §11.2 commercial add-ons in the framework-design doc, repriced as a managed service that wraps the existing audit-ledger/reproducibility primitives; (3) **consulting + custom-specialist development** — the framework-design §11 thesis, retained as the seed revenue engine. Two patterns are tempting but actively destructive: per-token markup on inference (commodity race, value capture inverts as frontier prices drop) and crippled-OSS-core gating (violates the framework-design §11.3 principle and kneecaps adoption, which is the lead-gen mechanism). The recommended sequencing follows the existing phase plan: monetize nothing before the Phase 1 kill-or-proceed gate; layer in consulting + compliance hosting after Phase 4; introduce vertical packaging after Phase 7; never bring a hosted-inference SKU into the lineup.

Unit-economics modeling under stated mid-2026 assumptions shows roughly **3× gross cost-wedge over frontier mid-tier** (≈$0.013 Oscillitron-cost vs. ≈$0.04 frontier-cost for a 4K-token reasoning task), giving ~$0.027 per task to split between user-side savings and Oscillitron-side margin. Within that envelope, charging in the $0.020–0.025 band per task delivers ~40–50% user savings at ~35–48% gross margin — a viable box, but a narrow one that collapses fast under any of: (a) frontier prices halving again, (b) cheap-model providers raising prices, (c) orchestration overhead being underestimated. **Architectural choices that keep Oscillitron off the inference bill entirely (BYO-keys) are robust to all three; choices that put Oscillitron on the inference bill are robust to none.** That asymmetry is the spine of the recommendation.

---

## §1 — The Constraint, Precisely

### 1.1 What "cheaper than frontier" means operationally

The slogan has to resolve to a number before it constrains anything. From framework-design §3, the stated target is *"85% quality at 15% cost on representative workloads"* — i.e., a ~6.7× cost wedge at near-frontier quality. That's the explicit promise. For monetization analysis, three operational variants matter and they are not interchangeable:

- **Raw inference cost wedge** — the cost of model calls only, Oscillitron stack vs. a single frontier call on the same task. This is what the architecture earns. It is also the easiest to measure and the easiest to lose to scaling-down frontier prices.
- **Total cost of completion wedge** — inference + orchestration overhead + verifier/audit + occasional strong-model escalation. This is what the user actually pays under any "Oscillitron handles it" pricing. It's the number that has to stay ≥3× lower than frontier-on-same-task for switching to feel rational.
- **Cost-of-meeting-requirements wedge** — the same as above plus compliance overhead (audit ledger storage, reproducibility manifests, PII detection). For regulated buyers this is the *only* number that matters, and frontier APIs often don't even produce a comparable number because they don't meet the requirements at any price.

The compliance variant is where the wedge stops being a commodity benchmark and starts being a category-defining feature. Frontier APIs in mid-2026 still don't ship cryptographically signed audit ledgers, reproducibility manifests, or classification-aware routing primitives. A regulated buyer comparing Oscillitron to "GPT-5 plus our own homegrown audit middleware" is comparing against a number that includes a salaried compliance engineer, not a per-token rate. The wedge widens substantially in that comparison even if the raw-inference wedge is thin.

### 1.2 What erodes the wedge

A taxonomy of erosion vectors, ranked by how much they should worry monetization design:

1. **Per-token markup on inference.** If the price the user pays scales with inference tokens, the user's effective price tracks frontier price minus discount. As frontier prices drop, either the absolute savings shrink or Oscillitron's margin shrinks. There is no third option. This is the single largest erosion vector and most "obvious" monetization patterns (per-token, per-million-tokens, "we resell at X% off OpenAI") fall in this bucket.
2. **Orchestration overhead growth.** Each AP/summary handoff, each verifier call, each audit sample is real inference. The skeleton's design (router + 2 specialists + verifier + 10% audit) costs roughly 3-4× the tokens of a single frontier call against the same task. The wedge survives because cheap-model rates are 5-10× lower than frontier rates, so 3-4× token volume at 1/8 per-token cost still wins. But this margin is computed against assumed token ratios; if real workloads need 6+ specialist hops or 50% audit sampling, the wedge collapses.
3. **Frontier price collapse.** Frontier API prices have dropped roughly 50% year-over-year for the past two years. If that continues, today's 3× wedge could be 1.5× by 2027 and at-par by 2028. Pricing models that survive only when the wedge is wide are bad bets.
4. **Cheap-model price increases.** Less likely than frontier collapse in the near term (the cheap-inference layer is competitive and capacity-rich), but a real risk if open-weight model providers consolidate. Oscillitron's wedge depends on cheap-inference availability; a monetization model that captures value somewhere other than the inference bill is robust to this.
5. **Quality drift.** If specialization grows poorly — playbook rot, sycophancy drift, verifier loop captured — Oscillitron's effective quality drops, and the *quality-adjusted* wedge erodes even at constant cost. The §11.2 audit ledger and audit-sampling primitives exist partly to make this measurable; they should also figure into the monetization story (paid audit-hosting is the way drift detection scales).
6. **Operational complexity tax.** Self-hosted Oscillitron means the user runs Hermes instances, manages routing topology, curates playbooks. If the cognitive overhead is high enough, the user effectively pays for it in time even when they pay nothing in dollars. Hosted offerings address this but they have to be priced carefully (see §3.H).

### 1.3 Who the wedge is *for*

A critical distinction the framework-design doc makes implicitly but doesn't name: **the cost wedge is a user-side promise, not a vendor-side margin target.** Oscillitron does not need to "be cheap" — it needs to *deliver cheap inference to its users while sustaining itself.* Those are different problems. Many monetization patterns that would be fatal if Oscillitron's margin had to track the wedge are perfectly viable when Oscillitron is paid for a different layer of value entirely.

Concretely: if Oscillitron charges a flat $40/month per developer seat for the orchestration brain and the user runs their own inference, Oscillitron's revenue has *no relationship* to per-task inference cost. The wedge stays intact at whatever multiple the user's cheap-inference setup achieves; Oscillitron's margin is independent. This is the strongest structural argument for the BYO-keys + orchestration-fee pattern recommended in §3.F.

### 1.4 Interaction between the two value propositions

Framework-design names two value propositions: the **cost wedge** and the **compliance moat**. They interact in ways monetization design must respect:

- The cost wedge is broad-market but commoditizing. Many compound-AI frameworks claim cost savings; the differentiator narrows over time.
- The compliance moat is narrow-market but durable. Regulated buyers move slowly, pay well, and rarely switch.
- The compliance moat is *only payable if the cost wedge holds* — regulated buyers want the savings too, they just want them on top of the compliance primitives, not instead of.
- Conversely, the cost wedge alone is hard to monetize directly (any monetization narrows it); the compliance moat creates a parking lot for monetization that doesn't narrow the wedge.

This is why §5 of this analysis weights compliance-bundle pricing patterns more heavily than pure cost-wedge resale: the compliance moat is the only place where Oscillitron can capture significant value *without subtracting it from what the user perceives as savings*.

---

## §2 — Unit-Economics Foundation

All numbers in this section are **stated assumptions**, not measured values. The point is to build a model whose assumptions can be challenged individually rather than smuggle in conclusions through vague pricing. The full assumption sheet is in §8.

### 2.1 Frontier baseline (mid-2026)

Frontier API prices in May 2026, assumed at roughly 50% of May 2025 levels (extrapolating the observed 30–50% YoY decline):

| Tier | Input ($/M tokens) | Output ($/M tokens) | Source/assumption |
|---|---|---|---|
| Frontier flagship (Opus 4.6 / GPT-5 class) | $5.00 | $25.00 | Extrapolated from 2025 Opus 3 pricing |
| Frontier mid-tier (Sonnet / GPT-4o class) | $1.50 | $7.50 | Extrapolated from 2025 Sonnet 3.7 pricing |

**Reference task.** A "medium-complexity reasoning task" — 2,000 input tokens, 2,000 output tokens. Examples: code review on a 200-line diff, structured analysis of a 4-paragraph document, multi-step math word problem.

| Tier | Cost per reference task |
|---|---|
| Frontier flagship | $0.010 + $0.050 = **$0.060** |
| Frontier mid-tier | $0.003 + $0.015 = **$0.018** |

Frontier mid-tier is the realistic comparison for "production task" cost — most production workloads do not use flagship models because they're 3× the price. Mid-point baseline: **~$0.040 per reference task**, which is what most cost-conscious teams currently pay.

### 2.2 Oscillitron cost stack (per reference task)

Mid-2026 assumed cheap-inference rates (Together / Groq / Fireworks / DeepInfra hosting Llama 3.3 70B, Qwen2.5 72B, Mixtral 8x22B class models): blended ~$0.50 per million tokens for both input and output (cheap-tier providers often don't split input/output pricing).

Reference task chain in Oscillitron (matches the skeleton's `code-analyst → fact-check → writer` demo plus router + verifier):

| Component | Tokens in | Tokens out | Cost @ $0.50/M |
|---|---|---|---|
| Router (cheap LLM, picks specialist) | 500 | 100 | $0.00030 |
| Specialist 1 | 2,000 | 2,000 | $0.00200 |
| AP/summary handoff | (compressed) | 500 | $0.00025 |
| Specialist 2 (sees orig + AP) | 2,500 | 2,000 | $0.00225 |
| Verifier loop | 4,500 | 100 | $0.00230 |
| Audit sample (10% of tasks @ flagship cost) | — | — | $0.00400 |
| Orchestration compute/storage/network | — | — | $0.00200 |
| **Total per task** | | | **~$0.0131** |

**Gross cost wedge:** $0.040 (frontier mid-tier baseline) / $0.0131 (Oscillitron) ≈ **3.05× cheaper**. Below the framework-design §3 *aspirational* target of 6.7× but well within the "≥3× to feel rational to switch" empirical threshold called out in §1.

### 2.3 Where the wedge actually comes from

The wedge is not magic; it is arithmetic. Cheap-tier inference is **9× per-token cheaper** than frontier mid-tier on a 50/50 input/output blend ($0.50/M vs. an effective $4.50/M weighted from $1.50 in / $7.50 out). The per-token wedge is asymmetric: 3× on input tokens, 15× on output tokens — output-heavy tasks benefit more. Oscillitron's full session-graph uses roughly **3.55× more tokens** than a single frontier call would (14,200 vs. 4,000 in the reference task), because of routing + handoffs + verification. Raw-inference wedge ≈ 9 ÷ 3.55 ≈ **2.5×** before audit-sampling and overhead are folded in.

The $0.040 baseline used in §2.4 is a *mix* of frontier mid-tier ($0.018) and frontier flagship ($0.060) — reflecting that most production teams use mid-tier as default with occasional flagship escalation. Against pure mid-tier alone, the wedge is only **~1.37×** ($0.018 / $0.013). Against pure flagship, it's **~4.6×**. The headline ~3× number lives in between, where realistic production cost mixes sit.

This decomposition is the most important diagnostic in the whole document. **The wedge is a ratio of two ratios**, and either ratio can swing it. If frontier prices halve again (per-token wedge → 4.5× blended) and architecture stays at 3.55× token volume, the raw-inference wedge becomes ~1.3×. If cheap-tier prices stay flat while frontier collapses, the same thing happens. **And — crucially — if the comparison baseline is pure mid-tier rather than a flagship-mixed baseline, today's wedge is already only ~1.4×.** Many cost-conscious users will run that comparison. The wedge is not load-bearing on architecture alone, and it is not as wide against the most realistic single-tier comparison as the §2.4 headroom table implies.

This is the strongest possible argument for monetization patterns that do not depend on the wedge for revenue (patterns F, J, K in §3). The arithmetic does not afford aggressive per-task pricing against mid-tier-only buyers.

### 2.4 Monetization headroom: where margin can live

The headroom is the gap between **frontier-equivalent cost** (what the user is willing to pay to not be on frontier) and **Oscillitron cost** (what it actually costs Oscillitron to deliver). Under the §2.2 numbers:

- Frontier-equivalent: $0.040 / task
- Oscillitron cost: $0.013 / task
- Total headroom: **$0.027 / task**

This $0.027 is split between (a) the user's perceived savings and (b) Oscillitron's margin. Three illustrative pricing points:

| Price/task | User saves vs. frontier | User savings % | Oscillitron gross margin/task | Oscillitron margin % |
|---|---|---|---|---|
| $0.025 | $0.015 | 37% | $0.012 | 48% |
| $0.020 | $0.020 | 50% | $0.007 | 35% |
| $0.015 | $0.025 | 62% | $0.002 | 13% |

**Read across the rows: there is no point that simultaneously delivers a category-defining 70% user savings and a comfortable 40%+ margin.** The wedge is wide enough to monetize but not wide enough to monetize *aggressively*. At $0.015/task the margin is thin enough that any underestimate of orchestration overhead, audit sampling rate, or chain depth wipes it out.

This is the strongest numeric argument against per-task pricing as Oscillitron's primary SKU. The model survives the math but doesn't have the headroom to absorb forecasting error.

### 2.5 Sensitivity analysis

Holding the architecture constant, varying frontier price (the most likely-to-move variable):

| Frontier mid-tier $/task | Wedge multiple | Headroom for monetization |
|---|---|---|
| $0.060 (no change from 2025) | 4.6× | $0.047 |
| $0.040 (2026 base case) | 3.0× | $0.027 |
| $0.025 (further 35% drop) | 1.9× | $0.012 |
| $0.018 (matches Oscillitron cheap-tier raw) | 1.4× | $0.005 |
| $0.013 (parity) | 1.0× | $0.000 |

At the $0.025 frontier price (a single ~35% further drop, plausible within 12-18 months), the headroom drops by more than half. The pricing model has to either (a) live entirely in a layer where this number doesn't affect revenue, or (b) be able to halve its own price-per-task and still cover cost.

(a) is BYO-keys + flat orchestration fee. (b) is essentially "be the lowest-margin operator forever," which is not a business; it's a calling card for the consulting business.

### 2.6 Compliance-buyer unit economics — a different planet

For a regulated buyer (banking SR 11-7, healthcare HIPAA, government FedRAMP), the cost comparison shifts entirely. The frontier alternative isn't $0.040/task; it's $0.040/task **plus**:

- Salaried compliance engineer to manage audit middleware: ~$200K loaded ÷ 1M tasks/year amortization = **$0.20/task** *if* the workload is high enough to amortize; for typical regulated workloads (~100K tasks/year) it's $2.00/task
- Custom audit infrastructure development: amortized capex of $300K–$1M, hard to spread per-task; commonly accounted as fixed overhead, but for monetization-positioning purposes "Oscillitron ships this in the box" is a real number on the order of $0.10–$1.00 per task during the first 2–3 years of operation
- Reproducibility tooling: similar order

A regulated buyer's effective frontier cost is on the order of **$0.30–$2.00 per task**, not $0.040. Against that baseline Oscillitron's wedge is 20-150×. The monetization headroom is enormous, and the question shifts from "how much margin can we capture" to "how much value can we credibly deliver and price accordingly."

This is the §1.4 point made numerically: **the compliance moat parking lot is where monetization can be aggressive without narrowing the user-perceived cost wedge.** A regulated buyer paying $0.15/task for Oscillitron + compliance hosting saves enormously vs. their alternatives, and Oscillitron has 10× the gross margin it would in a non-regulated comparison.

---

## §3 — Survey: 14 Monetization Archetypes

Each pattern below is described, scored against the wedge, and given a verdict. The verdicts are summarized in §4. Patterns are grouped: **A–E per-unit pricing**, **F–H infrastructure plays**, **I–J licensing plays**, **K–L specialization plays**, **M–N indirect/community**.

### A. Per-token markup on inference

**Pattern.** Oscillitron operates as a paid endpoint. User sends a request, Oscillitron handles routing/inference/verification internally, charges per million tokens consumed at the endpoint (typically a markup of 20–50% over what Oscillitron pays for cheap-tier inference). Closest exemplar: OpenRouter, Together, any aggregator.

**Where it bites the wedge.** Directly and continuously. The user's bill scales with their usage; as frontier prices drop, Oscillitron either drops prices in lockstep (margin collapses) or holds prices (user-perceived wedge collapses). Every assumption-error in §2 hits margin directly.

**Where it's defensible.** Marketed correctly, it appeals to small/medium customers who want a single bill and don't want to think about routing. The convenience tax is real. But it's a low-margin, high-volume business that requires a sales motion the framework-design doc explicitly says is out of scope.

**Verdict: avoid.** Worst alignment with the cost-wedge promise, worst fit for the solo-maintainer business model, race-to-the-bottom dynamics with OpenRouter, Together, and the long tail of inference aggregators. This is the single most tempting and most destructive monetization pattern Oscillitron could adopt.

### B. Per-task pricing (flat-rate per resolved task)

**Pattern.** Oscillitron charges a flat fee per "task" (where task is defined by a session-graph boundary — one entry, one decomposed-and-recomposed output). Customer doesn't pay for retries, restarts, audit samples — Oscillitron eats those.

**Where it bites the wedge.** Less directly than A because the price isn't visibly linear in tokens, but the underlying economics are the same: Oscillitron's margin is the difference between fixed fee and variable cost. §2.4 showed this is a narrow box.

**Where it's defensible.** Predictable customer bills are valuable, especially for budget-controlled buyers. Hides architectural complexity from the user. Aligns with the "specialist instance per task" lifecycle from design-notes.md. If Oscillitron can credibly hold operational overhead per task constant as workloads grow, the model can scale.

**Verdict: defensible but secondary.** Better than A. Worth considering as a hosted SKU layered on top of BYO-keys for users who don't want to manage their own inference providers. Not the primary monetization vehicle because the margin box is too narrow.

### C. Per-outcome pricing (pay only on verifier success)

**Pattern.** Oscillitron charges only when the verifier passes the output (grounded checks pass, no inhibitor abort, audit-eligible). Failed runs cost the customer nothing.

**Where it bites the wedge.** Theoretically aligned (Oscillitron's revenue is gated on quality, which aligns incentives), but the unit economics are punishing — Oscillitron eats the cost of every failure plus restart. From design-notes.md §inhibition, the inhibitor exists *because* failures are expected, so the failure rate is non-trivial.

**Where it's defensible.** Strong marketing story: "you only pay for outcomes." Differentiates against frontier APIs which charge per token regardless of output quality. Strong alignment with the layered verifier described in CLAUDE.md (grounded checks form the floor; pricing rides on the same signal).

**Verdict: high-leverage *as a premium SKU*, dangerous as the default.** Per-outcome pricing only works at a premium price — Oscillitron has to charge enough to cover failure rate + margin. If failure rate is 15% and margin target is 40%, the per-outcome price needs to be ~70% above the per-task cost. That's a fine premium SKU; it's not a price you lead with.

### D. Subscription with usage cap

**Pattern.** Customer pays $X/month for up to Y tasks (or Y million tokens). Overages priced per-unit. Closest exemplars: Anthropic Claude.ai Pro ($20/month, capped usage), GitHub Copilot ($10/month).

**Where it bites the wedge.** Indirectly. If the cap is set generously, most customers use less than they pay for; that's where the margin comes from. The wedge is preserved for power users only if overage pricing is also wedge-preserving.

**Where it's defensible.** Predictable revenue, low cognitive overhead for buyers, well-understood pricing model. Works especially well for developer-individual buyers who want "AI infrastructure" as a fixed monthly line item. Pairs naturally with the BYO-keys pattern (F) — sub fee for orchestration, BYO inference.

**Verdict: viable for individual developers as a packaging tactic.** Not the primary monetization for the regulated-industry vertical, but a reasonable on-ramp SKU for hobbyists and small teams. Strongest fit when bundled with BYO-keys: the sub fee buys the orchestration layer, inference cost remains on the user's account.

### E. Reserved-capacity / committed-spend

**Pattern.** Enterprise prepays $X for Y tasks over Z months, gets discounted unit price. Standard enterprise SaaS motion.

**Where it bites the wedge.** Same as A/B but cash-flow positive. The discount is real value to large buyers; the margin is preserved on the prepay structure (Oscillitron has the cash earlier, can deploy it).

**Where it's defensible.** Enterprise procurement loves committed spend. CFOs prefer predictable numbers. Works well alongside compliance subscriptions.

**Verdict: viable enterprise SKU, not a starter SKU.** Requires sales motion and enterprise procurement infrastructure that a solo project doesn't have. Worth keeping in mind as a Phase 8+ option once the consulting practice has produced reference customers.

### F. BYO-keys + orchestration fee ⭐

**Pattern.** Oscillitron's hosted/managed offering never owns inference. The user brings their own API keys (Together, OpenRouter, Anthropic, OpenAI, in-house vLLM endpoint). Oscillitron orchestrates, routes, verifies, and runs the audit ledger. Customer pays Oscillitron a flat or per-task fee for the orchestration layer; pays their inference providers directly.

**Where it bites the wedge.** **It doesn't.** Oscillitron's revenue is entirely decoupled from inference cost. The user's wedge is whatever their inference provider gives them; Oscillitron's margin is whatever they're willing to pay for orchestration. Frontier price collapse doesn't touch Oscillitron's revenue. Cheap-inference price spike doesn't touch Oscillitron's revenue. **The wedge stays user-side; the margin lives in a layer that doesn't compete with inference economics at all.**

**Where it's defensible.** Strong alignment with the wrap-Hermes architecture (CLAUDE.md): Oscillitron is already structured as "an orchestration wrapper above per-instance inference." Strong alignment with regulated buyers who need to keep inference on their own infrastructure for data residency. Eliminates the "we'll go bankrupt if frontier prices drop" risk class. Eliminates the "we have to be a hosting business" problem.

**Pricing models that fit.** Flat per-developer-seat ($40–$200/month/seat, similar to GitHub Copilot Business), per-task orchestration fee ($0.005–$0.015/task, well below frontier-equivalent inference), or per-deployment site license. Any of these survive the §2.5 sensitivity analysis.

**Verdict: strongest primary monetization pattern.** This is the spine of the recommended sequence in §5. It's also the closest fit to the framework-design.md positioning of Oscillitron as an "orchestration substrate above vLLM/SGLang/NIM" — the framework is *already designed* as a value layer on top of inference rather than as inference itself.

### G. Sidecar / router-only license

**Pattern.** Customer runs their own everything (inference, Hermes, application logic); Oscillitron ships only the router specialist and AP bus as a sidecar component, licensed per-deployment or per-developer.

**Where it bites the wedge.** Not at all (Oscillitron is paid for a component, not for inference). Margin is fixed; cost is software development amortized across the install base.

**Where it's defensible.** Appeals to users who already have heavy inference infrastructure and want to bolt on the routing layer. Low operational burden on Oscillitron — no hosting, no SLA. Pure-software margins.

**Verdict: solid secondary SKU but limited TAM.** Most users who would buy this are sophisticated enough to build their own router given the open-source reference implementation. Worth considering as a paid extension for enterprise customers who want vendor support on a specific component, but not a primary revenue line.

### H. Open-core + hosted SaaS

**Pattern.** Core orchestrator is OSS (framework-design §11.1). Hosted offering bundles Oscillitron-as-a-service: managed Hermes instances, managed routing topology, managed playbook curation, managed audit ledger. Customer pays hosting + premium for the managed surface.

**Where it bites the wedge.** Depends on pricing model. If hosted SaaS is priced per-token (this becomes pattern A) — bad. If hosted SaaS is priced flat-per-seat or flat-per-deployment with BYO-keys for inference — equivalent to pattern F, good.

**Where it's defensible.** The "managed Hermes instances" piece has real value — Hermes is a fast-moving project (~99K stars in 8 weeks per CLAUDE.md), keeping a fleet of instances production-stable is operational work the user might happily outsource. The managed audit ledger has compliance value that justifies premium pricing on its own (see pattern K).

**Verdict: viable as a packaging, but the pricing model matters more than the SaaS label.** "Hosted Oscillitron, BYO-keys, flat per-seat" is good. "Hosted Oscillitron, we charge per token end-to-end" is pattern A in a different wrapper.

### I. Dual license (AGPL + commercial)

**Pattern.** OSS core released under AGPL (strong copyleft); commercial license available for closed-source/hosted use cases. Customers who want to use Oscillitron as a component in a closed product pay for a commercial license that removes the AGPL obligations. Closest exemplars: MongoDB (pre-SSPL), GitLab (pre-CE/EE split done well), Sentry.

**Where it bites the wedge.** Not at all (license fees are paid per-use, not per-query). Aligned with the framework-design §11.3 thesis that the framework is the lead generator and revenue comes from selective commercial relationships.

**Where it's defensible.** The classical dual-license play. Works especially well when the OSS core is genuinely useful (which framework-design §1.3 explicitly commits to) and the buyer's alternative (replicating the core internally) is expensive.

**Verdict: viable but contingent on license choice.** The framework-design.md leading recommendation is Apache 2.0 (§11.1), which is permissive and forecloses the dual-license play entirely. AGPL is the prerequisite. **This is a real decision-fork** — choosing Apache 2.0 closes the dual-license door; choosing AGPL opens it but creates adoption friction (some companies refuse to install AGPL software on principle). The friction is real but probably overestimated: the audience for Oscillitron is technical enough to read the license, and the regulated buyers we care most about have lawyers who can read AGPL without panicking.

### J. Vertical packaging (legal-tron, finance-tron, medical-tron) ⭐

**Pattern.** Ship the OSS core for free. Sell preconfigured *specialist seed kits* for verticals: a curated playbook set, a curated content-specialist seed list, a curated retrieval index, and a curated routing topology, tuned for legal/finance/medical/etc. workloads. Customer gets a working Oscillitron deployment for their domain in days, not months.

**Where it bites the wedge.** Not at all. Vertical packaging is a one-time or subscription license fee for the configured-system; inference economics are untouched. Customer's wedge is whatever the architecture delivers.

**Where it's defensible.** This is where the *organic-within-niche* specialization story (CLAUDE.md / framework-design §4) becomes a *product*. The whole point of the architecture is that domain-specific specialists are differentiable; if Oscillitron is the platform that ships the most curated specialist kits, the platform value compounds. Each vertical kit can be priced in the $5K–$50K range for an annually-licensed deployment kit, comfortably above what a single domain customer would pay for a generic AI tool.

**Especially defensible for regulated verticals.** A "finance-tron" kit that ships with SR 11-7 audit ledger configuration, a curated set of finance-domain playbooks, a finance-specific retrieval index, and validated reproducibility manifests is a *very* valuable artifact for a bank. The framework-design §10 regulatory mapping is the spec for what these kits ship.

**Verdict: highest-leverage secondary monetization pattern.** Best fit with the architecture, best fit with the framework-design.md regulated-industries thesis, best fit with the consulting business in §11.2 (vertical kits emerge naturally from consulting engagements). Should be the second monetization SKU layered in (after consulting), not the third.

### K. Compliance / audit subscription ⭐

**Pattern.** OSS core ships the audit ledger primitives. Commercial offering hosts the audit ledger as a managed service: tamper-evident storage, regulator-friendly retention, evidence-export tooling, signed-snapshot guarantees, per-tenant key management. Pricing: annual subscription per-deployment ($10K–$100K/year depending on volume and regulatory scope), or per-task storage with reserved capacity.

**Where it bites the wedge.** Not at all. Audit hosting is a separate value layer; it doesn't appear in per-task cost from the user's perspective. The user perceives "I got the cost wedge AND I got audit hosting so I don't have to build it."

**Where it's defensible.** The most defensible pattern in the entire matrix. Audit-ledger hosting has:
- High switching cost (regulator-friendly retention chains lock customers in for years)
- High value (replaces a $200K-loaded compliance engineer's worth of infrastructure work)
- Compounding moat (the longer the customer is on it, the more historical audit data lives there)
- Natural pricing power (regulated buyers don't price-shop on audit infrastructure the way they do on inference)
- Direct alignment with §11.2 of framework-design

**Verdict: tied with F (BYO-keys) for primary monetization vehicle.** F is the breadth play; K is the depth play. Both should be in the portfolio. K probably ramps slower (regulated sales cycles are 6-18 months) but has higher per-customer ACV (annual contract value) and stronger retention.

### L. Playbook marketplace with revenue share

**Pattern.** Third-party creators publish playbooks, exemplar libraries, or specialist seed kits to a marketplace. Customers install playbooks into their Oscillitron deployment for a one-time or subscription fee. Oscillitron takes a percentage of marketplace transactions. Closest exemplar: Hugging Face model hub (free) → various commercial-creator-tier offerings; GitHub Copilot Extensions (early stage); Roam/Obsidian community plugins (no revenue share but model adjacent).

**Where it bites the wedge.** Not at all (marketplace revenue is decoupled from inference).

**Where it's defensible.** *Eventually.* A marketplace needs three populations to function — many creators, many buyers, and a discovery layer that connects them. None exist yet. Marketplaces have well-documented chicken-and-egg dynamics; they fail when launched too early.

**Verdict: long-term option, not a near-term monetization vehicle.** Don't try to launch the marketplace before there are 1,000+ active Oscillitron deployments. By that point the marketplace probably emerges naturally and Oscillitron can monetize it. Trying to force it earlier wastes maintainer time and produces an empty store that signals weakness.

### M. Consulting + custom integration ⭐

**Pattern.** Author (Jim) takes consulting engagements at premium rates for regulated-industry deployments: architecture advisory, migration from frontier APIs to Oscillitron, compliance evidence package design, calibration, custom specialist development. Closest exemplars: Tidelift (open-source maintainer paid by enterprise users), Sidekiq Inc. (Mike Perham as one-person company on top of Sidekiq OSS), every successful one-person-company OSS author.

**Where it bites the wedge.** Not at all (consulting is paid per-engagement, decoupled from inference).

**Where it's defensible.** Framework-design.md §11.3 makes the case explicitly. The author's regulated-industry experience is the moat. The framework is the lead generator. The economics work for a solo maintainer with a full-time role because engagements are bounded, high-margin, and don't require an org.

**Pricing.** Reference points for regulated-industry consulting rates in 2026: $300–$700/hour for individual senior consultants; $50K–$500K per project for scoped engagements. A solo maintainer with deep domain credibility can credibly charge at the upper end of those ranges once one or two reference customers exist.

**Verdict: confirmed as primary seed monetization.** Framework-design §11.2 already names this; this analysis adds nothing to dispute it. It is the right starter SKU. The recommendation in §5 is to keep consulting as the revenue engine through Phase 5–6, then layer F + K + J on top once the framework has produced reference customers.

### N. Sponsorware / GitHub sponsors / foundation grants

**Pattern.** GitHub Sponsors, Open Collective, or foundation grant from an organization with aligned interest (Linux Foundation AI, MLCommons, an AI-governance foundation, or a bank's open-source-program-office). Provides supplementary income that doesn't require commercial sales motion. Closest exemplars: Curl (Daniel Stenberg, sponsored full-time), Caddy, Sentry's early days.

**Where it bites the wedge.** Not at all.

**Where it's defensible.** Realistic supplementary income for a project with clear public-interest framing (e.g., "open-source compliance primitives for regulated AI"). Foundation grants in particular are well-suited to compliance-oriented OSS infrastructure — the same buyers who would license compliance hosting (pattern K) often also fund the OSS infrastructure that supports it, because they benefit either way.

**Verdict: viable supplementary income, low ceiling but low effort.** Worth setting up GitHub Sponsors and an Open Collective once the project is publicly announced (framework-design.md §16, "after Phase 2 minimum"). Should not be relied on as primary income. Could become more meaningful if a specific industry foundation (e.g., a banking-industry open-source AI consortium, if one emerges) takes interest in the compliance primitives.

---

## §4 — Scoring Matrix

Six dimensions, each scored on a 1–5 scale where 5 is best alignment with the cost-wedge-preserving / solo-maintainer-compatible goal:

| # | Pattern | Wedge preservation | Margin floor | Solo-maintainer fit | Compliance leverage | Race-to-bottom resistance | Time-to-first-$ | **Total** |
|---|---|---|---|---|---|---|---|---|
| A | Per-token inference markup | 1 | 1 | 1 | 1 | 1 | 4 | **9** |
| B | Per-task pricing | 2 | 2 | 2 | 2 | 2 | 3 | **13** |
| C | Per-outcome pricing | 4 | 2 | 2 | 3 | 4 | 2 | **17** |
| D | Subscription w/ usage cap | 3 | 3 | 3 | 2 | 3 | 3 | **17** |
| E | Reserved capacity | 3 | 3 | 1 | 3 | 3 | 1 | **14** |
| F | **BYO-keys + orchestration fee** | **5** | **5** | **5** | **4** | **5** | **3** | **27** |
| G | Sidecar / router-only license | 4 | 4 | 5 | 3 | 4 | 2 | **22** |
| H | Open-core + hosted SaaS | 3 | 3 | 2 | 3 | 3 | 2 | **16** |
| I | Dual license AGPL + commercial | 4 | 4 | 4 | 3 | 4 | 2 | **21** |
| J | **Vertical packaging** | **5** | **5** | **4** | **5** | **5** | **2** | **26** |
| K | **Compliance/audit subscription** | **5** | **5** | **3** | **5** | **5** | **2** | **25** |
| L | Playbook marketplace + rev share | 5 | 3 | 3 | 3 | 4 | 1 | **19** |
| M | **Consulting + custom integration** | **5** | **4** | **5** | **5** | **5** | **5** | **29** |
| N | Sponsorware / grants | 5 | 2 | 5 | 4 | 4 | 3 | **23** |

The four highest-scoring patterns — **M (consulting), F (BYO-keys), J (vertical packaging), K (compliance subscription)** — form the recommended portfolio. They share three structural properties:

1. **None of them put revenue on the inference bill.** Each captures value in a layer that doesn't compete with cheap-inference economics. The cost wedge is preserved by structure, not by careful pricing.
2. **Each is compatible with the others.** A solo maintainer can offer consulting (M) and license vertical kits (J) and host compliance ledgers (K) and provide BYO-keys orchestration (F) without conflict — they target different layers of customer value.
3. **Each compounds with the architecture's other moats.** Consulting compounds with domain credibility; BYO-keys compounds with the wrap-Hermes design; vertical packaging compounds with the playbook curation primitive; compliance hosting compounds with the audit-ledger primitive.

The lowest-scoring patterns — A (per-token markup), B (per-task pricing as primary SKU), H (open-core SaaS priced per-token) — share the inverse property: they put Oscillitron's revenue on a line item that competes directly with frontier inference providers' price curves. They are not just suboptimal; they are *structurally fragile*. Any monetization plan that includes them as primary SKUs should expect to be repriced or repositioned every 12–18 months.

---

## §5 — Recommended Sequence

The framework-design.md roadmap (§14) provides the development phases. The monetization sequence below maps onto those phases. Each stage names what to add, what to *not* add, and the gating condition for moving to the next stage.

### Stage 0 — Now, through Phase 1 gate

**Monetize: nothing.**

**Rationale.** The Phase 1 kill-or-proceed gate is the determinant of whether monetization is even on the table. If the cost wedge doesn't survive empirical validation, every monetization conversation downstream is moot. Spending maintainer time on monetization design before this gate is overpriced.

**Allowable preparation work.** Get the license decision locked (pattern I depends on it). Set up GitHub Sponsors (pattern N requires zero infrastructure). Start collecting consulting-pipeline signals from the regulated-industry network (pattern M groundwork, no commitments).

### Stage 1 — Post-Phase 2 / 3, framework publicly announced

**Monetize: M (consulting only).**

**Rationale.** First-customer consulting engagement is the first revenue and the first reference customer. Framework needs to be visibly real (Phase 2 orchestrator, Phase 3 decomposition) before consulting can be credibly sold against it. Until Phase 4 lands the audit ledger and routing primitives, there's nothing compliance-grade to host (pattern K is premature) and no vertical kits to ship (pattern J is premature).

**Pricing for stage 1 consulting.** $400–$600/hour individual rate; $50K–$150K scoped project work. Conservative for a first engagement; ramps after first reference.

**Do not.** Do not announce a hosted offering. Do not pre-sell vertical kits. Do not stand up the marketplace. Do not start writing per-token-pricing pages on the website.

### Stage 2 — Post-Phase 4 / 5, audit ledger and recomposition shipped

**Monetize: M + F + K.**

**Rationale.** Phase 4 ships the audit ledger; this is the prerequisite for the compliance subscription. Phase 5 ships recomposition (tree merges); this is the architectural piece that makes managed-hosted Oscillitron a meaningfully better experience than self-hosted Oscillitron (the merge logic is the most operationally fiddly piece).

**Add: F (BYO-keys orchestration fee).** Launch the hosted offering — managed Hermes instances, managed routing, managed playbook curation. BYO-keys for inference. Pricing: $99–$299/month per developer seat (Stage 2 launch tier); $0.005–$0.010 per task overage. Aim for the GitHub Copilot adjacent-pricing band; the value layer is comparable.

**Add: K (managed audit ledger).** $25K/year per regulated-deployment baseline tier; up to $200K/year for high-volume + extended-retention tiers. Pair with consulting engagements where appropriate (consulting wins the customer, audit ledger retains them).

**Do not.** Still no marketplace (L). Still no vertical kits (J — Phase 7 minimum). Still no per-token pricing on the hosted offering.

### Stage 3 — Post-Phase 7, quality and calibration tooling

**Monetize: M + F + K + J.**

**Rationale.** Phase 7 ships the calibration tooling and quality eval harness. This is what enables credible vertical kits: a "finance-tron" kit can claim measurable quality on finance-domain benchmarks because the harness exists to measure it.

**Add: J (vertical packaging).** Start with one vertical kit, derived from the first consulting engagement that produced a domain-specific playbook set. Price as an annual license: $30K–$75K/year per deployment. The kit ships with: curated playbooks, curated retrieval index, curated routing topology, validated reproducibility manifests for regulatory mapping, white-labeled audit-ledger configuration. Each new consulting engagement potentially seeds a new vertical kit.

**Do not.** Still no marketplace.

### Stage 4 — Post-Phase 9, consulting practice mature

**Monetize: M + F + K + J + optionally L, optionally I, optionally N at scale.**

**Add: L (marketplace), if and only if there are ≥1,000 active deployments and ≥50 community-contributed playbooks already in circulation.** Marketplace as harvester of organic community activity, not as forced creation.

**Reconsider: I (dual-license commercial license sales) as a defensive measure if a large vendor forks Oscillitron and ships a closed competitor.** This is the classic "commercial license to capture the people who would otherwise reverse-engineer my code." Only relevant if the project is successful enough to warrant cloning.

**Reconsider: N (foundation grants) at scale — pursue grant funding from an aligned foundation if doing so frees maintainer time to deepen the compliance moat.**

### Anti-stages — never do these

- Do not launch a per-token-priced hosted offering at any stage.
- Do not cripple the OSS core. Framework-design §11.3 is correct and this analysis reinforces it: the OSS core is the lead generator, kneecapping it breaks every downstream monetization vehicle.
- Do not start a marketplace before there is organic playbook-sharing activity to harvest.
- Do not under-invest in the audit ledger — it is the cornerstone of pattern K, which is the highest-margin SKU in the portfolio.

---

## §6 — Anti-Patterns (Explicit)

These deserve standalone callouts because they are tempting and visible in adjacent products, and each has an attached "but…" that this analysis rejects.

### "Charge a small markup per token — most users won't notice."

They will. Token-priced AI services in 2025–2026 are commodity infrastructure; users compare per-million-token rates the way they compare per-GB cloud storage rates. Oscillitron's wedge is *visible* — that's the whole point — and competing on per-token rates with the providers Oscillitron sits on top of is structurally losing.

### "Open-source the orchestrator but require a commercial license for production deployments."

This is the most subtle and most damaging anti-pattern: it looks like a reasonable open-core split but it actually corrupts the OSS core. The user can't run the OSS for what they need it for; they're trialing it on the way to a paid SKU. Adoption stalls. Framework-design §11.3 explicitly rejects this and is correct.

### "Bundle inference and orchestration into one hosted price for simplicity."

Tempting because it simplifies the sales conversation. Destructive because (a) it puts Oscillitron on the inference bill (every problem from pattern A), (b) it removes the BYO-keys data-residency story (every problem for regulated buyers), and (c) it makes Oscillitron look like an inference reseller rather than a value layer.

### "Charge for compliance features only after a customer hits an audit; let them onboard for free."

Tempting because it lowers acquisition friction. Bad because the compliance features are the most expensive ones to build, the audit ledger has cost-of-storage that compounds with deployment age, and the buyer who waits for an audit before paying often waits forever. Compliance features should be either always-priced or never-shipped, not conditionally-priced.

### "Match frontier API pricing for the hosted offering — undercut just enough."

This is the "lowest-margin operator forever" trap. It is a calling card for the consulting business, not a business in itself. If Oscillitron is consistently the cheapest endpoint, it has no margin, and the only way to grow margin is to either degrade the user experience or raise prices — both of which break the cost-wedge promise.

### "Defer the license decision until the project takes off."

Apache 2.0 vs. AGPL is a real decision that determines whether pattern I (dual-license commercial) is even available. Picking late means defaulting to whatever happens to be on the GitHub template, which is typically MIT or Apache 2.0 — both permissive, both foreclose I. The decision should be made consciously before public announcement.

---

## §7 — Open Questions That Block Monetization Sequencing

These are not "consider later"; they are "lock before Stage 1." Each has a default that is consistent with the rest of this analysis, but the user is the one who picks.

1. **Solo-with-full-time-job — durable posture, or 1–2 year off-ramp possible?**
   - If durable: Stage 4+ patterns J (vertical packaging) and L (marketplace) need to be designed to run with minimal maintenance overhead. Don't promise customer success an SLA you can't deliver while holding the day job.
   - If off-ramp possible: J and L can be more ambitious because there will eventually be more maintainer time.
   - **Default for this analysis: durable.** Sequencing in §5 is built around the solo-with-day-job constraint.

2. **License — Apache 2.0 (framework-design recommendation) or AGPL (this analysis's dual-license enabler)?**
   - Apache 2.0: foreclose pattern I; lean entirely on M + F + J + K.
   - AGPL: enable I as a defensive option; accept some adoption friction from companies that refuse AGPL.
   - **Default for this analysis: AGPL.** The dual-license optionality is worth the adoption friction given how strong the patterns are even without I; AGPL preserves the option to drop in commercial licenses later. But this is a real disagreement with framework-design.md §11.1 and Jim should pick consciously.

3. **Is the financial-services-domain credibility actually closable, or aspirational?**
   - If real: consulting (M) ramps fast; pattern K compliance subscription gets first customers quickly.
   - If aspirational: M may take 12-18 months to produce first revenue; sequencing slips.
   - **Default for this analysis: real.** Framework-design §1.3 names it explicitly. Sequencing in §5 assumes it. If it's actually aspirational, slow the consulting ramp and lean harder on F (BYO-keys, no sales motion) as the first-revenue vehicle.

4. **Hosted infrastructure — does Jim want to be on call for it?**
   - If yes: pattern F (BYO-keys hosted) and pattern K (managed audit ledger) ramp normally.
   - If no: pattern F can be replaced with G (sidecar/component license, customer self-hosts), and pattern K becomes a software product the customer hosts themselves with a paid update channel.
   - **Default for this analysis: no, prefer to avoid ops on-call.** The §5 sequence as written assumes some hosting; an even-more-conservative variant skips hosting entirely and relies on G + J + K-as-software + M.

5. **Naming, public announcement timing, and reference-customer pipeline.**
   - Framework-design §16 names "after Phase 2 minimum" as the public-announcement gate. Monetization sequence in §5 ties Stage 1 to this. If announcement slips, monetization slips. If announcement accelerates without Phase 2 substance, Stage 1 consulting credibility collapses. **The two are coupled.**

---

## §8 — Appendix: Assumption Sheet

Every numeric assumption in §2 stated explicitly, labeled, and challengeable. Each has a sensitivity estimate (how much the §5 recommendation changes if the assumption is wrong by ±50%).

| # | Assumption | Value | Source | Sensitivity |
|---|---|---|---|---|
| A1 | Frontier flagship $/M input tokens, May 2026 | $5.00 | Extrapolated from May 2025 Opus 3 ($15) with ~50% YoY decline | Medium — used in §2.1 baseline; ±50% shifts headroom but doesn't change pattern ranking |
| A2 | Frontier flagship $/M output tokens, May 2026 | $25.00 | Extrapolated from May 2025 Opus 3 ($75) | Medium |
| A3 | Frontier mid-tier $/M input tokens, May 2026 | $1.50 | Extrapolated from Sonnet 3.7 ($3) | High — this is the primary baseline |
| A4 | Frontier mid-tier $/M output tokens, May 2026 | $7.50 | Extrapolated from Sonnet 3.7 ($15) | High |
| A5 | Cheap-tier $/M tokens, May 2026 (blended in/out) | $0.50 | Extrapolated from 2025 Together/Groq Llama 70B rates of ~$0.60–0.90, with continued decline | High — this is the wedge denominator |
| A6 | Reference task input tokens | 2,000 | Heuristic; "medium-complexity reasoning task" | Low — scales all numbers proportionally |
| A7 | Reference task output tokens | 2,000 | Heuristic; same | Low |
| A8 | Specialist chain depth | 2 | From skeleton's `code-analyst → fact-check → writer` minus the writer (counts as final output) | Medium — 3 specialists would push token volume up 50% |
| A9 | AP/summary handoff size | 500 tokens | From design-notes.md "small structured envelope wrapping a freeform body" | Low |
| A10 | Verifier loop tokens | 4,500 in / 100 out | Verifier sees the full chain output | Medium |
| A11 | Audit sample rate | 10% | Heuristic; framework-design references "periodic strong-model audit on a sample" | Medium — 25% would erode 4% of margin |
| A12 | Audit sample $/task @ flagship | $0.04 | From A1/A2 applied to a 2K/2K task | Low |
| A13 | Orchestration compute/storage/network per task | $0.002 | Heuristic; assumes economy of scale at ≥10K tasks/day; will be much higher at lower volume | High — at 100 tasks/day this is more like $0.02/task |
| A14 | "≥3× wedge needed for switching to feel rational" | empirical heuristic | Survey of compound-AI vendor adoption studies; not a hard threshold | Medium — used in §1.1 for framing |
| A15 | Stage 1 consulting hourly rate | $400–$600 | Reference: regulated-industry senior consulting bands in 2026 | Low — affects revenue projections, not pattern selection |
| A16 | Stage 2 hosted SaaS per-seat pricing | $99–$299/month | Reference: GitHub Copilot Business ($19/seat) → enterprise developer tools ($300+/seat) | Low |
| A17 | Stage 2 managed audit ledger pricing | $25K–$200K/year | Reference: compliance-tooling annual contracts in 2026 | Medium — high band assumes regulated-financial-services customers |
| A18 | Stage 3 vertical kit pricing | $30K–$75K/year | Reference: domain-specific AI/compliance tool annual licenses | Medium |

The most fragile assumptions are A3/A4 (frontier mid-tier pricing — primary baseline) and A5 (cheap-tier blended rate — wedge denominator) and A13 (orchestration overhead — directly destroys margin if underestimated). The §5 recommendation is robust to ±50% on each *individually*, but a compound miss (frontier prices drop faster AND cheap-tier prices hold flat AND orchestration overhead is higher than modeled) could reduce the wedge from 3× to ~1.3×, at which point the BYO-keys recommendation (pattern F) becomes structurally critical because it doesn't depend on the wedge for Oscillitron's revenue.

---

## §9 — One-Paragraph Versions for Each Audience

**For Jim (working analysis).** Don't put Oscillitron on the inference bill. The cost wedge is the user-side promise and any per-token-or-per-task monetization narrows it on a price curve that keeps falling. Consulting is the right first revenue (framework-design.md was already right about this) and should stay the engine through Phase 5 or 6. BYO-keys + flat orchestration fee, vertical packaging, and managed audit-ledger hosting are the three layers that capture meaningful margin *off* the inference bill. Lock the license decision before Stage 1 — Apache 2.0 closes the dual-license option, AGPL keeps it open with adoption friction the regulated-industry audience can absorb. The wedge is real today at ~3×, narrow enough to need monetization patterns that don't depend on it, and likely to compress further in 12-18 months.

**For investors / partners.** Oscillitron is an orchestration layer above commodity inference, not an inference reseller. Revenue accrues from three structurally durable layers: vertical-domain packaging (curated specialist kits for regulated industries, annual contract licenses), managed compliance infrastructure (audit-ledger hosting with regulator-friendly retention guarantees), and consulting on regulated-industry deployments. None of these revenue lines compete with falling frontier API prices; all three compound with the architecture's specialization moat. Unit-economics modeling supports ~$0.013/task delivery cost against a $0.04/task frontier-equivalent baseline, but the headline number is the moat: the audit-ledger, classification-aware routing, and reproducibility primitives serve a regulated-buyer segment that frontier APIs structurally cannot serve at any price.

**For collaborators / contributors.** The OSS core stays open, permissively (Apache 2.0) or strongly (AGPL) — that decision is pending and we'll lock it before public announcement. The framework remains genuinely useful standalone; commercial offerings are additive (consulting, vertical kits, managed compliance hosting), never restrictions on the core. Contributors don't have to worry about being out-competed by a "premium edition" that holds back features; the business model is structured so adoption *is* the success metric, and contributor work compounds with that. The financial sustainability story is hosted-services and consulting on top of the OSS substrate, modeled on Sidekiq/Caddy/Sentry-early-days, not on the open-core-bait-and-switch model that has burned other communities.

---

---

## §10 — Bootstrap Posture and Investor Optionality (supersedes §5's phase-gated framing)

**Added after v1 review.** §5 reads as a phase-gated rollout schedule — Stage 1 at Phase 2, Stage 2 at Phase 4, Stage 3 at Phase 7. The actual operating posture is meaningfully different and worth being precise about, because the difference matters for *what to instrument during the consulting phase* and *what credible investor narrative the project sets up later*.

### 10.1 The posture, stated plainly

- **No or minimal capital at start.** Bootstrap. The framework runs on nights-and-weekends maintainer time on top of Jim's L6 role; no funding round is needed and none is being pursued at start.
- **Consulting is the default monetization, not the first stage of a planned expansion.** Pattern M from §3 is the business unless something pulls otherwise. The framework is the lead generator, the consulting engagements are the revenue, the regulated-industry domain credibility (framework-design.md §1.3) is the moat. This works indefinitely as a one-person business at the right pace.
- **Patterns F (BYO-keys + orchestration fee), J (vertical packaging), and K (managed compliance/audit hosting) are picked up *opportunistically*, gated on market appetite signal — not on the development-phase calendar.** §5's stages should be re-read as "available patterns once the architecture supports them," not as "stages to ramp into on schedule." The phase calendar is necessary (you can't host an audit ledger before Phase 4 ships it) but not sufficient (the audit ledger being shipped doesn't mean Oscillitron should launch K).
- **The opportunistic expansion is also the investor-optionality play.** If F/J/K pick up traction, the project moves from "credible solo consulting business" to "validated demand for compliance-tooling SaaS on top of a cost-wedge architecture" — which is a fundable narrative if Jim wants to raise. If F/J/K don't pick up, the consulting business is unaffected and the project keeps shipping. Asymmetric upside, bounded downside.

### 10.2 Consulting as demand-signal capture

This is the most important operational change implied by the posture: **consulting engagements should be selected and instrumented partly as cheap market research.** Each engagement reveals which of F/J/K has real customer pull. Specifically, watch for:

- **Pattern F signal — "Can you just host this for us?"** Two or more consulting customers asking for managed Oscillitron in the same quarter is the trigger to spin up the hosted offering. One customer is an anecdote; two is a category-of-ask.
- **Pattern J signal — vertical repeatability.** When a consulting engagement produces a curated playbook set, retrieval index, and routing topology that a second customer in the same vertical asks to use, the vertical kit is real. Don't pre-build vertical kits on speculation; let a second-buyer-in-same-vertical signal force the productization.
- **Pattern K signal — compliance hosting as a separate line item.** When a regulated customer asks for managed audit-ledger hosting as a discrete subscription distinct from consulting (rather than bundled into a consulting deliverable), the managed-compliance market is signaling. Single signal can trigger here because regulated buyers are rare and each one is meaningful.

Consulting engagements should be priced, scoped, and contracted in a way that *preserves* the IP needed to spin these up later: don't sign work-for-hire contracts that give the client exclusive rights to a vertical kit you might later want to license to other customers in the same vertical. This is a contract-mechanics point but it has direct monetization-strategy consequences.

### 10.3 Investor-optionality framing

If the appetite signals do appear and Jim chooses to pursue capital, the narrative the consulting + early F/J/K period establishes is:

> *"We have N regulated-industry customers paying for compound-AI deployments built on an open-source orchestration framework with structurally defensible cost economics. Of those, M customers have asked for managed compliance hosting as a separate line item, and we've signed K of them to annualized contracts. The architecture wraps commodity inference rather than competing with it, so margin is decoupled from frontier API price curves. We're raising to scale the managed offerings."*

This is a much stronger fundraise position than "we're building an AI framework and need capital to launch." It is *validated-demand-led* rather than *capability-led*. Investors price the former substantially higher because the de-risking has already happened.

The architectural decisions already locked (wrap-Hermes, BYO-keys-compatible, audit-ledger-first-class) are the ones that make this narrative credible. Decisions that would undermine it — pivoting to a per-token inference resale model, bundling inference into the hosted offering, crippling the OSS core — are not just bad monetization choices in their own right (per §6), they're also fundraise-narrative-destroying choices. The bootstrap posture and the investor-optionality posture want the same things, which is the strongest possible evidence the strategy is internally coherent.

### 10.4 What this changes about §5

Re-read §5 as a *catalog of available patterns ranked by structural fit and ordered by the earliest phase at which each is technically possible* — not as a rollout schedule. The phase numbers indicate when each pattern *becomes available*, not when it *should be launched*. Launches are signal-gated per §10.2. Consulting (Stage 1 in §5) is the only pattern that is both available early and launched on a schedule rather than on signal; everything past it waits.

### 10.5 What this doesn't change

The pattern selection itself, the unit-economics, the anti-patterns in §6, the assumption sheet in §8, and the recommendation against per-token inference resale are all unchanged. §10 is a posture overlay on §5's mechanics, not a replacement for the underlying analysis. The architectural and pricing-model reasoning still binds; only the *timing and triggers* for non-consulting patterns shift from calendar-gated to signal-gated.

---

---

## §11 — Self-Hosted Inference: Capex, Hosting, and Fixed-Cost Economics

**Added after v1 review.** §2 modeled Oscillitron's cost stack against cheap-API rates only ($0.50/M tokens, Together/Groq/Fireworks class). That's the right baseline for non-regulated users who can route through commercial API providers, but it omits the entire self-hosted inference layer that framework-design.md §8 makes a first-class deployment target. This section adds the self-hosted model and surfaces three findings — one of them genuinely surprising.

The three findings up front, before the arithmetic:

1. **Self-hosted is cheaper than frontier API, but more expensive than cheap-API at realistic utilization.** Across every hardware tier, owned/rented GPUs cost $1–$10 per million tokens at 30% utilization — beating frontier API rates by 2–5× but losing to cheap-API rates by 2–20×. The cheap-API providers operate at near-100% batch utilization with thousands of customers' workloads multiplexed; an individual customer can almost never replicate that. **The intuition "self-hosting is always cheaper" is wrong if the alternative is a cheap-API provider, not a frontier API.**
2. **Regulated buyers can't use cheap-API providers**, so for them the comparison is entirely different — self-hosted vs. frontier-enterprise (~$8–15/M tokens with data residency) vs. nothing. Oscillitron on self-hosted hardware wins decisively in that comparison. This is the strongest possible reinforcement of pattern K (managed compliance/audit hosting) from §3: the segment that *forces* self-hosting is also the segment that values the compliance moat most.
3. **Oscillitron's high token-volume multiplier interacts non-obviously with self-hosted economics.** It's a cost drag on API-rate users (3.55× more tokens = 3.55× the bill at flat per-token rates), but a *utilization feature* for self-hosted users (3.55× more tokens fills the GPU, dropping per-M-token amortized cost). Pair that with the fact that Oscillitron runs smaller, faster models (which throughput-on-fixed-hardware better than one big frontier-class model), and self-hosted Oscillitron is approximately cost-equivalent to self-hosted-frontier on the same hardware *with much better latency and quality-per-watt.* This is a meaningful architectural advantage that wasn't visible in §2.

### 11.1 The four hosting tiers (per framework-design.md §8) with cost models

All numbers are stated assumptions; full assumption sheet additions in §11.7. Hardware throughput figures assume 70B-class Q4-quantized model (Llama 3.3 70B / Qwen 2.5 72B) — Oscillitron's working-cheap-model class.

**Tier 1 — Compact Personal**

| Hardware | Capex | 3yr amort/yr | Power $/yr @ $0.12/kWh | Throughput (tok/s) | Total $/yr |
|---|---|---|---|---|---|
| DGX Spark (single) | $3,000 | $1,000 | $158 | 40 | $1,158 |
| DGX Spark (paired) | $6,000 | $2,000 | $315 | 80 | $2,315 |
| Mac Studio M4 Ultra (192GB) | $6,500 | $2,167 | $210 | 35 | $2,377 |

**Tier 2 — Workstation/Lab**

| Hardware | Capex | 3yr amort/yr | Power $/yr | Throughput (tok/s) | Total $/yr |
|---|---|---|---|---|---|
| Used A100 80GB (single) | $10,000 | $3,333 | $368 | 110 | $3,701 |
| Used A100 80GB (4-node) | $40,000 | $13,333 | $1,472 | 440 | $14,805 |
| AMD MI300X (single, new) | $15,000 | $5,000 | $420 | 130 | $5,420 |

**Tier 3 — Cloud GPU Rental** (no capex; reserved or on-demand)

| Setup | Hourly | Annual @ 24/7 | Throughput | — |
|---|---|---|---|---|
| RunPod H100 (community/reserved) | $2.50 | $21,900 | 150 | — |
| AWS H100 reserved (per-H100) | $7.00 | $61,320 | 150 | — |
| AWS H100 on-demand (per-H100) | $12.00 | $105,120 | 150 | — |

**Tier 4 — On-Prem Regulated Cluster**

| Setup | Capex (4yr amort/yr) | Infra (4yr amort/yr) | Power $/yr | Throughput (tok/s) | Total $/yr |
|---|---|---|---|---|---|
| 8x H100 DGX node | $75,000 | $15,000 | $11,038 | 1,200 | $101,038 |

### 11.2 Cost per million tokens, by tier and utilization

The key table. Each cell is amortized cost per million tokens at the stated annual utilization (% of max throughput actually used).

| Hardware | @10% util | @30% util | @50% util | @100% util |
|---|---|---|---|---|
| DGX Spark single | $9.18 | $3.06 | $1.84 | $0.92 |
| DGX Spark paired | $9.18 | $3.06 | $1.84 | $0.92 |
| Mac Studio M4 Ultra | $21.53 | $7.18 | $4.31 | $2.15 |
| Used A100 80GB | $10.67 | $3.56 | $2.13 | $1.07 |
| AMD MI300X | $13.22 | $4.41 | $2.64 | $1.32 |
| RunPod H100 reserved | $46.30 | $15.43 | $9.26 | $4.63 |
| AWS H100 reserved | $129.63 | $43.21 | $25.93 | $12.96 |
| AWS H100 on-demand | $222.22 | $74.07 | $44.44 | $22.22 |
| 8x H100 DGX node (Tier 4) | $26.70 | $8.90 | $5.34 | $2.67 |

Benchmark rows for comparison:

| Reference | $/M tokens |
|---|---|
| Cheap-API blended (Together/Groq/Fireworks) | **$0.50** |
| Frontier mid-tier blended (Sonnet-class, 50/50 in/out) | **$4.50** |
| Frontier flagship blended (Opus/GPT-5-class, 50/50 in/out) | **$15.00** |
| Frontier enterprise with data residency (estimated) | **$8–$15** |

### 11.3 The break-even surprise: self-hosted *cannot* beat cheap-API

At the cheap-API rate of $0.50/M tokens, the break-even volumes for self-hosted to win on cost:

| Hardware | Break-even annual M tokens | Required utilization | Achievable? |
|---|---|---|---|
| DGX Spark single | 2,315 | 184% | **No** (exceeds 100% of capacity) |
| Mac Studio M4 Ultra | 4,754 | 431% | **No** |
| Used A100 80GB | 7,402 | 213% | **No** |
| RunPod H100 reserved | 43,800 | 926% | **No** |
| AWS H100 reserved | 122,640 | 2,593% | **No** |
| 8x H100 DGX node | 202,075 | 534% | **No** |

**Read literally: no self-hosted hardware tier can beat cheap-API pricing for an individual user at any achievable utilization.** Cheap-API providers operate at scale with batch-multiplexed workloads that achieve near-100% effective utilization through bin-packing across thousands of customers; a single customer running their own GPU never gets there. This finding contradicts the natural intuition that "owning the metal must be cheaper" and is worth internalizing — for non-regulated users, **routing through a cheap-API provider is strictly better than self-hosting on cost alone, full stop.**

Break-even vs. frontier rates is much more favorable, however. Against frontier mid-tier ($4.50/M blended):

| Hardware | Break-even annual M tokens | Required utilization |
|---|---|---|
| DGX Spark single | 257 | 20% |
| Used A100 80GB | 822 | 24% |
| 8x H100 DGX node | 22,453 | 59% |

So self-hosted *does* beat frontier-mid-tier at modest, achievable utilization — and beats frontier flagship at very low utilization. This is the "self-hosting is cheaper than frontier" intuition correctly applied. It just isn't cheaper than the *cheap-API tier*, which is the relevant baseline for non-regulated users.

### 11.4 The regulated-buyer asymmetry — cheap-API isn't on the menu

For regulated buyers (banking SR 11-7, HIPAA, FedRAMP, GDPR data residency), the cheap-API tier is structurally unavailable: Together, Groq, Fireworks, and equivalents don't generally offer the data residency, audit primitives, BAA/DPA coverage, or air-gapped deployment required. The relevant alternatives narrow to:

1. **Self-hosted** (Tier 1 or Tier 4 from §11.1) — feasible, cost per §11.2
2. **Frontier-enterprise with data residency** — Anthropic Enterprise, OpenAI Enterprise, Azure GovCloud variants. Pricing estimated at $8–$15/M tokens blended (≈2–3× standard frontier rates). Available in some regulated jurisdictions, unavailable in others (e.g., certain banking workloads, classified government work).
3. **Nothing.** A substantial share of regulated AI projects today is in this bucket — the workload exists but no compliant inference path does.

Against this set of alternatives, self-hosted Oscillitron at $2–5/M tokens on Tier 1/2 hardware wins decisively on cost. Against frontier-enterprise at $8–15/M tokens, the wedge is **2–7×** — meaningful but narrower than the 9–30× wedge against frontier-flagship on the non-regulated comparison. Against "nothing," it's infinite.

**This reframes the whole monetization picture for the regulated segment.** The wedge isn't a 1.4× sliver against pure mid-tier (the uncomfortable finding in §2.3); it's a 2–7× wedge against the only-real-alternative for regulated buyers, plus the compliance primitives, plus the architecture's specialization moat. The compliance subscription (pattern K) and vertical packaging (pattern J) prices in §3 are well-supported by this revised baseline — at $25K–$200K/year for compliance hosting, Oscillitron is capturing a fraction of the gap between frontier-enterprise and self-hosted-bare, and delivering compliance primitives the frontier-enterprise alternative doesn't ship.

### 11.5 The token-volume multiplier ↔ hardware-utilization interaction

This is the §11 finding that wasn't visible from §2 at all.

Oscillitron's architecture uses ~3.55× more tokens per task than a single frontier-call would (§2.3). At cheap-API rates this is a **cost drag**: the per-token wedge has to overcome it to deliver net savings, which it does, narrowly. At per-token-billed cloud GPU rental (Tier 3) it is also a cost drag.

But on **owned or amortized self-hosted hardware (Tier 1, 2, 4)** the relationship inverts. The hardware costs the same whether it's idle or at 100% utilization. Anything that drives utilization up *decreases* the per-token amortized cost.

Concrete worked example. A user with a single DGX Spark ($1,158/year). They run 100 reference tasks/day (~36,500/year).

| Architecture | Tokens/year | Utilization | $/M tokens @ that util | Cost per task |
|---|---|---|---|---|
| Single frontier-class call (4K tokens) | 146M | 11.6% | $7.92 | $0.032 |
| Oscillitron (14.2K tokens) | 518M | 41% | $2.24 | $0.032 |

The arithmetic comes out to approximately the same cost per task — Oscillitron's 3.55× token-volume multiplier almost exactly cancels its 3.5× utilization boost. The architecture's "inefficiency" *vanishes* on owned hardware. **At higher base workloads it can flip in Oscillitron's favor**, because the utilization curve flattens (you're already near full util) while the token volume keeps adding capacity headroom benefits.

There's an even better story when you factor in **which model runs on the same hardware.** A DGX Spark with 128GB unified memory can host a 235B-class model (single big frontier-equivalent) at ~15 tok/s, or a 70B-class cheap model at ~40 tok/s. Oscillitron uses the smaller models. So on the same hardware, the *wall-clock throughput* of Oscillitron's stack is roughly equal to single-big-model-frontier-equivalent: 14.2K tokens at 40 tok/s ≈ 355 seconds per task; 4K tokens at 15 tok/s ≈ 267 seconds per task. Comparable order of magnitude, with Oscillitron's quality story (specialized playbooks, audit primitives, organic specialization) ridingon top.

For self-hosted users, this means: **Oscillitron is not a tax on hardware — it is a way to use cheaper, smaller models without sacrificing wall-clock throughput, while gaining the compliance and specialization features.** This is a much stronger positioning than §2 alone supports.

### 11.6 What §11 changes for the monetization recommendations

The §5/§10 monetization sequence and the §4 pattern scoring **do not change.** The directionally-correct conclusions hold: don't put revenue on the inference bill (whether API or hardware), capture value in the orchestration/compliance/vertical layers, lead with consulting, expand on signal. But §11 sharpens three specific points:

1. **Pattern F (BYO-keys + orchestration fee) covers self-hosted users naturally.** The framing "user brings their own inference" works identically whether they bring an API key for Together or a vLLM endpoint pointed at a DGX Spark. Pricing models in §3.F don't need to change. If anything, self-hosted users are stickier (they've sunk capex into matching infrastructure to Oscillitron's preferred model size).
2. **Pattern K (managed compliance hosting) is structurally stronger than §3 implied**, because the regulated buyer baseline isn't "frontier API" — it's "self-hosted + DIY compliance" or "nothing." Oscillitron-managed compliance hosting on top of customer self-hosted inference targets exactly this segment. Pricing toward the upper end of the $25K–$200K/year band is defensible.
3. **Consulting (pattern M) on regulated-industry deployments has a hardware-advisory leg that was implicit in framework-design.md §8 but worth surfacing explicitly.** Helping a regulated customer pick Tier 1 vs. Tier 2 vs. Tier 4 hardware, calibrate model quantization for their workload, and validate cost projections against the §11.2 tables is real consulting work that complements the compliance-architecture work. A bank exploring on-prem AI doesn't know whether DGX Spark, used A100 nodes, or a full 8x H100 DGX is right for them; Oscillitron's author does. Bill for this.

### 11.7 Assumption sheet additions for §11

| # | Assumption | Value | Sensitivity |
|---|---|---|---|
| A19 | DGX Spark 2026 price | $3,000 | Low — NVIDIA Project DIGITS announced at this MSRP |
| A20 | DGX Spark throughput on 70B Q4 | 40 tok/s | Medium — depends on quantization and batch size; reported numbers vary 30-60 |
| A21 | Mac Studio M4 Ultra 192GB price | $6,500 | Low |
| A22 | Mac Studio throughput on 70B Q4 | 35 tok/s | Medium |
| A23 | Used A100 80GB price (2026) | $10,000 | High — secondary market prices vary widely with crypto/AI cycles |
| A24 | A100 80GB throughput on 70B Q4 | 110 tok/s | Medium — well-documented benchmarks |
| A25 | AWS H100 reserved hourly | $7.00/hr | Low — AWS published rates |
| A26 | 8x H100 DGX node capex | $300,000 | Medium |
| A27 | On-prem infra (rack, cool, network, security) | $60,000 over 4yr | Medium — varies enormously by org maturity |
| A28 | Power cost | $0.12/kWh | Low for US commercial; regulated buyers often pay $0.15–$0.25 |
| A29 | Hardware amortization period | 3yr (Tier 1-2), 4yr (Tier 4) | Medium — conservative; longer amortization improves all tier numbers proportionally |
| A30 | Frontier-enterprise with data residency $/M | $8–$15 | High — estimated; actual contracts vary widely and many regulated jurisdictions have no published pricing |
| A31 | "Realistic" utilization for self-hosted | 30% | High — single-user workloads typically 5–30%; multi-tenant or batch-heavy 30–70% |

The most fragile of these is A31 (realistic utilization). At 10% utilization, every self-hosted tier is several times more expensive than at 30%. The §11.5 architectural advantage depends on Oscillitron's token-volume multiplier driving real utilization gains — if a customer's workload is too small to reach 20–30% utilization with Oscillitron either, the self-hosted path is bad regardless of architecture.

A30 (frontier-enterprise pricing with residency) is the second-most-fragile because it underpins the regulated-buyer wedge calculation in §11.4. Actual frontier-enterprise pricing for regulated workloads is opaque; the $8–$15/M estimate is conservative-to-realistic but could be wrong by 50% in either direction. The §11.4 conclusion (regulated buyers' alternatives are scarce and expensive) holds qualitatively under any plausible value, but the precise wedge multiple is uncertain.

---

---

## §12 — Quality-Matching and the Cost-Quality Tension

**Added after v1 review.** §2's cost model assumed Oscillitron's standard stack (router + 2 specialists + verifier + audit sample) and compared it to a frontier mid-tier call as if the quality on both sides were equivalent. The model didn't ask the harder question: *what does it cost to actually rival frontier quality with cheap base models?* The honest answer is that quality-matching requires a heavier stack — more parallel specialists, a tree-merge recomposition step, deeper verification, higher audit sampling, occasional flagship escalation for high-conflict merges. **That heavier stack costs nearly as much as a flagship call.** The "cheaper than frontier" wedge and the "frontier-quality matching" claim are partially in tension, and the honest monetization story is workload-dependent in a way §2 didn't reveal.

### 12.1 The three execution tiers

The architecture supports — and should explicitly model — at least three depths of execution. Each is a different stack, with different cost and quality profile. The router (§3, the rule-based skeleton) should select between them.

**Tier A — Light stack (easy tasks).** Router + single specialist + lightweight verifier. ~5,600 tokens. The cheap-model layer is enough; no merging, no audit sample, no escalation. Examples: simple code completion, structured-data extraction with low ambiguity, single-domain factual lookup.

**Tier B — Standard stack (medium tasks).** The §2.2 model. Router + 2 specialists + AP handoff + verifier + 10% audit sample. ~14,200 tokens. Examples: multi-step reasoning that benefits from handoff between two specialists, code review with reasoning + verification, document analysis with grounded checks.

**Tier C — Heavy stack (frontier-quality-matching tasks).** Router + 3 parallel specialists (Branch-Solve-Merge style) + 3 AP handoffs + tree-merge recomposition + deep verifier with grounded checks + 5% inhibition restart expected + 25% audit sample + 10% flagship escalation for high-conflict merges. ~39,000 tokens cheap-tier plus flagship escalation cost. Examples: complex synthesis across multiple sources, high-stakes reasoning where output quality is the deliverable (legal analysis, financial modeling, medical decision support), tasks where frontier-flagship would have been the alternative.

### 12.2 Per-task cost by tier

Under §2.1 and §11 pricing assumptions:

| Tier | Tokens | Cheap inference | Audit sampling | Flagship escalation | Overhead | **Total per task** |
|---|---|---|---|---|---|---|
| A | 5,600 | $0.0028 | — | — | $0.0010 | **$0.0038** |
| B | 14,200 | $0.0071 | $0.0040 (10% × $0.04) | — | $0.0020 | **$0.0131** |
| C | 39,000 | $0.0195 | $0.0100 (25% × $0.04) | $0.0070 (10% × $0.07) | $0.0030 | **$0.0395** |

### 12.3 Wedge by tier — and the brutal Tier C finding

Against frontier reference costs ($0.018 mid-tier, $0.040 mid-point mix, $0.060 flagship):

| Tier | Cost | vs mid-tier | vs mid-point | vs flagship |
|---|---|---|---|---|
| A | $0.0038 | **4.7×** | **10.3×** | **15.8×** |
| B | $0.0131 | **1.4×** | **3.0×** | **4.6×** |
| C | $0.0395 | **0.46× (LOSES)** | **0.99× (parity)** | **1.5×** |

**Tier C is the honest answer to "what does it cost to rival frontier quality."** It's $0.0395/task — almost identical to the $0.040 frontier mid-point mix and *more expensive than frontier mid-tier alone.* The wedge against flagship is only 1.52×, which delivers 34% user savings — real but not transformative. **The "fraction of frontier cost" promise does not survive Tier C execution against a mid-tier baseline.** It survives only against flagship-quality demands.

This is the most uncomfortable finding in the analysis so far. The orchestration architecture genuinely can match frontier-flagship quality with cheap base models — that's the engineering case for the design — but it does so by spending almost as many dollars as the flagship call would have cost. The wedge isn't in the per-call cost; it's in the *flexibility to spend less on tasks that don't need flagship quality.*

### 12.4 Workload-mix weighted economics

A real deployment isn't all Tier C. Most production workloads are a mix. Weighted average cost depends entirely on the A/B/C distribution:

| Workload profile | Mix (A/B/C) | Weighted $/task | Wedge vs mid-tier | Wedge vs mid-point | Wedge vs flagship |
|---|---|---|---|---|---|
| Commodity-heavy | 70/25/5 | $0.0079 | 2.3× | 4.9× | 7.6× |
| Balanced | 50/35/15 | $0.0124 | 1.5× | 3.1× | 4.8× |
| Quality-sensitive | 30/40/30 | $0.0182 | **parity** | 2.1× | 3.3× |
| Frontier-quality-everywhere | 5/15/80 | $0.0338 | 0.5× (loses) | 1.2× | 1.8× |

Read the rows: a customer with a commodity-heavy workload sees a 2.3× wedge against mid-tier (better than §2.2's 1.4×); a customer with a frontier-quality-everywhere workload sees *no* wedge against mid-tier and a narrow 1.8× wedge against flagship.

**This is the workload-mix dependency that §2 hid.** The right monetization story isn't "Oscillitron costs $0.013 per task." It's "Oscillitron costs whatever the workload demands, and the customer's wedge depends on what they actually need." A buyer with a quality-sensitive workload buying because they want frontier-flagship quality at a fraction of the price is paying ~$0.018/task and saving 3.3× against flagship — meaningful but bounded. A buyer with a commodity-heavy workload buying because they want cost savings is paying ~$0.008/task and saving 7.6× — much more dramatic but on a different value claim.

### 12.5 The three claims can't all be 100% simultaneously

Oscillitron has historically pitched three concurrent value claims:

1. **Cheaper than frontier** (the cost wedge)
2. **Rivals frontier quality** (the architectural insight from framework-design.md §4)
3. **Compliance and audit primitives** (the moat for regulated buyers)

§12 makes explicit that **(1) and (2) trade off against each other**. You can have a wide cost wedge on commodity workloads where quality isn't the differentiator (Tier A/B execution), or you can match frontier-flagship quality with a narrow cost wedge (Tier C execution), but not both simultaneously on the same task. Claim (3) is independent of both and doesn't trade off; it stacks regardless of tier choice.

The honest positioning is therefore:

> *On commodity and standard workloads, Oscillitron delivers 1.5–8× cost savings against frontier APIs at production-acceptable quality. On quality-sensitive workloads where frontier-flagship would otherwise be required, Oscillitron delivers comparable quality at a 1.5–2× wedge. On regulated workloads where frontier APIs are unavailable at any price, Oscillitron is the only path with compliance primitives baked in.*

That's three sentences instead of one slogan, but it's defensible numerically and it doesn't oversell.

### 12.6 Adaptive depth is the load-bearing architectural feature

The §12.4 weighted-cost analysis makes one architectural feature **structurally critical**: the router (or some equivalent upstream classifier) must select execution depth correctly. If easy tasks get Tier C execution by mistake, the weighted cost balloons; if hard tasks get Tier A execution, quality collapses and the verifier-driven self-improvement loop fights uphill.

The framework-design.md and the skeleton's `pkg/router/rule` cover *which specialist* to route to, but not *how deep to run*. Adaptive depth selection is the missing piece. Two reasonable approaches:

- **Cheap intent classifier upstream of the router** decides tier-A vs. tier-B vs. tier-C based on task features (length, ambiguity signals, domain, prior similar tasks in playbook store).
- **Tier escalation in-flight**, where the system always starts at tier-A or tier-B, monitors verifier signal, and escalates to a deeper tier on grounded-check failure or low-confidence outcome. This is more expensive on average (some tasks pay the escalation cost) but more robust against classification error.

The brain analog (CLAUDE.md "anatomical priors plus cortical plasticity") favors the second: cortical processing depth varies with task demand, in-flight, governed by inhibitor-style detection of insufficiency. The cost-control implication is identical in either case: **depth is a property of the graph, controllable, monetizable.** A "premium depth" tier of service could explicitly include more aggressive tier-C escalation thresholds and be priced higher.

### 12.7 Specialization over time bends the curve

The architecture's self-improvement loop (CLAUDE.md "specialization grows organically within seed niches") has a cost implication that wasn't visible in §2: **as playbooks mature and retrieval indexes accumulate, tasks that previously required Tier C should fall back into Tier B.** A task that needed three parallel specialists and a tree-merge in month one might be handled by one well-curated specialist with a strong playbook in month twelve. The Tier B/C distinction is partly a function of how well-developed the specialist is.

This is a real monetization story: **Oscillitron's per-task cost should decline over deployment lifetime**, not by improving the architecture, but by improving the specialists running on it. The user who deploys today and stays on for two years should see their effective $/task drop as the specialization compounds. Compare this against a frontier API user whose costs are governed by external pricing decisions outside their control. Oscillitron has a *negative-cost-growth* story that frontier APIs cannot match.

This is also a meaningful pattern-K (compliance hosting) retention mechanism: customer playbook stores become more valuable to that customer over time, increasing switching cost. The longer they're on Oscillitron, the more specialized their deployment is, the more expensive it would be to rebuild on another platform.

### 12.8 What §12 changes for the monetization recommendations

**Three updates, no replacements:**

1. **The "cheaper than frontier" claim is workload-dependent and should be stated that way.** Marketing should not promise a flat "X% cheaper" without qualifying by workload mix. The honest positioning in §12.5 is the right outward-facing language. The §0 TL;DR's "≥3× cheaper than frontier API" is correct *for the standard-stack reference workload* but should not be over-applied.

2. **Adaptive depth + tier-pricing is a new monetization option.** A hosted offering could offer "Bronze / Silver / Gold" tiers that map roughly to Tier A / B / C execution profiles, with the customer choosing depth-per-workload. This is a known SaaS pattern, and it aligns Oscillitron's pricing surface with its actual cost structure rather than papering over the workload-mix dependency.

3. **The specialization-over-time story is a retention narrative.** When pitching pattern K (compliance hosting) or pattern J (vertical packaging), the cost curve flattening as playbooks mature should be part of the multi-year value story. It's also a defensible answer to the recurring objection "frontier API costs are dropping fast — won't you be eaten alive?" The answer is: frontier API costs fall on a curve set externally; Oscillitron's costs fall on a curve set by the customer's deployment maturity, which they own.

The §5/§10 sequence does not change. The §4 pattern scoring does not change. The bootstrap-with-consulting posture from §10 is reinforced — consulting engagements are the natural place to develop tier-C playbooks for specific verticals, and those playbooks become the seed of pattern J (vertical packaging) later.

### 12.9 Assumption sheet additions for §12

| # | Assumption | Value | Sensitivity |
|---|---|---|---|
| A32 | Tier A token count (light stack) | 5,600 | Low |
| A33 | Tier B token count (standard, from §2.2) | 14,200 | Low |
| A34 | Tier C token count (heavy stack, cheap-tier only) | 39,000 | Medium |
| A35 | Tier C audit sampling rate | 25% | Medium — could be higher for high-stakes regulated workloads |
| A36 | Tier C inhibition-restart expected overhead | 5% × 14K = 700 tokens | Medium |
| A37 | Tier C flagship escalation rate | 10% | Medium — depends on merge-conflict frequency |
| A38 | Workload mix in real deployments | Variable per customer | **High — this is the dominant factor for cost claim variance** |
| A39 | Specialization curve over time | Qualitative, not yet measurable | High — the "negative-cost-growth" story is an assertion until measured |

A38 is the single most consequential assumption in this whole document. Two customers with the same Oscillitron deployment can see 4× different per-task costs based on workload mix alone. Any per-task pricing model (B from §3) is exposed to this; any flat-fee or subscription pricing (D, F) is insulated. **This is yet another structural argument for the F/K/J patterns over per-task pricing.**

A39 is the strongest narrative-but-weakest-evidence claim. The architecture is *designed* to compound specialization over time, but until Phase 7 (calibration/quality harness from framework-design.md §14) ships and produces longitudinal data, the cost-curve-bending claim is unsupported. Don't lead with it; mention it as a directional expectation.

---

---

## §13 — The Quality-Lift Framing: Amplifier, Not Replacement

**Added after v1 review.** §12 framed Oscillitron's quality story as "match frontier" — an absolute target that turned out to be expensive and partially uneconomic. A sharper framing is **base-model amplification with measurable, per-model quality lift on defined benchmarks**. The architecture is not a frontier replacement; it is a *force multiplier on cheap base models*. The expected lift scales inversely with base-model strength (more headroom on weaker models, less on stronger ones), and the lift is measurable, falsifiable, and directly monetizable.

This reframing makes the whole project more honest, more testable, and more sellable. It also reframes the Phase 1 kill-or-proceed gate (framework-design.md §14) from a vague "does the cost-quality math work" into a concrete benchmark: *did we deliver ≥X% lift on benchmark Y for base model Z at cost multiplier ≤K?*

### 13.1 The reframing

The old claim was implicit and absolute: *"Oscillitron delivers frontier-equivalent quality at a fraction of frontier cost."* §12 showed that claim collapses on the workload mix that needs it most. The new claim is explicit and relative:

> *"Oscillitron delivers a measurable quality lift on cheap base models. The lift is largest on smaller/weaker models and smaller on already-strong models. We commit to specific lift targets per base model, measured on specific benchmarks, with public methodology."*

This is the difference between "trust us, it's almost as good as Claude Opus" and "on GSM8K, Qwen 2.5 7B baseline scores X; Qwen 2.5 7B running in Oscillitron's standard stack scores X+10%, with full eval reproducibility manifests." The second is a marketing claim, an engineering spec, a Phase 1 acceptance test, and an audit-eligible compliance artifact, all at once.

### 13.2 Per-model lift targets (illustrative)

Initial targets, to be calibrated against actual measurement in Phase 1. These are *hypotheses about what the architecture should deliver*, not measured outcomes. The pattern — larger lift on weaker base models — is consistent with the empirical prior art in §13.3.

| Base model | Approximate parameters | Target lift on reasoning benchmarks | Rationale |
|---|---|---|---|
| Phi-3 mini / Qwen 2.5 3B | 3–4B | +12 to +15% | Smallest base; most headroom; verification + recomposition compensates most for parametric weakness |
| Mistral Small / Qwen 2.5 7B | 7B | +10% | The user's hypothesized number; meaningful headroom, fast inference |
| Llama 3.1 13B / Qwen 2.5 14B | 13–14B | +8% | Middle ground; specialist division of labor still yields gains |
| Qwen 2.5 32B / Llama 3.3 32B | 32B | +7% | Strong base; lift comes mostly from playbook curation and grounded-checks |
| Llama 3.3 70B / Qwen 3.5 70B | 70B | +5 to +7% | The user's hypothesized number for this tier; least headroom but still real gains on harder tasks |
| Qwen3-235B / Llama 4 400B (MoE) | 200B+ | +3 to +5% | Diminishing returns; specialization still helps domain-specific tasks |

The shape of the curve — diminishing lift as base-model strength grows — is the most important architectural commitment in this section. **It says explicitly that Oscillitron is most useful for users running smaller cheap models, less useful for users already running near-frontier-class open weights.** This matters for go-to-market: the natural customer is the team running Qwen 7B/14B on consumer-grade hardware, not the team running Llama 4 400B on an 8x H100 cluster.

### 13.3 Empirical prior art for plausibility

The lift targets in §13.2 are not arbitrary. Multi-agent and verification-loop techniques published in 2022–2025 show lifts in the same range on similar benchmarks:

| Technique | Reported lift | Source/comparator |
|---|---|---|
| Self-consistency (multi-sample + voting) | 5–15% on GSM8K | Wang et al. 2022 |
| Tree-of-Thoughts | 20–40% on narrow puzzle tasks | Yao et al. 2023 |
| Branch-Solve-Merge | 5–10% on creative writing, fact-checking | Meta 2023 |
| Reflexion (iterative self-critique) | 10–30% on coding/reasoning depending on iterations | Shinn et al. 2023 |
| Self-Refine | 5–15% across various tasks | Madaan et al. 2023 |
| Multi-agent debate | 5–20% on factuality benchmarks | Various 2023–2024 |

Oscillitron's architecture combines elements from several of these techniques (specialist division of labor, AP-mediated handoff, recomposition tree-merge, verifier loop, audit sampling). The 5–15% lift range for cheap base models is well within the documented envelope of similar orchestration patterns. **What's novel is not the individual techniques but the production-grade integration plus the compliance primitives, plus the organic specialization-over-time story from CLAUDE.md.** Phase 1 measurement against these baselines is exactly the right validation gate.

Important caveat: published lifts are typically reported on narrow benchmarks (GSM8K, HumanEval, narrow puzzle sets). **Aggregate lift across realistic mixed workloads is typically smaller than headline benchmark lifts** — often 50–70% of the narrow-benchmark number. Oscillitron's targets should be set against realistic-workload aggregates, not against narrow-benchmark peak numbers.

### 13.4 Cost per percentage point of lift

The §12 tier-cost model + §13 lift hypotheses produce a useful operational metric: **dollars per percentage point of quality lift, per base model, per tier.** Lower is better. This becomes the architecture's primary efficiency metric.

Hypothetical illustration (lift values are targets, cost is from §12.2):

| Base model | Tier | Cost/task | Hypothesized lift | $/pt lift |
|---|---|---|---|---|
| 7B | A | $0.0038 | 4% | $0.00095 |
| 7B | B | $0.0131 | 8% | $0.00164 |
| 7B | C | $0.0395 | 14% | $0.00282 |
| 70B | A | $0.0038 | 2% | $0.00190 |
| 70B | B | $0.0131 | 5% | $0.00262 |
| 70B | C | $0.0395 | 9% | $0.00439 |

Two patterns emerge that are both architecturally meaningful and monetizable:

1. **Diminishing returns within a tier ladder.** A→B costs more per lift-point than baseline, B→C costs even more. The marginal lift-point gets more expensive as you go deeper. This is the cost-quality tradeoff from §12 expressed in lift terms.
2. **Diminishing returns across base-model strength.** 70B costs nearly 2× per lift-point compared to 7B at the same tier. Strong base models are more expensive to amplify because they have less headroom.

**The architecture's job is to minimize $/pt lift across the customer's workload mix.** This is a concrete optimization target the calibration tooling in framework-design.md §7 should produce numbers for. It's also a much sharper way to talk about "improving Oscillitron over time" than vague qualitative claims about specialization growth.

### 13.5 What lift framing changes for monetization

**Pattern J (vertical packaging) gains a concrete promise.** A "finance-tron" vertical kit can ship with a specific claim: *"On the FinanceBench reasoning benchmark, Qwen 2.5 14B baseline scores N; finance-tron-amplified Qwen 2.5 14B scores N+8%."* That number is the deliverable. Customers can verify it. Pricing the vertical kit at $30K–$75K/year (§3.J) is more defensible when the kit ships with a measurable, reproducible lift claim than when it's "a curated playbook set."

**Pattern K (compliance hosting) gains a regulator-friendly artifact.** SR 11-7 (banking model risk) explicitly requires documentation of model performance and ongoing monitoring. "Oscillitron lifts our base model by N% on benchmark B, measured quarterly, with reproducibility manifests stored in the audit ledger" is exactly the kind of model-validation evidence banking model-risk teams produce. The lift figure becomes part of the model documentation.

**Pattern M (consulting) gains scoped deliverables.** Engagements can be structured as *"Deliver ≥X% lift on benchmark Y for base model Z on the client's workload, with audit-ledger evidence, within N weeks."* This is a much sharper consulting product than "compound AI architecture advisory."

**The pricing surface shifts from per-token toward per-quality-point.** A hosted offering could plausibly price tiers by lift-target ("Standard: target +7% lift; Premium: target +10% lift; Enterprise: target +12% lift with custom calibration"). This aligns the price with the value claim in a way per-token never could. It also avoids the wedge-erosion problem in §3.A entirely — the customer pays for measurable improvement, not for inference.

**The "will frontier prices eat you alive" objection gets a new answer.** Frontier API costs falling doesn't reduce Oscillitron's amplification value. A user running Qwen 2.5 7B in Oscillitron at +10% lift today will still see +10% lift next year regardless of what GPT-6's input pricing does. The value proposition is *decoupled* from frontier pricing dynamics — the comparison is "Oscillitron on cheap base vs. cheap base alone," not "Oscillitron on cheap base vs. frontier API." Both alternatives stay on the customer's roster; Oscillitron isn't competing with frontier at all in this framing.

### 13.6 Phase 1 kill-or-proceed reframed

Framework-design.md §14 Phase 1 says:

> *Manual implementation on chosen workflow. Measure cost ratio and quality delta vs frontier baseline. Decision gate: does the architecture deliver on the cost-quality goal? Kill or proceed honestly.*

The lift framing sharpens this into a testable acceptance criterion. Reframed Phase 1 gate:

> *On benchmarks {GSM8K, MMLU, HumanEval, plus 1 domain-specific benchmark for the chosen Phase 1 workflow}, with base models {Qwen 2.5 7B, Qwen 2.5 14B, Llama 3.3 70B}, did the standard stack (Tier B) deliver ≥{8%, 6%, 4%} aggregate lift respectively, at cost multiplier ≤3× the base-model-alone cost? If yes, proceed. If no, kill or refactor.*

This is concrete, measurable, and survives external scrutiny. It's also the kind of gate that produces credible evidence for the investor narrative in §10.3.

### 13.7 What lift framing does NOT change

It does not change §5/§10 sequencing — the monetization order (consulting first, then signal-gated F/J/K) holds. It does not change the §4 pattern scoring — patterns F, J, K, M remain the highest-scoring. It does not eliminate the cost-quality tension in §12 — that tension is real and the lift framing just gives it a sharper name (the architecture amplifies the base by an amount; amplifying more costs more).

What it does change:

- The headline value claim (§0 TL;DR should be updated in a future version to lead with lift, not with absolute cost)
- The Phase 1 acceptance test (§13.6)
- The vertical-kit and consulting deliverable shape (§13.5)
- The way Oscillitron talks about the relationship to frontier APIs (no longer a competitor on the same dimension)

### 13.8 Assumption sheet additions

| # | Assumption | Value | Sensitivity |
|---|---|---|---|
| A40 | Quality lift on 7B-class models (Tier B) | +8 to +10% | **High — central claim, not yet measured. Phase 1 validates.** |
| A41 | Quality lift on 70B-class models (Tier B) | +5 to +7% | High — central claim, not yet measured |
| A42 | Diminishing returns curve shape (lift inversely proportional to base strength) | Qualitative | Medium — supported by prior-art pattern, exact slope unmeasured |
| A43 | Lift on aggregate workloads vs. narrow benchmarks | 50–70% of narrow-benchmark number | Medium — empirically robust pattern across literature |
| A44 | $/pt-lift improvement over time as specialization compounds | Directional only | High — same caveat as A39 in §12.9 |

**A40 and A41 are the central claims of the entire reframing.** They are the deliverables Phase 1 must produce evidence for. If Phase 1 measurement comes back with lift below the lower bound of these targets, the lift framing in §13 needs to be re-pitched — the architecture is either delivering less than expected, the targets were set wrong, or both. Either way it is the foundational empirical question, and §13.6 makes it explicit.

---

*End of monetization-analysis v1. Update as patterns are validated empirically and as assumptions in §8, §11.7, §12.9, and §13.8 are repriced against actual market data. §10 added 2026-05-18 to reflect Jim's clarified bootstrap-and-opportunistic-expansion posture. §11 added 2026-05-18 to fold in self-hosted capex/hosting economics and the regulated-buyer asymmetry. §12 added 2026-05-18 to model the quality-matching heavy stack and surface the cost-quality tension that §2 hid. §13 added 2026-05-18 to reframe the value claim as base-model amplification with measurable per-model quality lift, replacing the absolute "match frontier" framing.*
