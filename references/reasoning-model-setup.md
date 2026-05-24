# Reasoning-mode substrates: a capability the library should manage

**TL;DR — Reasoning/thinking mode in modern substrates is a *capability* the
library should coordinate per-call. The `ThinkingPolicy` interface in
`pkg/adapter/ollama` (landed in PR #66) is the right architectural shape,
but it is **currently inert on Ollama's `/v1/chat/completions` endpoint
for qwen3.x models** due to upstream Ollama bug
[#15392](https://github.com/ollama/ollama/pull/15392). Probes confirm
qwen3.5:9b emits a hidden reasoning trace on `/v1` regardless of the
`think: false` flag. The same flag works correctly on the native
`/api/chat` endpoint — so the policy code is correct; the substrate-side
plumbing is broken.

**Practical implication today:** reasoning-capable substrates
(qwen3.x, DeepSeek-R1, Magistral, exaone-deep, glm-5, kimi-k2-thinking,
gemma4) cannot be benched cleanly through the current ollama adapter.
Hard prompts → 3–10 minute calls regardless of policy. The bench
should default to non-reasoning substrates (qwen2.5:7b, llama3.1:8b,
mistral:7b) until Ollama #15392 lands or the adapter ports to
`/api/chat`.**

## What this is about

Modern substrates ship two-mode generation:

1. A **thinking** pass — the model emits an internal reasoning trace before
   producing visible answer content. The trace goes into a separate field
   (`thinking` on `/api/chat`, `reasoning` on `/v1/chat/completions`) and can
   run thousands of tokens for hard prompts.
2. A **direct** pass — same model, no internal trace, fast.

Qwen3.x ships thinking-on by default. DeepSeek-R1 family, Magistral, gpt-oss,
exaone-deep, glm-5, kimi-k2-thinking, gemma4 — all the recent reasoning-tuned
models ship a similar two-mode capability.

The library should *coordinate* which mode to use per call, the same way it
already coordinates vote-N attempts per call via `stakes.AttemptScale`.
Thinking is variable substrate effort. So is vote-N. They belong in the same
family of orchestration knobs.

## Empirical context — how we discovered the gap (2026-05-24)

Running the v3.5 bench against `qwen3.5:9b` (Ollama, q5/q6, Apple Silicon
Metal), with the current ollama adapter (no thinking-mode awareness):

```
07:00:31  msg=runner.evaluated     playbook=plan
07:10:31  msg=runner.execute_error err="ollama: POST /v1/chat/completions:
                                        context deadline exceeded"
```

The bench's 10-min HTTP client timeout was hitting consistently. Ollama's
serve log on the same wall-clock confirmed the server-side picture:

```
POST /v1/chat/completions | 500 | 10m0s  (× many)
POST /v1/chat/completions | 200 |  4m2s  (one survivor)
```

The model wasn't hung — it was generating ~1-2 tokens of thinking-trace
content per pass, accumulating thousands of tokens, never reaching the
visible-content step in under 10 minutes.

**The first round of probes showed `think: false` working — on `/api/chat`.**
Those results led to the ThinkingPolicy plumbing in PR #66. A second round
on 2026-05-24 (after the PR landed) probed the same flag on `/v1` and
revealed the upstream bug — `/v1` always emits reasoning on qwen3.5,
regardless of the flag. The full matrix:

| # | Endpoint | Schema | think | Model | Time | Reasoning len | Content len |
|---|---|---|---|---|---|---|---|
| 1 | `/v1` | none | false | qwen3.5:9b | 16 s | **564** | 1 |
| 2 | `/v1` | full | unset | qwen3.5:9b | 16 s | **741** | 42 |
| 3 | `/v1` | full | false | qwen3.5:9b | 15 s | **686** | 37 |
| 4 | `/v1` | simple | false | qwen3.5:9b | 12 s | **533** | 17 |
| 5 | `/api/chat` | none | false | qwen3.5:9b | **0 s** | **0** | 1 |
| 6 | `/api/chat` | `format: "json"` | false | qwen3.5:9b | 2 s | **0** | 34 |
| 7 | `/v1` | full | (n/a) | llama3.1:8b | 5 s | 0 | 49 |

Key reads:

- **`think: false` is a no-op on `/v1` for qwen3.5** — every `/v1` probe
  (rows 1–4) emits 500–740 chars of reasoning regardless of the flag.
- **`/api/chat` honors `think: false` correctly** — rows 5–6 show zero
  reasoning, sub-2-second response time.
- **Non-reasoning models on `/v1` are fine** — row 7 (llama3.1:8b) with
  full schema enforcement returns clean JSON in 5 s with zero reasoning.
- **Schema complexity isn't the issue** — rows 1 (no schema) and 4
  (simple one-field schema) both still emit reasoning.

So the slowness on hard prompts (the 181-sec call yesterday) was the
model generating ~30k tokens of reasoning at ~180 tok/sec, with no way
to suppress it on `/v1`.

**Generation speed when reasoning IS suppressed:** ~26–30 tok/sec on
qwen3.5:9b q5_K_M on this hardware. The substrate is fast when not
buried in reasoning; the bug is the inability to suppress it on `/v1`.

**`/no_think` chat-template prefix does not work either.** Ollama's
`/v1` wrapping passes it through as content. Neither template tricks
nor JSON flags reach the engine's think-suppression path on `/v1`.

## The capability framing — *not* "disable a problem"

It would be tempting to fix this by adding `Config.DisableThinking bool` to
the ollama adapter and calling it done. **That's the wrong shape.** Three
reasons thinking is a *capability* worth coordinating, not a *feature* worth
silencing:

1. **Variable substrate effort.** Same logical idea as vote-N: spend more
   compute when the case warrants. We already do this dispatch via
   `stakes.AttemptScale` (low → 1 attempt, high → 2×base). Thinking-budget
   slots into the same family. High-stakes case → think. Low-stakes case
   → answer fast. Hardcoding off everywhere defeats the substrate.

2. **The thinking trace is data.** It's:
   - *Auditable* — operators can inspect *why* the model picked an answer.
   - *Embeddable* — for the v5 routing / calibration learning, the trace
     embedding is likely more discriminative than the final answer
     embedding (`scratch/design-conversation-2026-05-24.md` §8b).
   - *Composable* — a child AP's reasoning trace could feed the parent's
     recompose step as "show your work" evidence.
   - *Verifier signal* — a critic playbook could grade reasoning quality
     independently of the final answer.

   Discarding it discards a signal the library could be exploiting.

3. **Per-call decision belongs at the orchestrator level.** The cope
   dispatcher already routes per-call based on stakes + confidence. The
   same logic applies to thinking: cheap inputs deserve fast generation,
   stakes-heavy inputs deserve reasoning. The adapter should *honor* the
   per-call decision, not make a global one.

## Architecture sketch — `ThinkingPolicy` interface

```go
// pkg/adapter/ollama/ollama.go

type Config struct {
    // ... existing fields ...

    // Thinking selects the reasoning-mode policy for substrates that
    // expose a thinking trace. nil = substrate default (which on Qwen3.x,
    // DeepSeek-R1, etc. means thinking-on).
    Thinking ThinkingPolicy
}

type ThinkingPolicy interface {
    // ShouldThink returns whether thinking mode should be enabled for
    // this Execute call. The envelope carries the per-call context
    // (Stakes, Evaluate.Playbook, Path, etc.) the policy reasons over.
    ShouldThink(env session.Envelope) bool
}
```

Stock policies (operator picks; orchestrator can override per-call):

| Policy | Behavior |
|---|---|
| `ThinkingAlwaysOn{}` | Substrate default. Reasoning on every call. Honest but expensive. |
| `ThinkingAlwaysOff{}` | What `think: false` would do globally. Fast bench mode. |
| `ThinkingByStakes{}` | `Stakes.High` → on, others → off. Aligns with the existing stakes-driven attempt scaling. |
| `ThinkingByPlaybook{...}` | Per-playbook map: plan → on (decomposition needs reasoning), process → off, compose → on, etc. Natural fit for the two-step AP shape. |
| `ThinkingByCalibration{Store}` | v5+: looks up the (model, domain, band) bucket from the v4 calibration store and enables thinking on buckets that historically need it. |

**Status as of 2026-05-24:** the policy interface + stock implementations
landed in PR #66. `Config.Thinking` is wired through ollama / vllm /
lmstudio adapters; `chatRequest.Think *bool` lands on the request body
per-call. **The code is correct, but the flag is currently a no-op on
Ollama's `/v1/chat/completions` for qwen3.x** per the probe matrix
above. See "Upstream bug — current status" below.

## Upstream bug — current status

[ollama/ollama#15392 — "server: fix structured output when think=false"](https://github.com/ollama/ollama/pull/15392)
is **open and in progress**. It addresses the broader pattern that
includes the bug we hit: structured-output requests not respecting
`think: false`. Related: closed
[#14440](https://github.com/ollama/ollama/issues/14440) confirmed the
think + JSON-schema interaction for gpt-oss:20b.

So the `/v1/chat/completions` think-suppression failure is upstream-
acknowledged, with an active fix. Our adapter is correctly emitting
the flag; Ollama just isn't honoring it yet for qwen3.x. When the
upstream PR lands and we update to that Ollama version, the policy
becomes effective on `/v1` for free — no code change needed on our
side.

## LM Studio — same family of problem expected

Probes against LM Studio's `/v1/chat/completions` weren't run today
(LM Studio doesn't have qwen3.5 on local disk; pulling 6 GB just for
cross-validation isn't worth it). But the architectural expectation:

- **LM Studio wraps llama.cpp**, the same engine Ollama uses under
  the hood. Reasoning behavior is a model-side property; both servers
  see the same trace generation.
- **LM Studio's `/v1` surface has no `think` parameter at all** —
  it follows strict OpenAI compatibility. So there's not even a knob
  to fail; reasoning is unconditionally on for reasoning-tuned models.
- **The likely failure mode on LM Studio:** reasoning embedded in
  `content` (mixed with the answer) rather than separated into a
  `reasoning` field. Worse for parsers; the JSON-schema enforcement
  may also break because the reasoning prose would be inside the
  string value, not as a sibling field.

Practical conclusion: **neither Ollama nor LM Studio offers reliable
thinking-mode suppression on `/v1`-style OpenAI-compat surfaces
today.** Reasoning-capable substrates are out of scope for the bench
until Ollama #15392 ships or the adapter ports to `/api/chat`
(which exposes the working `think` flag).

## What the orchestrator side looks like

The benchmark runner (`pkg/benchmark.Runner`) doesn't directly construct
adapter configs — `cmd/bench` does. So the wiring lands in `buildAdapter`
in `cmd/bench/main.go`:

```go
case "ollama":
    cfg := ollama.SingleEndpoint(url, model)
    // ... existing wiring ...
    cfg.Thinking = ollama.ThinkingByStakes{}  // or operator-configured
    return ollama.New(cfg)
```

For per-orchestrator-arm differences (e.g., frontier arm always-on,
cope-vote arm by-stakes, tree arm by-playbook), each arm constructs its
own adapter and picks its own policy. That's already the pattern for
`Inspector` and other per-arm config.

## Implications for the running bench

Once `ThinkingPolicy` lands, the substrate roster splits two ways:

| Group | Substrates | What `ThinkingByStakes` does |
|---|---|---|
| **Reasoning-capable** | qwen3.5, qwen3-coder-next, deepseek-r1, magistral, gpt-oss, exaone-deep, glm-5, kimi-k2-thinking, gemma4 (reasoning-tuned tags) | High-stakes → thinking on (slow, deep); else → thinking off (fast) |
| **No reasoning mode** | qwen2.5:7b, llama3.1:8b-instruct-q6_K, mistral:7b-instruct-v0.3-q6_K, hermes3:8b, glm4:9b | Policy is a no-op (the `think` field gets ignored by these substrates) |

This makes the cross-substrate bench comparison meaningfully richer. Same
case, two arms on the same reasoning-capable substrate (thinking-on vs
thinking-off) is itself an interesting calibration signal:

- If thinking-on improves pass-rate by N pp at X× cost, we can measure
  the per-token return on reasoning.
- v4 calibration-correction gets a new orthogonal axis to learn (pass rate
  in the (model, domain, thinking-mode) bucket).

## Until the upstream fix lands

The policy code has landed (PR #66) but is inert on Ollama's `/v1`
endpoint per the matrix above. The 10-min adapter timeout is still
the de-facto upper bound on per-call latency for reasoning substrates.
Three operator workarounds:

1. **Stick to non-reasoning models.** qwen2.5, llama3.1, mistral, hermes3,
   glm4 — none ship a reasoning mode by default. Q4-vs-Q6 caveat from
   `scratch/substrate-quantization-notes.md` still applies but is a 1-3 pp
   gap, not a multi-minute one. **This is the bench's default substrate
   roster until upstream fixes land.**
2. **Raise `Config.RunTimeout` past 10 min** if you genuinely want the
   reasoning trace. Slow but honest. Sliding-window numbers under this
   mode are NOT comparable to non-reasoning runs.
3. **Port the adapter to Ollama's `/api/chat` endpoint** (medium-term).
   The native endpoint honors `think: false` correctly per the matrix
   above, but doesn't expose `response_format` schema enforcement —
   it has a looser `format: "json"` constraint instead. That's a
   real trade-off worth measuring before committing to the port; on
   above-floor substrates the prompt-text alone usually produces
   valid JSON, so the schema's belt-and-suspenders may not be needed.

The cleanest fix is upstream — let Ollama #15392 land. The matrix data
above will be the validation for whatever fix shape they pick.

## Cross-references

- [`scratch/design-conversation-2026-05-24.md`](../scratch/design-conversation-2026-05-24.md)
  §8c — where the timeout symptom was first noted on the in-flight smoke.
- [`scratch/substrate-quantization-notes.md`](../scratch/substrate-quantization-notes.md)
  — broader substrate-setup discussion. Quantization and thinking-mode are
  the two big substrate-setup axes; this doc handles the second.
- [`oscillitron/pkg/adapter/ollama/ollama.go`](../oscillitron/pkg/adapter/ollama/ollama.go)
  — where `Config` lives and where `ThinkingPolicy` slots in. Request
  builder is in the same file.
- [`oscillitron/pkg/stakes/stakes.go`](../oscillitron/pkg/stakes/stakes.go)
  — the existing variable-substrate-effort knob. `ThinkingByStakes` is the
  natural sibling.
- [`references/substrate-routing.md`](substrate-routing.md) — size-based
  routing. Reasoning-mode is orthogonal — both axes matter independently.
  Note that the auto-routing list in `cmd/bench/main.go`'s
  `smallModelSubstrings` does NOT include any `qwen3*` tag today, so
  `qwen3.5:9b` and friends route to Hermes by default; going through
  Hermes does NOT fix the thinking-trace cost (the model still reasons —
  you just pay an additional 4k envelope tokens on top). The policy needs
  to live wherever the substrate call happens.

## Verification recipe — when the policy lands

Smoke against `qwen3.5:9b` under three configs on the original failing
case (GPQA Diamond case 1, plan playbook on the Tree arm):

- `ThinkingAlwaysOn{}` → expect ~4 min per call, content correctly emitted,
  reasoning trace populated, sliding-window quality (presumably) high.
- `ThinkingAlwaysOff{}` → expect ~3-8 sec per call, content emitted, no
  reasoning trace.
- `ThinkingByStakes{}` → expect mixed timing per case based on assigned
  stakes; high-stakes cases match the "always on" profile, others match
  "always off."

If any config exhibits the original 10-min-timeout pattern, the policy is
incomplete — most likely the options map is being clobbered downstream,
or the response parser is mis-routing the `reasoning` field back into the
content extraction path.

The empirical comparison across these three configs is itself a useful
data point for v4 calibration-correction design — it gives us the per-call
cost of thinking-on, which feeds the cost/benefit decision the orchestrator
should be making automatically over time.
