// CLAUDE GENERATED
package hermes

import (
	"encoding/json"
	"strings"
)

// outputFormatInstruction is appended to every prompt. It asks Hermes
// to end its response with a fenced JSON block carrying the
// structured fields the orchestrator cares about. If Hermes omits or
// malforms the block, the adapter falls back to verdict-only — the
// instruction is best-effort, not load-bearing.
//
// Design choice (locked 2026-05-18): structured output via
// prompt-engineering rather than a parsing-layer or a verifier-pass
// node. Cheapest path that lets the existing confidence/contradictions
// inhibitors keep working when Hermes is in the loop.
const outputFormatInstruction = `

---

After your answer above, on a new line, emit a fenced JSON code block in this exact form. Omit a field if you have nothing to say for it. Confidence is a number in [0,1].

` + "```json" + `
{
  "confidence": 0.0,
  "signals": [],
  "open_questions": [],
  "contradictions": []
}
` + "```" + `
`

// outputBlock is the shape we expect inside the fenced JSON block.
// All fields optional; zero values are fine.
type outputBlock struct {
	Confidence     *float64 `json:"confidence,omitempty"`
	Signals        []string `json:"signals,omitempty"`
	OpenQuestions  []string `json:"open_questions,omitempty"`
	Contradictions []string `json:"contradictions,omitempty"`
}

// extractStructured pulls the trailing ```json ... ``` block (if any)
// out of raw, returns it parsed, and returns the verdict text with the
// block removed. If no block is found or it doesn't parse, the verdict
// is raw unchanged and the returned block is the zero value.
//
// Strategy: find the LAST fenced block tagged "json" (Hermes might
// emit other code blocks in its answer; we only care about the
// structured suffix). Tolerate variants: "```json", "``` json", and
// case-insensitive on the tag.
func extractStructured(raw string) (verdict string, block outputBlock) {
	const fence = "```"
	// Find the last opening fence.
	openIdx := -1
	tagLen := 0
	searchEnd := len(raw)
	for {
		i := strings.LastIndex(raw[:searchEnd], fence)
		if i < 0 {
			break
		}
		// Examine the tag immediately after the fence.
		rest := raw[i+len(fence):]
		// Trim leading spaces between fence and tag.
		j := 0
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		// Read up to newline as the tag.
		nl := strings.IndexByte(rest[j:], '\n')
		if nl < 0 {
			searchEnd = i
			continue
		}
		tag := strings.ToLower(strings.TrimSpace(rest[j : j+nl]))
		if tag == "json" {
			openIdx = i
			tagLen = len(fence) + j + nl + 1 // up to and including the newline
			break
		}
		searchEnd = i
	}
	if openIdx < 0 {
		return raw, outputBlock{}
	}
	// Find the closing fence after the opening block.
	bodyStart := openIdx + tagLen
	closeRel := strings.Index(raw[bodyStart:], fence)
	if closeRel < 0 {
		return raw, outputBlock{}
	}
	body := raw[bodyStart : bodyStart+closeRel]

	if err := json.Unmarshal([]byte(body), &block); err != nil {
		// Malformed JSON inside the block — leave verdict intact, no
		// structured fields. (We could try to repair, but the contract
		// is best-effort.)
		return raw, outputBlock{}
	}

	// Strip the whole fenced block (and any trailing fence + newline)
	// out of the verdict.
	tail := bodyStart + closeRel + len(fence)
	if tail < len(raw) && raw[tail] == '\n' {
		tail++
	}
	verdict = strings.TrimRight(raw[:openIdx], " \t\n") + raw[tail:]
	verdict = strings.TrimRight(verdict, " \t\n")
	return verdict, block
}
