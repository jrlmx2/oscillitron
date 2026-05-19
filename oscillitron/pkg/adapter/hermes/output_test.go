// CLAUDE GENERATED
package hermes

import "testing"

func TestExtractStructured_Happy(t *testing.T) {
	raw := "The loop walks one past the end.\n\n" +
		"```json\n" +
		`{"confidence": 0.82, "signals": ["off-by-one at line 1"], "open_questions": ["is xs allowed to be empty?"]}` + "\n" +
		"```\n"

	verdict, block := extractStructured(raw)
	if verdict != "The loop walks one past the end." {
		t.Errorf("verdict = %q", verdict)
	}
	if block.Confidence == nil || *block.Confidence != 0.82 {
		t.Errorf("confidence = %v", block.Confidence)
	}
	if len(block.Signals) != 1 || block.Signals[0] != "off-by-one at line 1" {
		t.Errorf("signals = %v", block.Signals)
	}
	if len(block.OpenQuestions) != 1 {
		t.Errorf("open_questions = %v", block.OpenQuestions)
	}
}

func TestExtractStructured_NoBlock(t *testing.T) {
	raw := "Just a plain answer."
	verdict, block := extractStructured(raw)
	if verdict != raw {
		t.Errorf("verdict mutated: %q", verdict)
	}
	if block.Confidence != nil || len(block.Signals) != 0 {
		t.Errorf("expected zero block, got %+v", block)
	}
}

func TestExtractStructured_MalformedBlockKeepsVerdict(t *testing.T) {
	raw := "Answer.\n\n```json\nnot valid json\n```\n"
	verdict, block := extractStructured(raw)
	if verdict != raw {
		t.Errorf("verdict should be unchanged on parse failure, got %q", verdict)
	}
	if block.Confidence != nil {
		t.Errorf("block should be zero on parse failure")
	}
}

func TestExtractStructured_PicksLastJSONBlock(t *testing.T) {
	// Hermes might emit a code-block within its answer (e.g. a code
	// suggestion) before the structured suffix. Make sure we pick the
	// last fence tagged json.
	raw := "Fix:\n\n```go\nfor i := 0; i < len(xs); i++\n```\n\nNotes.\n\n```json\n{\"confidence\": 0.5}\n```\n"
	verdict, block := extractStructured(raw)
	if block.Confidence == nil || *block.Confidence != 0.5 {
		t.Errorf("confidence = %v", block.Confidence)
	}
	// The go code block should still be present in the verdict.
	if !contains(verdict, "for i := 0; i < len(xs); i++") {
		t.Errorf("verdict lost the go code block: %q", verdict)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
