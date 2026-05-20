<!-- CLAUDE GENERATED -->

# Cost dynamics narrative (evaluator-facing)

For someone reading the Oscillitron pitch and trying to decide whether the **cost wedge thesis** is real. Conceptual, not operational. Companion to `artifacts/monetization-analysis.md` (which models the dollar math) and `references/performance-operator-guide.md` (which tells operators how to size their hardware).

Updated 2026-05-20.

## The thesis in one sentence

Per-token cost is set by the substrate (the model + the runner around it); per-output-quality cost is set by **how many of those tokens the substrate spends on boilerplate vs. on the user's actual work** — and that ratio is what Oscillitron's orchestration layer changes.

## What "cost" actually means in an LLM call

A single call to an LLM is paid in two currencies regardless of whether you're billed in dollars (hosted API) or watts (self-hosted):

1. **Input tokens** — the model has to read them. Cost scales linearly with input length and the model's per-token compute. On hosted APIs, ~$0.10–$15 per 1M input tokens depending on the model. On self-hosted, this is what the prompt-processing phase of the call burns.
2. **Output tokens** — the model has to generate them, one at a time. Cost scales linearly with output length and the model's per-token compute. On hosted APIs, typically ~3× the input cost per token because generation is sequential and harder to parallelize. On self-hosted, this is the token-by-token streaming phase.

For most chat-shaped workloads the output dominates. For Oscillitron-shaped workloads — **structured-output JSON envelopes, routing decisions, verifier verdicts** — the output is tiny (tens of tokens) and the input is fat (10–20k tokens of system prompt + instructions + task). The cost equation inverts: input tokens are 95%+ of the bill.

That inversion is the lever the cost wedge sits on.

## The empirical anchor

From the live smoke test in this repo on 2026-05-20:

- A single Oscillitron `Evaluate` call to a local Hermes-wrapped gemma-4-e4b: **14,087 input tokens, 5 output tokens.**
- The user's actual question ("say hello in three words"): ~10 tokens.
- The model's actual reply: ~5 tokens.
- Everything else (~14,000 tokens, 99.9% of the input): substrate boilerplate.

Of that ~14k tokens of substrate boilerplate:

- ~12k tokens: Hermes-agent's tool-definition + agent-loop system prompt. Loaded on every `/v1/runs` call by default. Most of it is tool definitions Oscillitron never invokes.
- ~1–2k tokens: Oscillitron's per-playbook instructions teaching the substrate to emit structured JSON.

The number that should make an evaluator pay attention is not 14k tokens. It's **99.9% of the input is overhead, not work.** Any orchestration layer that reduces that ratio buys real economic margin — at small model scale (where margins are tight) the difference between viable and non-viable, at large model scale where you can throw frontier API spend at it.

## Three structural facts driving the wedge

### Fact 1: Prompt-processing throughput is hardware-bounded, not model-bounded

A 4B-parameter model on Apple Silicon processes prompts at ~100–300 tokens/second. The same model on an RTX 4090 processes them at ~5,000–10,000 tokens/second. Same weights, same answers — **30–100× speed difference from hardware alone**. Why this matters for the pitch: when someone says "your wedge disappears if I use a faster GPU," they're correct *if* the workload's bottleneck is the cheap base model's per-token cost. It's not. The bottleneck is how many tokens the substrate forces through that base model. Oscillitron orchestrates fewer wasted tokens; faster hardware processes the inevitable tokens faster. **They're orthogonal levers — they multiply, not substitute.**

### Fact 2: The KV cache changes the steady-state economics

Modern LLM runtimes cache the activations for the prompt prefix. If two consecutive calls share a prefix (same system prompt, same instructions, different user message), the second call doesn't reprocess the prefix — it picks up from the cached state and only processes the delta.

This means **steady-state per-call cost is set by prompt *variance*, not prompt *length*.** A 20,000-token shared prefix that's reused across 1,000 calls costs about as much as a 100-token prompt that's never reused. From the smoke test: warm evaluate took 7.6s (cached prefix), cold call took 206s (uncached) — for the same 14k tokens of input.

