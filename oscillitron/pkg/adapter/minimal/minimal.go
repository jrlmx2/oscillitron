// Package minimal provides the process-playbook instructions used by
// every substrate adapter that hits the OpenAI-compatible
// `/v1/chat/completions` surface.
//
// Why this exists: the legacy playbook prompts shipped in each
// adapter's `instructions.go` ask the substrate to emit a verbose
// JSON envelope (content / confidence / grounded_pass / ...) on every
// call — roughly 250 tokens of pure formatting overhead per request
// before the model sees the actual question. The minimal template
// drops that overhead. It asks for a small two-field JSON object,
// task-agnostic, with the heavy lifting done by the engine's
// `response_format` schema enforcement (Ollama / vLLM / LM Studio
// honor this; the schema constrains output to the expected shape so
// format compliance ceases to be a failure mode).
//
// Format: a JSON object with two fields:
//
//	{
//	  "response":   "...",   // task-agnostic; the loader's case
//	                          // prompt decides what belongs here
//	                          // (a letter, a \boxed{} expression,
//	                          // a paragraph, code, anything)
//	  "confidence": 0.0..1.0  // model's self-reported confidence;
//	                          // feeds the cope dispatcher
//	}
//
// Task-specific guidance lives in the case prompt the loader builds,
// not in this universal template. The MCQ-shaped framing that
// previous iterations carried ("Reply with the single letter
// A/B/C/D") is gone — that belongs to the GPQA / MMLU-Pro loaders,
// not the substrate template.
//
// Why the field is named `response` and not `answer`:
//
//   - "answer" reads as MCQ-specific (you give an answer); "response"
//     is task-agnostic.
//   - The earlier XML-tag iteration (PR #64, retired in this PR) used
//     `<response>` / `<confidence>` tag names. Keeping the same field
//     name on the JSON path makes the migration painless for any
//     downstream consumer that learned the tag-shaped contract.
//
// On capability floor: this package assumes a substrate ≥7B params at
// Q5_K_M or better that honors `response_format` JSON schema
// constraints. Substrates below this floor are out of scope — see
// `references/model-capability-floor.md`.
package minimal

// ProcessInstructions is the task-agnostic instruction template for
// the `process` playbook. Pairs with ProcessSchema (passed via
// `response_format` on the chat-completions request) so the engine
// constrains output to the expected JSON shape.
//
// Design notes:
//   - The template never changes per loader. Task specifics belong in
//     the case prompt the loader builds.
//   - `confidence` is load-bearing for the v3 chain (cope dispatcher
//     reads it). Without a reported confidence the dispatcher reads
//     0 and routes to ShipWithCaveat.
//   - Keep this under ~250 chars. Every extra sentence is context tax
//     on small substrates; if the universal template grows much past
//     this, the load belongs in the case prompt instead.
const ProcessInstructions = `Answer the following.

Reply with a single JSON object: {"response": "<your answer>", "confidence": <number 0.0 to 1.0>}.`

// ProcessSchema returns the JSON Schema constraining the model's
// response to {response: string, confidence: number 0-1}. Wrap it
// with AsResponseFormat and pass the result via the adapter's
// `response_format` field; the chat-completions engine constrains
// sampling against the schema.
//
// Returns a fresh map per call (Go can't have map constants); the
// allocation is negligible vs. the marshal-to-JSON cost the request
// already pays.
//
// Schema choices:
//   - `response` is `type: string` — the loader's case prompt decides
//     what shape belongs inside (a letter, a math expression,
//     prose, code, ...). The grader extracts after the substrate
//     returns; we don't pre-constrain content shape here.
//   - `confidence` is bounded [0, 1] so the engine refuses to emit
//     values outside that range.
//   - `additionalProperties: false` so the model can't sneak in
//     reasoning fields that break our parser.
func ProcessSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"response":   map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []string{"response", "confidence"},
		"additionalProperties": false,
	}
}

// AsResponseFormat wraps a JSON schema in the OpenAI-standard
// `response_format` envelope:
//
//	body["response_format"] = AsResponseFormat("process_response",
//	                                            ProcessSchema())
//
// `strict: true` tells the engine to enforce the schema rigorously.
// Ollama honors this; vLLM honors via outlines / lm-format-enforcer;
// LM Studio via its grammar layer.
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
