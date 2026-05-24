# Model capability floor

The library coordinates substrates that meet a minimum capability bar.
It does not compensate for substrates that fall below it.

This is the architectural choice the JSON-output-restore PR locked in
on 2026-05-24: the earlier XML-tag iteration was a workaround so
`phi4-mini` (2.7 B) wouldn't choke on JSON. The substrate was below the
floor; we should not have re-shaped the protocol around it. Floor
instead, workarounds never.

## The floor

A substrate is in-scope for Oscillitron's bench and orchestration
patterns if it meets all three of:

1. **≥ 7 B parameters**, instruction-tuned (not a base model).
2. **≥ Q5_K_M quantization** (Q6_K recommended for parity).
3. **Reliably honors `response_format` JSON-schema enforcement** when
   served via an OpenAI-compatible surface (Ollama, vLLM, LM Studio).

The third bullet is the *hard* requirement. The schema is what makes
`pkg/adapter/minimal.ProcessInstructions` work — the engine constrains
sampling to the documented `{response, confidence}` shape, so the
adapter's parser doesn't have to guess. A substrate that can't honor
the schema can't be used in-scope, regardless of how it scores on
GPQA Diamond in isolation.

## Validated above the floor (2026-05-24 roster)

| Substrate | Params | Quantization | Notes |
|---|---|---|---|
| qwen2.5:7b-instruct-q6_K | 7 B | Q6_K | reference substrate; tested across GPQA / MMLU-Pro / MATH-500 |
| qwen3.5:9b | 9 B | Q5_K_M (default) | reasoning-capable; needs `--thinking` policy (see `reasoning-model-setup.md`) |
| llama3.1:8b-instruct-q6_K | 8 B | Q6_K | broad chat baseline |
| mistral:7b-instruct-v0.3-q6_K | 7 B | Q6_K | older but reliable instruct |
| hermes3:8b | 8 B | Q4_K_M (no q6 tag in registry) | tool-use-tuned; flagged in `substrate-quantization-notes.md` |
| glm4:9b | 9 B | Q4_K_M (no q6 tag in registry) | agentic-capable |

The Q4 substrates (Hermes 3, GLM 4) sit at the edge of the floor;
they pass schema enforcement but produce ~1-3 pp lower pass rates
than the Q6 cohort on the same cases. Apply that adjustment when
comparing across substrates.

## Validated below the floor — excluded

| Substrate | Why it failed |
|---|---|
| `phi4-mini` (2.7 B) | Schema compliance unreliable. The XML-tag workaround (PR #64, retired in this PR) confirmed the model gibbering on the new format. Below the parameter floor *and* the compliance floor. |

`phi4-mini` is excluded from the matrix going forward. The library is
not designed to compensate for substrates below the floor. Operators
who want sub-7B behavior should use a different framework or upgrade
the substrate.

## Why a floor

The original Oscillitron pitch was *"cheap-substrate orchestration ≈
frontier quality at fraction of cost."* "Cheap" is not the same as
"weak." The orchestration premise rests on the cheap substrate being
*competent enough to follow basic structured-output contracts*. Below
the floor, the library spends compute working around the substrate's
inability to obey the protocol — which is the opposite of the design
goal.

Setting a floor instead of compensating:

- **Keeps the protocol clean.** ProcessInstructions stays at ~150 chars
  of task-agnostic JSON-asking text. No XML escape hatches, no
  per-substrate fallback parsers.
- **Makes the bench numbers meaningful.** When two substrates score
  differently, the gap is about the substrates' actual capabilities,
  not about which one's quirks we patched harder.
- **Sets correct expectations downstream.** Anyone integrating
  Oscillitron knows the bar their substrate has to clear. They don't
  pull in a 2 B model and discover at run-time that the library
  silently degrades it.

## How the floor is enforced

There is **no automatic check** today. The bench will *attempt* to run
against any substrate the operator wires; below-floor substrates will
just fail in characteristic ways (schema parse failures, gibberish
content, low pass rates). The floor lives in this doc and in the
substrate-roster discipline; if a future contributor proposes a
sub-7B substrate, the answer is "no" — point them here.

A `--require-floor` flag that statically rejects below-floor substrates
based on a known list could be added later, but isn't worth the
complexity yet.

## Cross-references

- [`scratch/design-conversation-2026-05-24.md`](../scratch/design-conversation-2026-05-24.md)
  — where the JSON-restore-plus-floor decision was made (§1 of that
  doc captures the empirical XML-vs-JSON debate).
- [`scratch/substrate-quantization-notes.md`](../scratch/substrate-quantization-notes.md)
  — the Q4-vs-Q6 caveat across the current roster.
- [`references/reasoning-model-setup.md`](reasoning-model-setup.md)
  — reasoning-mode substrates need the `ThinkingPolicy` knob; that
  axis is orthogonal to the capability floor.
- [`oscillitron/pkg/adapter/minimal/minimal.go`](../oscillitron/pkg/adapter/minimal/minimal.go)
  — where the `{response, confidence}` JSON shape is defined.
