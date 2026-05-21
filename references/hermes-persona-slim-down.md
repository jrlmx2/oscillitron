<!-- CLAUDE GENERATED -->

# Hermes persona slim-down (operator-facing)

For someone running Oscillitron against a local Hermes who needs to **cut the 14k-token per-call overhead** and reduce VRAM pressure. The existing `references/performance-operator-guide.md` flags the problem ("Hermes-agent injects ~12k tokens of boilerplate") and lists three top-line levers; this doc goes one level deeper on the persona/config side — what every section of a default Hermes persona costs, what you can drop for an Oscillitron-shaped workload, and how that translates to VRAM.

Updated 2026-05-21.

## Read this first

Three things to internalize before touching config:

1. **You are paying for the persona on every call, not just the first.** Prompt processing reuses the KV cache when the prefix is byte-stable. Slimming the persona shrinks both the cold-call cost *and* the KV cache size — i.e., it gives you VRAM headroom too. See "VRAM math" below.

2. **Oscillitron's adapter already supplies the per-call instructions.** Hermes' default persona tries to make the model a useful generalist agent — tools, skills, helpfulness framing, multi-channel I/O. Oscillitron doesn't need any of that: every AP arrives with playbook-specific instructions telling the model exactly what JSON shape to emit. Persona work that Hermes does by default is **redundant** under the wrap-Hermes architecture (parent CLAUDE.md "Hermes integration: WRAP, not modify or fork", LOCKED 2026-05-18).

3. **Cuts are one-way until you remember why you cut them.** The persona is the user-facing thing operators tune over time. Document what you removed and why; future-you debugging a regression will want to be able to flip pieces back on.

## What's actually in a default Hermes persona

Hermes' default config injects four broad categories of content into every `/v1/runs` call. Approximate token shares come from the 2026-05-20 smoke test (gemma-4-e4b, 14,087 input tokens, ~99.9% boilerplate per `cost-dynamics-narrative.md`):

| Category | Approx. tokens | What it does | Oscillitron needs it? |
|---|---|---|---|
| Tool registry (`toolsets`) | ~10–12k | Definitions for every tool Hermes can call (CLI invocations, file ops, web fetches, etc.). One JSON-Schema-shaped block per tool. | **No.** The Oscillitron runner doesn't dispatch tool calls. Hermes only needs to return structured JSON. |
| Agent-loop framing | ~500–1k | Instructions teaching the model to plan-act-observe, decide when to stop, request approval, etc. | **No.** The runner owns the loop; Hermes is a single-shot completion from the runner's perspective. |
| Persona / behavioral framing | ~500–1k | "You are a helpful assistant," tone guidance, safety preamble, identity. | **Mostly no.** Per-playbook instructions Oscillitron sends already include the behavioral framing the playbook needs. Generic helpfulness preamble is wasted. |
| Skill / memory bootstrap | ~200–500 | Hooks for Hermes' built-in skill creation + persistent-memory recall. | **Partially.** Persistent memory is the architectural reason Hermes is the substrate. Skill creation, under the uniform-node lock (parent CLAUDE.md, 2026-05-19), is redundant — the curation layer handles consolidation. |

Plus Oscillitron's own ~1–2k tokens of per-playbook `instructions` (evaluate preamble or execute preamble). **That's the only part you should not cut** — it's the contract between the adapter and the substrate.

## The slim-down decisions, by category

### Cut: tool registry

Already called out in the perf guide. Set `toolsets: []` (or whatever the equivalent key is in your `~/.hermes/config.yaml`) for the Oscillitron profile. This is the single biggest win — **80%+ of the 14k baseline disappears.**

What you lose: the model can no longer invoke CLI tools, file ops, web fetches via Hermes' built-in tool surface. Oscillitron never reads tool-call output from Hermes anyway; this changes nothing observable.

When to keep it: while you're debugging Hermes itself and want the model to be able to poke around. Not for steady-state Oscillitron operation.

### Cut: agent-loop framing

Set the persona to single-shot mode if Hermes exposes one (some versions of `hermes-agent` have a `mode: single-shot` or equivalent). If it doesn't, you can usually empty out the agent-loop persona section in `~/.hermes/persona.yaml` (or whichever file your install uses) and let Oscillitron's per-call `instructions` drive the model entirely.

What you lose: Hermes can't pause for approval, request follow-ups, or chain its own internal sub-calls. Oscillitron rejects approval requests as inhibited (see parent CLAUDE.md "Architecture") and dispatches its own call tree — the agent loop is duplicate machinery.

When to keep it: if you're using the same Hermes instance for non-Oscillitron workloads (Slack bot, CLI tool, etc.) that *do* need the agent loop. In that case run Oscillitron against a separate Hermes profile.

### Cut (mostly): persona / behavioral framing

Strip the generic-helpful-assistant preamble. Replace with the bare minimum the model needs to interpret Oscillitron's instructions — typically just "You will receive structured instructions and must reply with structured JSON. Follow the schema exactly." A 20–50-token preamble is plenty.

What you lose: the model is less "chatty," which is exactly what you want for structured-output workloads. Tone framing for end-user-facing assistants doesn't apply to Oscillitron.

When to keep it: if you want the verifier/critique playbook to read more naturally for human review. Even there, the per-playbook execute instructions can carry the framing — keep it out of the persona to preserve cache hits.

### Keep: persistent-memory / per-instance learning hooks

Hermes' persistent memory is the architectural reason Oscillitron uses it as a substrate rather than calling `/v1/chat/completions` directly. Don't strip the memory subsystem. The skill-creation hook is the part you can cut — under the uniform-node lock, Oscillitron's curation layer (the playbook substrate) handles consolidation, and Hermes' generic skill-creation tends to invent skills Oscillitron's call tree never invokes.

