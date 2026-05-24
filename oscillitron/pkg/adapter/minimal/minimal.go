// Package minimal provides the stripped-down process-playbook
// instructions used by every substrate adapter.
//
// Why this exists: the default playbook prompts in
// pkg/adapter/{hermes,ollama,vllm,lmstudio}/instructions.go ask the
// substrate to emit a verbose JSON envelope on every call. Capable
// models (≥30B) shrug it off; small models (≤7B) spend significant
// attention on "produce this JSON exactly" and reason worse about
// the underlying problem.
//
// The minimal template here drops the envelope. The instruction is
// task-agnostic — the case prompt supplies whatever task-specific
// guidance is needed (e.g., the GPQA loader appends "Answer with a
// single letter," and the MATH-500 loader appends "wrap your final
// answer in \boxed{}"). The minimal template's job is just to mark
// where the response goes, in a parseable shape.
//
// Format: XML-tagged response and confidence.
//
//	<response>{whatever the case asked for}</response>
//	<confidence>{0.0 to 1.0}</confidence>
//
// XML tags were chosen over JSON because:
//   - They work uniformly across every substrate (no `response_format`
//     schema enforcement needed — universally trained-on).
//   - They produce clean start/end markers that don't confuse small
//     models the way "end your response with a single letter as the
//     final character" did (the 2026-05-23 regression source — small
//     models meandered mid-calculation and never anchored an answer).
//   - The response slot is task-agnostic. A letter, a `\boxed{53}`,
//     prose, or code all fit inside.
//
// Parsing helpers live in parse.go: ExtractResponseTag pulls the
// last `<response>...</response>` content; ExtractConfidenceTag
// pulls the last `<confidence>...</confidence>` numeric value.
// Both fall through gracefully when the tag is missing — that's
// the adapter's signal to apply unstructured-fallback semantics.
package minimal

// ProcessInstructions is the bare instruction template for the
// `process` playbook. Task-agnostic — the case prompt supplies
// task-specific guidance.
//
// Design notes:
//   - The format is XML tags (not JSON, not letter-positional). This
//     is the canonical wire shape across every substrate.
//   - The `<confidence>` slot is load-bearing for the v3 chain:
//     EffectiveConfidence (pkg/notice) feeds cope.Decide. Without a
//     reported confidence the dispatcher reads 0 and routes to
//     ShipWithCaveat.
//   - No mention of "single letter," "multiple choice," or any other
//     task-specific framing. The case prompt — built by the loader —
//     handles that.
const ProcessInstructions = `Answer the following.

Reply in this exact format:
<response>your answer</response>
<confidence>0.0 to 1.0</confidence>`
