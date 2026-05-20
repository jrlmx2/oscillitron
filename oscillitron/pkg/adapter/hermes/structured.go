//go:build hermes_stage5

// CLAUDE GENERATED
package hermes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// structuredPayload is the JSON shape the adapter expects Hermes to
// emit so the call-tree machinery has something to act on. Hermes'
// run.completed "output" is plain prose by default — the adapter
// nudges it toward this shape via system instructions (see
// renderInstructions) and parses it back here. The fields mirror
// session.Output minus ExitReason (which the adapter sets) and
// minus content-vs-rest negotiation (we accept "content", but if it
// is missing the raw output prose stands in).
//
// A model that fails the instructions and emits no JSON block at
// all is handled by extractStructured falling back to the raw text
// with zero-valued structured fields — the run completed, we just
// got no decomposition signal out of it.
type structuredPayload struct {
	Content        string             `json:"content"`
	Classification string             `json:"classification"`
	Confidence     float64            `json:"confidence"`
	Signals        []string           `json:"signals"`
	Contradictions []string           `json:"contradictions"`
	OpenQuestions  []string           `json:"open_questions"`
	SubAPs         []structuredSubAP  `json:"sub_aps"`
}

type structuredSubAP struct {
	BrainFunction string `json:"brain_function"`
	Input         string `json:"input"`
	OutputSchema  string `json:"output_schema"`
}

// jsonFenceRE matches a fenced JSON code block — Markdown's standard
// ```json ... ``` — anywhere in the output. The (?s) flag lets `.`
// span newlines. We tolerate any leading whitespace inside the fence.
var jsonFenceRE = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")

// rawObjectRE matches a JSON object that occupies the whole string
// (modulo whitespace). This is the "model obeyed the instructions
// and emitted JSON, nothing else" case. (?s) for multiline.
var rawObjectRE = regexp.MustCompile(`(?s)^\s*(\{.*\})\s*$`)

// extractStructured pulls a structuredPayload out of Hermes' raw
// run.completed output. Order of preference:
//
//  1. The whole output is a JSON object → use it.
//  2. A fenced ```json block exists → use that block.
//  3. Otherwise → return the raw text in Content, zero-value the
//     rest, no error.
//
// If parse fails on a candidate that LOOKED like JSON, return an
// error — that's a real protocol violation worth surfacing to the
// caller.
func extractStructured(raw string) (structuredPayload, bool, error) {
	if m := rawObjectRE.FindStringSubmatch(raw); m != nil {
		var p structuredPayload
		if err := json.Unmarshal([]byte(m[1]), &p); err != nil {
			return structuredPayload{}, false, fmt.Errorf("hermes: parse whole-output JSON: %w", err)
		}
		return p, true, nil
	}
	if m := jsonFenceRE.FindStringSubmatch(raw); m != nil {
		var p structuredPayload
		if err := json.Unmarshal([]byte(m[1]), &p); err != nil {
			return structuredPayload{}, false, fmt.Errorf("hermes: parse fenced JSON: %w", err)
		}
		// Content defaults to the prose around the fence if the model
		// didn't fill it. Strip the fence itself out so prose +
		// content don't double up.
		if strings.TrimSpace(p.Content) == "" {
			p.Content = strings.TrimSpace(jsonFenceRE.ReplaceAllString(raw, ""))
		}
		return p, true, nil
	}
	return structuredPayload{Content: raw}, false, nil
}

// toOutput projects a structuredPayload into a session.Output.
// Caller sets ExitReason.
func (p structuredPayload) toOutput() session.Output {
	subs := make([]session.SubAPSeed, 0, len(p.SubAPs))
	for _, s := range p.SubAPs {
		bf := session.BrainFunction(strings.TrimSpace(s.BrainFunction))
		if bf == "" {
			continue
		}
		subs = append(subs, session.SubAPSeed{
			BrainFunction: bf,
			Input:         session.Input{Type: "subap_result", Content: s.Input},
			OutputSchema:  s.OutputSchema,
		})
	}
	return session.Output{
		Content:        p.Content,
		Classification: p.Classification,
		Confidence:     p.Confidence,
		Signals:        p.Signals,
		Contradictions: p.Contradictions,
		OpenQuestions:  p.OpenQuestions,
		SubAPs:         subs,
	}
}

// structuredInstructions is the system-prompt preamble appended to
// the envelope's OutputSchema. It tells the substrate (Hermes +
// whatever model it's pointed at) to wrap its answer in a structured
// envelope the adapter can parse.
//
// Kept deliberately short: cheap local models drop long instructions
// before they reach the answer. The preamble explains the shape; the
// envelope's OutputSchema is what the model's "classification" field
// validates against.
const structuredInstructions = `You are a specialist invocation inside a call-tree reasoning system. Your reply MUST be a single JSON object (no prose around it) with this exact shape:

{
  "content": "<your actual answer>",
  "classification": "<one short label asserting your output matches the schema below, or 'schema_violation' if you cannot satisfy it>",
  "confidence": <number between 0.0 and 1.0>,
  "signals": ["<short grounding notes>"],
  "contradictions": ["<self-noticed contradictions with anything you've been told>"],
  "open_questions": ["<unresolved threads you noticed but did not pursue>"],
  "sub_aps": [
    {"brain_function": "<one of: perception|retrieval|planning|reasoning|critic|composition>", "input": "<what to ask the sub-invocation>", "output_schema": "<contract for the sub-output>"}
  ]
}

Leave any of signals / contradictions / open_questions / sub_aps as empty arrays when you have nothing to report. Emit sub_aps only when you genuinely need a sub-invocation to finish your work — otherwise return [].

Output schema (your "content" must satisfy this):
`

// renderInstructions builds the full "instructions" string for a
// /v1/runs request. When the envelope has an OutputSchema, it
// becomes the trailing schema directive; otherwise the preamble
// stands alone (caller still gets a structured envelope back).
func renderInstructions(outputSchema string) string {
	if strings.TrimSpace(outputSchema) == "" {
		return structuredInstructions + "(no specific schema; satisfy the user request as best you can)"
	}
	return structuredInstructions + outputSchema
}
