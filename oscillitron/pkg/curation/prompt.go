// CLAUDE GENERATED
package curation

import (
	"fmt"
	"strings"
)

// DefaultPromptTemplate is the cold-path session prompt builder
// used when Config.PromptTemplate is nil. The structure:
//
//   - Persona framing ("You are the [action] specialist…")
//   - Task description (the exemplar-selection criterion)
//   - One-shot output format example
//   - The batch of candidates
//   - "Reply with the JSON object only" tail
//
// Custom prompt templates can replace this entirely but must
// produce output the model will respond to with a SelectionResponse
// (JSON object with "exemplars": [{case_id, reason}, ...]) so
// parseSelectionResponse can extract it.
//
// The candidate format is intentionally compact (case_id, prompt,
// output, score) to maximize how many candidates fit per cold-path
// call. The cold-path session is asked to be SELECTIVE — preserving
// every candidate would defeat the curation/GC purpose.
func DefaultPromptTemplate(action string, candidates []Candidate) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are the %q-action specialist for a long-running LLM substrate.\n\n", action)
	b.WriteString("Your job in this offline curation pass: identify which of the candidate ")
	b.WriteString("AP outcomes below are worth preserving as few-shot exemplars for future ")
	b.WriteString("calls of this same action. Selected exemplars will be retrieved (by ")
	b.WriteString("similarity) and prepended to future warm-path prompts.\n\n")

	b.WriteString("Selection criteria for a good exemplar:\n")
	b.WriteString("  - The candidate's output is correct AND well-reasoned (not noise, refusal, or lucky guess).\n")
	b.WriteString("  - The reasoning pattern would generalize to other questions of the same kind.\n")
	b.WriteString("  - The exemplar is DIVERSE relative to other selections — favor variety in subject / approach.\n")
	b.WriteString("  - Skip candidates where the output is unusually short or unusually long for its question.\n\n")

	b.WriteString("Be SELECTIVE. If 20 candidates are below, pick 5-10. If most are mediocre, pick fewer.\n\n")

	b.WriteString("Reply with ONLY a JSON object in this exact shape (no prose, no markdown):\n")
	b.WriteString(`{"exemplars": [{"case_id": "<id>", "reason": "<one short sentence>"}, ...]}` + "\n\n")

	fmt.Fprintf(&b, "Candidates (%d total):\n\n", len(candidates))
	for i, c := range candidates {
		fmt.Fprintf(&b, "─── Candidate %d ───\n", i+1)
		fmt.Fprintf(&b, "case_id: %s\n", c.CaseID)
		fmt.Fprintf(&b, "orchestrator: %s\n", c.OrchestratorName)
		fmt.Fprintf(&b, "score: %.2f\n", c.Score)
		fmt.Fprintf(&b, "prompt:\n%s\n", indent(c.Prompt, "  "))
		fmt.Fprintf(&b, "output:\n%s\n\n", indent(c.Output, "  "))
	}

	b.WriteString("Reply with the JSON object only.")
	return b.String()
}

// indent prefixes every line of s with prefix. Used to nest
// multi-line prompt/output under the candidate header so the model
// can clearly delimit one candidate from the next.
func indent(s, prefix string) string {
	if s == "" {
		return prefix + "(empty)"
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
