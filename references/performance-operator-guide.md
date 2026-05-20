<!-- CLAUDE GENERATED -->

# Performance & sizing guide (operator-facing)

For someone deciding **what hardware to put behind Oscillitron** and **which base model to point each playbook at**. Concrete numbers, decision points, sizing logic. Updated 2026-05-20.

## The single mental model

Every adapter call to a substrate (Hermes or anything that wraps a base model) costs one of two things:

1. **Prompt processing** — turning the input tokens into KV-cache activations. Scales linearly with input length, bounded by model architecture and hardware bandwidth.
2. **Token generation** — producing output tokens one at a time. Scales linearly with output length, bounded by sequential per-token compute.

For Oscillitron-shaped workloads (`evaluate` picks a playbook, `execute` runs it), **prompt processing dominates wall-clock by 1–2 orders of magnitude.** Output is short — a JSON envelope, a verdict, a few sub-AP seeds — typically tens of tokens. Input is long — substrate boilerplate (tool definitions, agent framework) plus Oscillitron's per-playbook instructions plus the task. Easily 10–20k tokens before the task itself.

This is the single most important fact to internalize: **the price of an Oscillitron call is set by how many input tokens the substrate sends to the model, not by how much output the model produces.** Every sizing decision flows from there.

## Measured numbers (gemma-4-e4b @ LM Studio @ Apple Silicon, 2026-05-20)

From the live smoke test in this repo:

| Call | Input tokens | Output tokens | Wall-clock |
|---|---|---|---|
| Cold call (KV cache empty) | 13,854 | 54 | **206.0s** |
| Warm evaluate | 14,087 | 5 | **7.6s** |
| Warm execute (different prefix) | 13,994 | 5 | **38.2s** |

Three things to read off this table:

1. **The first call is brutal.** 14k tokens of prompt processing at ~70 tok/s on integrated GPU. Anyone running Oscillitron locally for the first time will measure cold-call latency, conclude the system is broken, and quit.
2. **Warm calls can be fast.** The KV cache for the shared prompt prefix is reused — only the delta is processed. 7.6s on the same hardware is plausible for interactive use.
3. **Prefix changes break the cache.** Going from `evaluate` instructions to `execute` instructions changes the prefix → cache miss → re-process most of 14k tokens. 38s instead of 7s, on the same hardware, for an identical model query.

## The cost curve by hardware tier

Prompt-processing throughput for a 4B-parameter model:

| Tier | Hardware | Prompt tok/s | 14k-token call |
|---|---|---|---|
| Apple Silicon (M1–M3) integrated GPU | M-series MacBook / Studio | 100–300 | 50–140s |
| Consumer discrete GPU | RTX 4090 / RTX 5090 | 5,000–10,000 | 1–3s |
| Datacenter GPU | A100 / H100 | 20,000+ | <1s |
| Frontier hosted API | Anthropic / OpenAI / Google | n/a (different model) | 1–2s end-to-end |

Apple Silicon is **30–100× slower** than a single consumer discrete GPU for this workload. The reason is bandwidth: prompt processing is memory-bandwidth-bound, and an RTX 4090 has ~10× the bandwidth of an M2 Pro and a much wider math pipeline.

## Operator decision tree

### 1. "I'm just iterating on the design / running smoke tests"

Use a **frontier hosted API** (point Hermes at `openrouter:openai/gpt-4o-mini` or `openrouter:anthropic/claude-3.5-haiku`). 1–2s per call, no local model sizing, no GPU. Costs a few cents per smoke run. This is the fastest path to "the code works end-to-end" and lets you focus on the runtime, not the infrastructure.

### 2. "I'm building Phase 1 lift measurements"

Mixed setup. Local cheap model (gemma / phi / qwen-small) for the **evaluate** step — it's a routing classifier, doesn't need a smart model, and the per-call cost is the floor on your wedge. Frontier-hosted for **critique / verify_judge** sampling — those are the audit layer per the verifier policy lock; cost is paid only on the sampled slice.

To make local viable on Apple Silicon for Phase 1:

