package minimal

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProcessInstructions_TaskAgnostic(t *testing.T) {
	// The minimal template must NOT contain MCQ-specific framing.
	// Task specifics belong in the case prompt (built by the
	// per-benchmark loader), never in the universal instructions.
	got := strings.ToLower(ProcessInstructions)
	mcqForbidden := []string{
		"single letter",
		"a, b, c, or d",
		"multiple-choice",
		"multiple choice",
		"final character",
	}
	for _, w := range mcqForbidden {
		if strings.Contains(got, w) {
			t.Errorf("ProcessInstructions contains MCQ-specific phrase %q; the universal template should be task-agnostic — task specifics belong in the loader's case prompt", w)
		}
	}
}

func TestProcessInstructions_RequestsJSONShape(t *testing.T) {
	// The universal template asks for the {response, confidence}
	// JSON shape. Both keys must appear so the model has explicit
	// format guidance alongside the schema enforcement.
	for _, frag := range []string{`"response"`, `"confidence"`, "JSON"} {
		if !strings.Contains(ProcessInstructions, frag) {
			t.Errorf("ProcessInstructions missing %q; current text:\n%s", frag, ProcessInstructions)
		}
	}
}

func TestProcessInstructions_NoXMLTagRemnants(t *testing.T) {
	// The XML-tag iteration (PR #64) is retired. Guard against
	// regression: no <response> / <confidence> tag remnants in the
	// universal template.
	for _, frag := range []string{"<response>", "</response>", "<confidence>", "</confidence>"} {
		if strings.Contains(ProcessInstructions, frag) {
			t.Errorf("ProcessInstructions still contains XML-era fragment %q; the JSON path is canonical now", frag)
		}
	}
}

func TestProcessInstructions_IsActuallyMinimal(t *testing.T) {
	// Pin the contract that minimal stays much smaller than the
	// legacy envelope. If this fails because we made the template
	// longer, reconsider whether the additions are pulling weight —
	// every extra sentence is context tax on small substrates.
	maxChars := 400
	if got := len(ProcessInstructions); got > maxChars {
		t.Errorf("ProcessInstructions is %d chars; should stay under %d (defeats the purpose otherwise)", got, maxChars)
	}
}

func TestProcessInstructions_NoLegacyEnvelopeKeys(t *testing.T) {
	// The legacy envelope had grounded_pass / contradictions /
	// open_questions / etc. The minimal template carries none of
	// them; guard against drift.
	banned := []string{
		`"grounded_pass"`,
		`"contradictions"`,
		`"open_questions"`,
		`"content"`, // the v3.5 field name was renamed to "response"
		`"answer"`,  // legacy MCQ-specific name; we use "response"
	}
	for _, b := range banned {
		if strings.Contains(ProcessInstructions, b) {
			t.Errorf("ProcessInstructions contains %q; minimal should not mention legacy envelope keys or pre-rename field names", b)
		}
	}
}

func TestProcessSchema_Shape(t *testing.T) {
	s := ProcessSchema()
	if got, _ := s["type"].(string); got != "object" {
		t.Errorf("schema type = %q, want object", got)
	}
	props, _ := s["properties"].(map[string]any)
	if _, ok := props["response"]; !ok {
		t.Errorf("schema missing properties.response (the new field name)")
	}
	if _, ok := props["confidence"]; !ok {
		t.Errorf("schema missing properties.confidence")
	}
	if _, ok := props["answer"]; ok {
		t.Errorf("schema should NOT have properties.answer; renamed to 'response' in this PR")
	}
	required, _ := s["required"].([]string)
	if len(required) != 2 {
		t.Errorf("schema required has %d fields, want 2 (response, confidence)", len(required))
	}
	if addl, _ := s["additionalProperties"].(bool); addl {
		t.Errorf("additionalProperties should be false (closed schema)")
	}
}

func TestProcessSchema_ConfidenceBounded(t *testing.T) {
	s := ProcessSchema()
	props, _ := s["properties"].(map[string]any)
	conf, _ := props["confidence"].(map[string]any)
	if got, _ := conf["minimum"].(int); got != 0 {
		t.Errorf("confidence.minimum = %v, want 0", conf["minimum"])
	}
	if got, _ := conf["maximum"].(int); got != 1 {
		t.Errorf("confidence.maximum = %v, want 1", conf["maximum"])
	}
}

func TestAsResponseFormat_WrapsSchema(t *testing.T) {
	rf := AsResponseFormat("process_response", ProcessSchema())
	if got, _ := rf["type"].(string); got != "json_schema" {
		t.Errorf("response_format.type = %q, want json_schema", got)
	}
	js, _ := rf["json_schema"].(map[string]any)
	if got, _ := js["name"].(string); got != "process_response" {
		t.Errorf("json_schema.name = %q, want process_response", got)
	}
	if got, _ := js["strict"].(bool); !got {
		t.Errorf("json_schema.strict should be true")
	}
	if _, ok := js["schema"].(map[string]any); !ok {
		t.Errorf("json_schema.schema not embedded as map")
	}
}

func TestProcessSchema_MarshalsCleanJSON(t *testing.T) {
	// End-to-end: schema + wrapper marshal to valid JSON the engine
	// can consume.
	rf := AsResponseFormat("process_response", ProcessSchema())
	body, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(body, &roundtrip); err != nil {
		t.Fatalf("unmarshal roundtrip: %v", err)
	}
	if _, ok := roundtrip["json_schema"]; !ok {
		t.Errorf("roundtrip lost json_schema field")
	}
}
