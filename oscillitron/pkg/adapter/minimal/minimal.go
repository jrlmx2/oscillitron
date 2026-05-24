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
// `process` playbook against an MCQ-shaped benchmark. Roughly 60
// tokens vs. the default ~250.
//
// Design notes:
//   - The "report your confidence" line is load-bearing for the
//     v3 chain: ExtractConfidence (pkg/notice) parses the model's
//     self-reported confidence to feed EffectiveConfidence ->
//     cope.Decide. Without it, v3.2-v3.4 degenerates to
//     ShipWithCaveat on every call (because cope reads 0 as
//     "not reported" and refuses to escalate).
//   - When paired with ProcessSchema (response_format enforcement
//     at the OpenAI API level), the instruction is belt-and-
//     suspenders — the schema constrains the engine to emit both
//     fields. The text instruction stays useful for fallback when
//     a server doesn't honor response_format.
//   - The bench's Multichoice grader runs last-match-wins regex
//     over [A-D]; the closing-position imperative ("end your
//     response with…") is load-bearing on the fallback path. When
//     response_format isn't honored (or the substrate doesn't
//     support it), the grader extracts the last A/B/C/D in the
//     text — without a closing-position imperative, models often
//     emit "Answer: D. (Note this excludes option A.)" and
//     last-match-wins catches the wrong letter.
const ProcessInstructions = `Answer the following multiple-choice question. Choose the best option and report your confidence as a number between 0.0 and 1.0. End your response with the single letter (A, B, C, or D) as your final character.`

// ProcessSchema returns the JSON Schema constraining the model's
// response to {answer: string, confidence: number 0-1}. Used with
// the OpenAI `response_format` parameter: Ollama, vLLM, and LM
// Studio all support grammar-constrained sampling against this
// shape, eliminating format compliance as a failure mode.
//
// Returns a fresh map per call (Go can't have map constants); the
// allocation is negligible vs. the marshal-to-JSON cost the
// chat-completions request already pays.
//
// Schema choices:
//   - `answer` is `type: string` not enum[A,B,C,D] because some
//     benchmarks aren't MCQ. The bench's grader does the letter
//     extraction; we don't pre-constrain the substrate to a fixed
//     letter set at the schema layer.
//   - `confidence` is bounded [0, 1] explicitly so the engine
//     refuses to emit values outside that range.
//   - `additionalProperties: false` so the model can't sneak in
//     reasoning fields that break our parser.
func ProcessSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answer":     map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []string{"answer", "confidence"},
		"additionalProperties": false,
	}
}

// AsResponseFormat wraps a JSON schema in the OpenAI-standard
// `response_format` envelope:
//
//	body["response_format"] = AsResponseFormat("process_answer",
//	                                            ProcessSchema())
//
// `strict: true` tells the engine to enforce the schema rigorously.
// Ollama honors this; vLLM honors via outlines / lm-format-enforcer.
func AsResponseFormat(name string, schema map[string]any) map[string]any {
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   name,
			"schema": schema,
			"strict": true,
		},
	}
}