The pitch implication is subtle but load-bearing: **Oscillitron's per-playbook session architecture (one Hermes-style specialist per playbook with stable instructions + a per-call user message) is structurally friendly to KV caching.** A naive "one giant prompt with all the playbooks inline" architecture is not. The architectural lock is paying off in dollars-per-call without the design having to optimize for it directly.

### Fact 3: Small + smart-orchestration beats big + dumb-call on the cost-per-quality curve

This is the part the monetization analysis (§13) frames quantitatively. The short version: a frontier model running stateless one-shot ($X per call, quality Q) is one point in the design space. A cheap-local model running inside an Oscillitron call tree (~$X/100 per call, but spending 5–20 calls in critique + verify + recompose) lands at *a different point* in the same space — comparable quality at meaningfully lower total cost, *or* materially higher quality at comparable total cost. Which axis you optimize against depends on the workload.

What's not appreciated by people reading the pitch for the first time: **the cost-per-quality curve isn't a single line through the design space.** It bends. Orchestration is what bends it. Substrate choice (small local vs. frontier hosted) picks where on the bent curve you sit; orchestration determines the curve's shape.

## Why "use a cheap model" is the wrong framing

The single most common misunderstanding when explaining Oscillitron to evaluators is the assumption that the value prop is "use a cheap model and pay less per token." That's not the value prop. That framing is:

- **Trivially true** (you can always use a cheap model and pay less)
- **Trivially false on quality** (cheap models alone give cheap-model quality)
- **Beside the point** (the wedge is about cost-per-quality, not cost-per-token)

The correct framing: **orchestration changes how much value each token produces, regardless of which model produced it.** If you point Oscillitron at a frontier hosted model, you get frontier quality at a cost discount via per-call efficiency (fewer wasted calls, cheaper verification sampling, etc.). If you point it at a cheap local model, you get above-cheap-model quality at near-cheap-model cost via verification loops + recomposition + targeted frontier escalation.

The substrate is a tuning knob; the architecture is the product.

## Things to be ready to answer

**"Doesn't a faster GPU collapse your advantage?"**
No — faster hardware reduces per-token wall-clock, but the orchestration layer reduces per-call token count. They compose. See Fact 1.

**"Why not just use the frontier API and skip the orchestration?"**
For some workloads this is the right answer and Oscillitron will route there (the `delegate` runtime escalation path is exactly this). The wedge lives in the workloads where frontier cost is prohibitive or where verifier sampling needs to be cheap. Workload mix matters; see monetization-analysis §12.

**"Won't your warm-cache advantage evaporate when traffic patterns change?"**
The KV cache benefit is structural to the per-playbook session architecture, not a workload-pattern artifact. Each playbook's session has stable instructions; user messages vary. As long as that architecture holds (LOCKED 2026-05-19), the steady-state cost dynamics hold.

**"Isn't 14k tokens of overhead just a Hermes problem?"**
Hermes is the worst case among substrates we've measured. A bare model behind `/v1/chat/completions` cuts the overhead 5–10× — but loses the per-specialist persistent memory that's the architectural reason Hermes is the substrate. **The trade is real and conscious:** Hermes makes specialization possible at the cost of fatter prompts. Oscillitron's orchestration is what amortizes that fatness across enough calls for the economics to work.

**"Are you measuring quality lift or just cost reduction?"**
Both. Cost reduction is measurable today (the smoke test demonstrates the cost-per-call shape directly). Quality lift requires the verifier policy to ramp up to steady state — `bootstrap_threshold=10_000` invocations is the moment lift measurement becomes meaningful. See parent CLAUDE.md "Verifier policy" lock.

## Where this connects to the monetization analysis

- `artifacts/monetization-analysis.md` **§11** — Self-hosted economics: assumes a single GPU tier and quantifies break-even. Read this guide first to understand *why* the GPU tier matters less than the prompt-size assumption.
- `artifacts/monetization-analysis.md` **§12** — Three-tier execution model and workload-mix-dependent positioning: this is the formal version of "the substrate is a tuning knob."
- `artifacts/monetization-analysis.md` **§13** — Per-model quality lift reframing: where the cost-per-quality curve becomes concrete with Phase 1 acceptance criteria.

`references/performance-operator-guide.md` is the operator-facing counterpart: same dynamics, but framed as "what hardware do I buy and what model do I point at" rather than "is the wedge real."
