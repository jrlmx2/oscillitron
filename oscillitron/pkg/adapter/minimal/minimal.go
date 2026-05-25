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

import "github.com/jrlmx2/oscillitron/pkg/session"

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

// PlanSchema returns the JSON Schema constraining the model's
// response for the `plan` playbook to {sub_aps, recompose}. The plan
// playbook's output category is emit_subtree — it decomposes a task
// into sub-APs and specifies how their results recompose.
//
// sub_aps items carry the fields an AP child needs: input (required),
// plus optional input_kind, output_schema, classification, and
// needs_verification. recompose is enum-constrained to the three
// modes the runner supports (pairwise, sequential, none).
func PlanSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sub_aps": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input_kind":         map[string]any{"type": "string"},
						"input":              map[string]any{"type": "string"},
						"output_schema":      map[string]any{"type": "string"},
						"classification":     map[string]any{"type": "string"},
						"needs_verification": map[string]any{"type": "boolean"},
					},
					"required":             []string{"input"},
					"additionalProperties": false,
				},
			},
			"recompose": map[string]any{
				"type": "string",
				"enum": []string{"pairwise", "sequential", "none"},
			},
		},
		"required":             []string{"sub_aps", "recompose"},
		"additionalProperties": false,
	}
}

// CritiqueSchema returns the JSON Schema constraining the model's
// response for the `critique` and `verify_grounded` playbooks to
// {verdict, issues}. Both playbooks produce verifier_signal output —
// a pass/fail/issues verdict with structured issue details.
//
// Each issue carries severity (info/warning/error), where (location
// in the target), and what (description of the problem). All three
// fields are required per issue so downstream consumers can triage
// without guessing.
func CritiqueSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"verdict": map[string]any{
				"type": "string",
				"enum": []string{"pass", "fail", "issues"},
			},
			"issues": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"severity": map[string]any{
							"type": "string",
							"enum": []string{"info", "warning", "error"},
						},
						"where": map[string]any{"type": "string"},
						"what":  map[string]any{"type": "string"},
					},
					"required":             []string{"severity", "where", "what"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"verdict", "issues"},
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

// AllPlaybookFormats returns a map from every v0 playbook to its
// pre-wrapped AsResponseFormat envelope, ready to pass directly as
// the `response_format` field on a chat-completions request.
//
// Mapping:
//   - plan           → PlanSchema        (emit_subtree output)
//   - process        → ProcessSchema     (return_result output)
//   - critique       → CritiqueSchema    (verifier_signal output)
//   - verify_grounded → CritiqueSchema   (same verifier_signal shape)
//   - compose        → ProcessSchema     (return_result, same shape as process)
func AllPlaybookFormats() map[session.Playbook]map[string]any {
	return map[session.Playbook]map[string]any{
		session.PlaybookPlan:           AsResponseFormat("plan_response", PlanSchema()),
		session.PlaybookProcess:        AsResponseFormat("process_response", ProcessSchema()),
		session.PlaybookCritique:       AsResponseFormat("critique_response", CritiqueSchema()),
		session.PlaybookVerifyGrounded: AsResponseFormat("verify_grounded_response", CritiqueSchema()),
		session.PlaybookCompose:        AsResponseFormat("compose_response", ProcessSchema()),
	}
}
