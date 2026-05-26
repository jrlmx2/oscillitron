# Bench findings — 2026-05-26

Session arc: removed JSON structured output from process/compose
playbooks, replaced with natural text + confidence annotation.
Added SystemPreamble ("Be terse and dense."), RawCaller for goal
extraction, and intent-based goal derivation.

## Key discovery

**JSON structured output was costing 15-55pp on every benchmark.**
Removing it and letting the model answer in natural text produced
the single largest quality improvement in the project's history.

| Benchmark | JSON frontier | Natural frontier | Delta |
|---|---|---|---|
| SB (20) | 70% | 90% | **+20pp** |
| ARC (20) | 70% | 85% | **+15pp** |
| GSM8K (20) | 0% | 55% | **+55pp** |
| MMLU-Pro (20) | 24% | 10% | -14pp |

GSM8K went from 0% to 55% — the model was computing correct answers
all along but couldn't produce valid JSON AND do arithmetic. Natural
text let it just compute.

MMLU-Pro dropped — 10-option MCQ (A-J) extraction from natural text
is less reliable than from JSON. Needs investigation.

## Changes that produced this

1. **Natural text process/compose** — instructions changed from
   "return a single JSON object" to "Answer the following. End your
   response with confidence: X.X on its own line." Token cost per
   call dropped 72% (36 → 10 tokens on a simple MCQ).

2. **SystemPreamble** — "Be terse and dense." prepended to every
   adapter call. Reduces rambling, self-contradiction, and verbose
   reasoning on small models.

3. **Removed grounded_pass, contradictions, open_questions** from
   process schema. Model never produced meaningful values for any of
   them. Dead token weight.

4. **RawCaller interface** — freeform prompts that bypass playbook
   instructions and response_format. Used by goal extraction and
   LLM answer extraction.

5. **Intent-based goal** — "Describe the intent of the following
   prompt." Produces natural-language goal descriptions that thread
   through the tree and recomposer.

6. **Confidence line stripping** — adapter strips "confidence: X.X"
   from Result.Content after extracting the number, so downstream
   consumers see only the answer.

## Tree still trails frontier

| Benchmark | Frontier | Vote-1 | Tree |
|---|---|---|---|
| SB (20) | 90% | 85% | 80% |
| ARC (20) | 85% | 70% | 80% |
| GSM8K (20) | 55% | 55% | 50% |
| MMLU-Pro (20) | 10% | 10% | 5% |

Tree loses to frontier on every benchmark. Root cause: on flat
process calls (which is most cases), tree is just frontier + an
evaluate call + the goal-as-outputSchema paragraph. The goal adds
noise without adding signal on simple MCQ. Decomposition rarely
fires (model picks process over plan on most cases), and when it
does, the recomposition doesn't reliably outperform a single call.

## 20-case sample noise

Results at 20 cases have ~5-10pp variance between runs. The
directional findings (natural > JSON, preamble helps, tree trails
frontier) are consistent across multiple runs, but exact numbers
fluctuate.

## What's still broken

- **MMLU-Pro** — dropped from 24% to 10%. 10-option MCQ extraction
  from natural text needs work. The regex extractor for A-J may be
  matching noise in verbose responses.

- **Tree value** — the orchestration stack isn't earning its
  overhead. Either the decomposition needs to be smarter (force plan
  on complex questions, skip on simple ones) or the tree needs to
  add value through something other than decomposition (e.g.,
  multiple perspectives, self-critique).

- **LLM extraction** — built but not wired (passthrough extractor
  used instead). The LLM extractor failed on single-letter responses
  because the model said "there's nothing to extract." Needs a
  short-response fast path or better prompt.

## Commits this session

```
fdcfedb refactor(extractor): add ctx + goal params to Extractor interface
699d071 feat(extractor): add DeriveGoal and LLMExtractor
f7e6b9a refactor(tree): use Case.Goal instead of internal goal derivation
5f827ac feat(bench): wire LLM goal derivation and extraction into runner
283a126 test(extractor): add integration test for goal + LLM extraction pipeline
0b29242 fix(extractor): drop structured output enforcement, use natural text
6274859 feat(adapter): add RawCaller interface, use for goal + extraction
88f3882 feat(adapter): add SystemPreamble for universal behavioral directives
83686b2 fix(extractor): resolve governor deadlock, add RawCaller + SystemPreamble
2032848 feat: goal-driven intent extraction + SystemPreamble + RawCaller
6103980 perf(adapter): remove contradictions + open_questions from process schema
57dc75a perf(adapter): remove grounded_pass from process schema
5ebc6ea feat(adapter): natural text for process/compose, drop JSON enforcement
7eda00c fix(bench): remove process response_format fallback
048659f fix(adapter): strip confidence line from Result.Content
fd970c3 fix(bench): restore per-benchmark regex extractors for vote tallying
```

## Bench result files

| File | What |
|---|---|
| /tmp/final2-sb.jsonl | SB 20-case natural text (F:90% V:85% T:80%) |
| /tmp/final2-arc.jsonl | ARC 20-case natural text (F:85% V:70% T:80%) |
| /tmp/final2-gsm8k.jsonl | GSM8K 20-case natural text (F:55% V:55% T:50%) |
| /tmp/final2-mmlu.jsonl | MMLU-Pro 20-case natural text (F:10% V:10% T:5%) |
