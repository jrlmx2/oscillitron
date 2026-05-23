// CLAUDE GENERATED
// Package minimal provides stripped-down playbook instructions for
// small substrates that get crushed by the JSON-envelope tax.
//
// Why this exists: the default playbook prompts in
// pkg/adapter/{hermes,ollama,vllm,lmstudio}/instructions.go ask the
// substrate to emit a specific JSON object on every call:
//
//	{
//	  "content":        "<your actual answer>",
//	  "confidence":     <number between 0.0 and 1.0>,
//	  "grounded_pass":  null,
//	  "contradictions": [],
//	  "open_questions": []
//	}
//
// That envelope is ~250 tokens of pure formatting overhead per call
// before the model sees the actual question. Capable models (≥30B)
// shrug it off. Small models (≤7B) spend significant attention on
// "produce this JSON exactly" and reason worse about the underlying
// problem.
//
// Empirical evidence: phi4-mini's published GPQA Diamond score is
// 36.9% (Microsoft, Q4 quantized base model). Our bench run with the
// JSON envelope measured 21–26%. Microsoft's eval prompt is bare:
// "Answer with A/B/C/D." The gap is the envelope tax. See
// scratch/bench-findings-2026-05-23.md (if present) and
// references/substrate-routing.md.
//
// The minimal templates here drop the envelope entirely. The
// adapters' existing `unstructuredFallback` path (in each substrate's
// structured.go) wraps the bare response into a
// ReturnResultPayload{Result.Content: rawText}, so the downstream
// grader's regex extractor still picks up the answer letter.
//
// SCOPE: v0 ships only `ProcessInstructions` — the single playbook
// the bench currently calls. Other playbooks can be added when a
// non-bench consumer needs them. The minimal forms are also
// MCQ-shaped (single-letter answer) — if a future bench needs free-
// form answers, a parallel `ProcessInstructionsFreeForm` would land
// alongside.
package minimal

// ProcessInstructions is the bare instruction template for the
// `process` playbook against an MCQ-shaped benchmark. Roughly 40
// tokens vs. the default ~250.
//
// Design notes:
//   - "End your response with the single letter" is the load-bearing
//     line — small models will reason out loud and forget to commit
//     a final letter without it. The bench's Multichoice grader runs
//     last-match-wins regex over [A-D], so the closing letter wins
//     even if earlier letters appear in the reasoning.
//   - No mention of "{answer}" output schema — bench sets it to a
//     placeholder string that's useless to the model.
//   - No JSON envelope — the adapter's unstructuredFallback handles
//     bare text and wraps it into ReturnResultPayload.
const ProcessInstructions = `Answer the following multiple-choice question. Read it carefully, then choose the best answer. End your response with the single letter (A, B, C, or D) of your final answer.`
