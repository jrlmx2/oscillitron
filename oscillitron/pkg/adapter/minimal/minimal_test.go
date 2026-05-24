package minimal

import (
	"strings"
	"testing"
)

func TestProcessInstructions_MentionsLetters(t *testing.T) {
	// Smoke test: the minimal template must steer the model toward a
	// single closing letter. Without that, last-match-wins extraction
	// has nothing to match.
	want := []string{"single letter", "A, B, C, or D"}
	got := strings.ToLower(ProcessInstructions)
	for _, w := range want {
		if !strings.Contains(got, strings.ToLower(w)) {
			t.Errorf("ProcessInstructions missing %q; got:\n%s", w, ProcessInstructions)
		}
	}
}

// TestProcessInstructions_HasClosingPositionDiscipline guards
// bug #5 from the 2026-05-23 code review: the prompt used to say
// "Reply with the single letter…" but lacked a closing-position
// imperative. Multichoice grader uses last-match-wins regex over
// [A-D], so without anchoring the letter to the end of the
// response, models that emit "Answer: D. (Note this excludes
// option A.)" get last-match-wins → A → marked wrong.
//
// Empirically motivated: phi4-mini on 198-case Diamond produced
// 12 format_no_letter cases on the frontier arm — a slice of
// those were last-match-wins picking up a non-final letter. The
// closing-position imperative makes the fallback path (when
// response_format isn't honored) extract correctly.
func TestProcessInstructions_HasClosingPositionDiscipline(t *testing.T) {
	got := strings.ToLower(ProcessInstructions)
	// "end" + "response" together prevent matching e.g. "send the response".
	if !strings.Contains(got, "end your response") && !strings.Contains(got, "end with") {
		t.Errorf("ProcessInstructions missing closing-position imperative ('end your response with…'); got:\n%s", ProcessInstructions)
	}
}

func TestProcessInstructions_AsksForConfidence(t *testing.T) {
	// v3.x: the chain depends on the model self-reporting confidence.
	// If this assertion fails because the prompt evolved, double-check
	// that confidence still flows to ExtractConfidence somehow (or via
	// ProcessSchema enforcement).
	want := []string{"confidence", "0.0", "1.0"}
	got := strings.ToLower(ProcessInstructions)
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("ProcessInstructions missing %q (confidence is v3-chain-load-bearing); got:\n%s", w, ProcessInstructions)
		}
	}
}

func TestProcessInstructions_IsActuallyMinimal(t *testing.T) {
	// Pin the contract that minimal is much smaller than the default
	// envelope. If this fails because we made the template longer,
	// reconsider whether the additions are pulling weight — every
	// extra sentence is context tax on the small model.
	maxChars := 600
	if got := len(ProcessInstructions); got > maxChars {
		t.Errorf("ProcessInstructions is %d chars; should stay under %d (defeats the purpose otherwise)", got, maxChars)
	}
}

func TestProcessInstructions_NoJSONEnvelope(t *testing.T) {
	// The whole point of this package is to drop the legacy JSON
	// envelope (content/grounded_pass/contradictions/...). Belt-
	// and-suspenders: assert no envelope-shape keywords leak in.
	// Note: structured-output enforcement at the API level (via
	// ProcessSchema + AsResponseFormat) is a separate concern;
	// the schema lives outside the instruction text.
	banned := []string{
		"JSON",
		"json object",
		`"content"`,
		`"grounded_pass"`,
	}
	for _, b := range banned {
		if strings.Contains(ProcessInstructions, b) {
			t.Errorf("ProcessInstructions contains %q; minimal should not mention legacy envelope", b)
		}
	}
}

func TestProcessSchema_Shape(t *testing.T) {
	s := ProcessSchema()
	if got, _ := s["type"].(string); got != "object" {
		t.Errorf("schema type = %q, want object", got)
	}
	props, _ := s["properties"].(map[string]any)
	if _, ok := props["answer"]; !ok {
		t.Errorf("schema missing properties.answer")
	}
	if _, ok := props["confidence"]; !ok {
		t.Errorf("schema missing properties.confidence")
	}
	required, _ := s["required"].([]string)
	if len(required) != 2 {
		t.Errorf("schema required has %d fields, want 2", len(required))
	}
	if addl, _ := s["additionalProperties"].(bool); addl {
		t.Errorf("additionalProperties should be false (closed schema)")
	}
}

func TestAsResponseFormat_WrapsSchema(t *testing.T) {
	rf := AsResponseFormat("test_name", ProcessSchema())
	if got, _ := rf["type"].(string); got != "json_schema" {
		t.Errorf("response_format.type = %q, want json_schema", got)
	}
	js, _ := rf["json_schema"].(map[string]any)
	if got, _ := js["name"].(string); got != "test_name" {
		t.Errorf("json_schema.name = %q, want test_name", got)
	}
	if got, _ := js["strict"].(bool); !got {
		t.Errorf("json_schema.strict should be true")
	}
	if _, ok := js["schema"].(map[string]any); !ok {
		t.Errorf("json_schema.schema not embedded as map")
	}
}
