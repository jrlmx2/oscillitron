# Substrate quantization variance — 2026-05-24

Pulled this set for the cross-substrate matrix run. Two of the five
fell short of the Q6 target because no `q6_K`-tagged variant exists in
Ollama's registry. The Q4 vs Q6 delta is worth keeping in mind when
interpreting pass-rate gaps between families — a 7-9B Q4_K_M model
typically scores 1-3 pp below the same model at Q6_K on the
benchmarks we run.

| Family | Ollama tag | Size on disk | Effective bits/param | Match Q6 target? |
|---|---|---|---|---|
| Qwen 3.5 | `qwen3.5:9b` | 6.6 GB | ~6 (Q5_K_M or low Q6 — default tag) | close enough |
| Llama 3.1 | `llama3.1:8b-instruct-q6_K` | 6.6 GB | ~6 (Q6_K explicit) | **yes** |
| Mistral | `mistral:7b-instruct-v0.3-q6_K` | 5.9 GB | ~6 (Q6_K explicit) | **yes** |
| Hermes 3 | `hermes3:8b` | 4.7 GB | ~4.5 (Q4_K_M default) | **no — Q4** |
| GLM 4 | `glm4:9b` | 5.5 GB | ~4.5 (Q4_K_M default) | **no — Q4** |

## Why this matters for cross-substrate comparison

The bench will report per-substrate pass rates and confidence
calibration tables. If Hermes 3 or GLM 4 score noticeably lower than
the Q6-aligned family, *part* of that gap is the quantization, not
the model. Specifically:

- **Q4_K_M → Q6_K typically adds 1-3 pp** on GPQA Diamond / MMLU-Pro
  (per published delta studies on Llama and Mistral families).
- **Calibration tightens slightly at higher bit-counts** — confidence
  bands at Q4 are typically wider (model has less precision to
  distinguish "I'm sure" from "I think so").

So when we read the matrix output:

- Honest comparisons between **Qwen / Llama / Mistral** (all ~Q6) — direct.
- **Hermes / GLM** vs the Q6 trio — apply a mental "+ 1-3 pp adjustment"
  before concluding the model family is weaker. The handicap is a
  variance source, not a signal.

## If we want clean Q6 parity later

Hermes 3 and GLM 4 *do* have Q6_K GGUFs available on HuggingFace; they
just aren't published to Ollama's library. Two paths to clean parity:

1. **Side-load via Modelfile**: download `.gguf` from HF, write a tiny
   Modelfile (`FROM /path/to/Hermes-3-8B-Instruct.Q6_K.gguf`), then
   `ollama create hermes3:8b-q6_K-osc -f Modelfile`. ~10 minutes per
   model. Same pattern we already used for `*-osc` tags in the past.
2. **Direct HuggingFace pull**: `ollama pull hf.co/<org>/<repo>:Q6_K`.
   Newer Ollama feature; should work if the repo has the right tag
   structure.

Defer until/unless the bench data shows we *care* about closing this
gap. For the v3.5 cross-substrate read, the rough Q4-vs-Q6 adjustment
is sufficient context.

## Pulled-but-skipped variants

- `phi4-mini` — capability floor too low (validated on 2026-05-23
  bench; XML format compliance broke at this scale)
- `qwen2.5:7b-instruct-q6_K` — superseded by qwen3.5:9b
- `qwen2.5-coder` variants (7b/14b plus -osc custom builds) — coder-
  specialized; not aligned with reasoning + general + tool use criteria

All cleared from local Ollama on 2026-05-24 to free ~52 GB.