- Get the cache **warm before measurement**. The cold call is not the steady state.
- Use a **stable instruction prefix across evaluate and execute** (today they differ per-playbook; that's a known performance lever — see `references/cost-dynamics-narrative.md`).
- Prune the Hermes toolset for the Oscillitron profile. The default `[hermes-cli]` toolset ships ~12k tokens of tool definitions Oscillitron never uses. Set `toolsets: []` in `~/.hermes/config.yaml` for the Oscillitron profile and the prompt drops by 80%+.

### 3. "I'm planning to self-host past Phase 1"

A single workstation-class discrete GPU is the right shape. Concretely:

- **RTX 4090 (24GB)** — runs a 4–8B model at fp16 or a 30B model at int4 with room for KV cache. ~3,000–8,000 tok/s prompt processing. Handles all five v0 playbooks against the same model with the per-playbook session isolation Hermes provides. Cost ~$1,800 + $1,500 for a host system.
- **Two-GPU server (2× RTX 4090 or 1× A6000 + 1× consumer)** — lets you split: one GPU for the cheap-local-first evaluate model + grounded checks, the other for a stronger execute model. Matches the locked "evaluate is cheap-local-first" architecture without bouncing requests across machines.
- **Mac Studio M2 Ultra (192GB unified)** — counterintuitive option for self-host. Slower per call than a 4090 (~500–1,500 tok/s prompt processing) but can host much bigger models (70B+ in unified memory) on consumer-grade silent hardware. Worth considering if your workload mix tolerates per-call latency in the 5–20s range and you want a single quiet box on a desk rather than a tower in a server room. Around $5,000–7,000 depending on RAM.

### 4. "I'm doing production at scale"

Out of scope for v0. The relevant question is *where the per-instance Hermes processes live* (one per playbook per tenant, or shared with tenant-keyed session_ids) and how compose / verify_judge route to frontier vs local. See `artifacts/monetization-analysis.md` §11 for capex math.

## The Hermes-specific overhead

Hermes-agent injects ~12k tokens of boilerplate (tool definitions, agent loop instructions, behavioral framing) into *every* `/v1/runs` call by default. This is per-call, not amortized. Oscillitron's adapter adds another ~1–2k tokens of evaluate/execute instructions on top. The actual user task is usually <50 tokens.

**Three levers to cut this:**

1. **Trim Hermes toolsets.** `toolsets: []` in the Oscillitron profile's `config.yaml` removes the largest chunk. Default is `[hermes-cli]` which ships dozens of CLI tool definitions Oscillitron doesn't invoke. Confirmed in the smoke test that Oscillitron only needs the model to return JSON — no tool calls.
2. **Use `/v1/chat/completions` instead of `/v1/runs`.** Skips Hermes' agent loop entirely. Saves ~10k tokens per call. *Costs:* breaks the "specialist with persistent memory" architectural lock. v0-dev shortcut at best; not a steady-state answer.
3. **Stable prefix across evaluate and execute.** The largest warm-call regression in the smoke test was evaluate (7.6s) → execute (38.2s) — caused by the per-playbook execute instructions differing from the evaluate preamble. Restructuring instructions to share a stable system-prompt prefix would cut warm execute latency by ~5×. This is a code change in `pkg/adapter/hermes/instructions.go`; tracked as an open item.

## Quick recipes

### Make the smoke test fast for the next session

```yaml
# ~/.hermes/config.yaml (Oscillitron profile)
toolsets: []   # was: [hermes-cli]
```

```bash
# ~/.hermes/.env
API_SERVER_ENABLED=true
API_SERVER_HOST=127.0.0.1
API_SERVER_PORT=8642
```

Then restart Hermes (`hermes gateway run --replace`) and warm the cache with one throwaway call before measuring.

### Switch to a frontier model for dev

```yaml
# ~/.hermes/config.yaml — replace the model block
model:
  default: openrouter:openai/gpt-4o-mini
  provider: openrouter
```

Cost: ~$0.15/M input tokens for gpt-4o-mini. A full demo (10 calls × ~14k tokens) = ~$0.02. Iteration speed: ~10s end-to-end instead of 7+ minutes.

### Probe your own setup before tuning

```bash
# Cold-call latency:
time curl -sX POST http://127.0.0.1:8642/v1/runs \
  -H "Content-Type: application/json" \
  -d '{"input":"ping","session_id":"probe-cold","instructions":"reply with one word"}'

# Then tail the agent log to see actual prompt size and per-call latency:
tail -f ~/.hermes/logs/agent.log | grep "API call"
```

If you see input tokens >5k for a single-word query, the toolset boilerplate is your bottleneck — see "Trim Hermes toolsets" above.

## When to stop optimizing locally and just pay for tokens

Frontier hosted API pricing for the kind of model Oscillitron's `evaluate` step needs (cheap-local-first means: routing classifier, ~5 output tokens, JSON-shaped reply) is on the order of **$0.10–$0.30 per 1M input tokens.** A demo-shaped workload is ~140k tokens (10 calls × 14k each) — that's $0.014–$0.042 per full demo.

If your dev velocity is throttled by 7+ minutes per smoke run on local hardware, you are losing **more than $0.04 of productive time every smoke**. The frontier-API math is overwhelmingly favorable for dev work. Save the local GPU spend for steady-state self-hosting after the runtime has stabilized.

The wedge Oscillitron promises is *not* "use a cheap model and pay less per token." It's "use orchestration to get more value per token regardless of which model you point at." Dev tooling should optimize for iteration speed; production economics get optimized when production happens.