What you keep, concretely: anything that loads prior session state on a `session_id` match. With the tree-scoped session_id rework (`<RootID>:evaluate` and `<RootID>:<playbook>` — see Hermes adapter docs), persistent memory now spans entire call trees within one playbook, which is the whole point.

## A starter shape (not a literal template)

Hermes' config schema evolves; an Oscillitron-shaped slim persona looks roughly like this in spirit — copy the structure, not the literal keys, into your install's actual format:

```yaml
# ~/.hermes/config.yaml (Oscillitron profile) — slim shape
toolsets: []                    # was: [hermes-cli]   ← biggest win
mode: single-shot               # or whatever your install calls it
persona:
  system_prompt: |
    You will receive structured instructions and must reply with
    structured JSON. Follow the schema in the instructions exactly.
  skill_creation: false         # curation layer owns consolidation
  memory:
    enabled: true               # keep — this is why we wrap Hermes
    scope: session_id           # tree-scoped per the adapter's session_id rework
model:
  default: <your local or hosted model id>
```

Concrete keys for your version of `hermes-agent` will differ. The principle is: **toolsets empty, agent loop off, persona minimal, memory on**. If a key isn't on this list, you probably don't need it tuned.

## VRAM math: what slimming buys you

Persona tokens cost VRAM in two places:

1. **KV cache for the prefix.** ~2 × `tokens × layers × heads × head_dim × bytes_per_element`. For a 4B parameter model at fp16: roughly 80 KB per token. A 14k-token persona → ~1.1 GB just to keep the prefix's K and V tensors resident across calls. Cutting to 2k tokens → ~160 MB. **~950 MB freed.**

2. **Per-call activation memory** during prompt processing. Smaller (depends on batch and sequence length) but scales linearly with prompt size on the way in.

If you're running close to the VRAM ceiling on consumer hardware — RTX 4060 16GB, M-series with 16–32GB unified — the persona slim-down can be the difference between "the model OOMs after a few calls because the KV cache grew" and "comfortable headroom for the whole call tree."

For a worked example: an RTX 4090 (24 GB) running a 4B model at fp16 with the *default* Hermes persona uses ~9 GB for model weights + ~1.1 GB per persona KV cache. Five active sessions (the locked v0 playbook set, per-instance under multi-endpoint) → 5 × 1.1 GB = 5.5 GB of cache alone, before any user data. **Slimming to 2k tokens cuts the cache budget to ~800 MB total** — leaves ~4–5 GB for actual conversation history.

Numbers are order-of-magnitude; exact bytes depend on attention architecture, group-query-attention factor, and quantization. The shape of the curve is what matters: persona size dominates KV cache, and KV cache dominates non-weight VRAM for the workloads Oscillitron drives.

## Verifying your slim-down worked

The probe recipe from the perf guide, repeated here for convenience:

```bash
# Send a trivial query and time it.
time curl -sX POST http://127.0.0.1:8642/v1/runs \
  -H "Content-Type: application/json" \
  -d '{"input":"reply with one word","session_id":"slim-probe","instructions":"reply with one word"}'

# Tail the agent log to see the actual prompt token count Hermes built.
tail -f ~/.hermes/logs/agent.log | grep -E "(API call|prompt|tokens)"
```

Targets after slimming:

- **Input tokens for a trivial query: <3,000.** Down from the 13,854 baseline in the smoke test. If you're seeing >5,000 for a single-word input, the toolset is still loaded or the persona is still fat.
- **Cold call wall-clock on Apple Silicon: <40s.** Down from ~206s baseline. On discrete GPU, sub-second.
- **VRAM at idle after one warm session: <2 GB above model weights.** Above that means cache is large.

If the numbers don't move, the most common cause is the wrong config file. Hermes installs sometimes maintain multiple persona files; the one Oscillitron's traffic hits is the one bound to whichever profile/port Oscillitron points at, not necessarily the one you edited.

## What you trade by slimming — the honest list

| You drop | You give up |
|---|---|
| Tool registry | Hermes-driven tool calls (CLI, files, web). Oscillitron doesn't use these. |
| Agent loop | Hermes-driven plan-act-observe + approval pauses. The runner does its own dispatch. |
| Generic persona preamble | "Helpful assistant" tone. Per-playbook instructions can carry whatever tone the playbook needs. |
| Skill creation | Hermes' built-in skill auto-creation. The curation layer does consolidation (parent CLAUDE.md, uniform-node lock). |
| **Nothing else.** | |

Persistent memory, the session model, the `/v1/runs` SSE surface, and Oscillitron's per-call instructions are all preserved.

## When NOT to slim

- **You're debugging Hermes itself.** The toolset and agent loop make it easier to inspect what the model can/can't do.
- **You share the Hermes install with non-Oscillitron callers.** Slack bot, CLI agent, anything that wants the default agent loop — keep that config intact and create a separate Oscillitron profile.
- **You're benchmarking the *unslimmed* baseline.** Measuring the wedge requires keeping the "before" picture honest.

## Cross-references

- `references/performance-operator-guide.md` — the "Hermes-specific overhead" section is the higher-level companion to this doc; this one goes deeper on persona structure and VRAM.
- `references/cost-dynamics-narrative.md` — Fact 2 (KV-cache changes the steady-state economics) is the conceptual frame; persona slim-down is the direct lever the operator pulls to make that fact pay off.
- Oscillitron adapter docs (`oscillitron/CLAUDE.md` Hermes adapter section) — explains the tree-scoped session_id scheme that makes persona slimming compound with KV-cache hits.
