package minimal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
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

// --- PlanSchema tests ---

func TestPlanSchema_Shape(t *testing.T) {
	s := PlanSchema()
	if got, _ := s["type"].(string); got != "object" {
		t.Errorf("schema type = %q, want object", got)
	}
	props, _ := s["properties"].(map[string]any)
	if _, ok := props["sub_aps"]; !ok {
		t.Errorf("schema missing properties.sub_aps")
	}
	if _, ok := props["recompose"]; !ok {
		t.Errorf("schema missing properties.recompose")
	}
	required, _ := s["required"].([]string)
	if len(required) != 2 {
		t.Errorf("schema required has %d fields, want 2 (sub_aps, recompose)", len(required))
	}
	if addl, _ := s["additionalProperties"].(bool); addl {
		t.Errorf("additionalProperties should be false (closed schema)")
	}
}

func TestPlanSchema_RecomposeEnum(t *testing.T) {
	s := PlanSchema()
	props, _ := s["properties"].(map[string]any)
	recompose, _ := props["recompose"].(map[string]any)
	enum, ok := recompose["enum"].([]string)
	if !ok {
		t.Fatalf("recompose.enum is not []string; got %T", recompose["enum"])
	}
	want := []string{"pairwise", "sequential", "none"}
	if len(enum) != len(want) {
		t.Fatalf("recompose.enum has %d values, want %d", len(enum), len(want))
	}
	for i, v := range want {
		if enum[i] != v {
			t.Errorf("recompose.enum[%d] = %q, want %q", i, enum[i], v)
		}
	}
}

func TestPlanSchema_MarshalsCleanJSON(t *testing.T) {
	rf := AsResponseFormat("plan_response", PlanSchema())
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

// --- CritiqueSchema tests ---

func TestCritiqueSchema_Shape(t *testing.T) {
	s := CritiqueSchema()
	if got, _ := s["type"].(string); got != "object" {
		t.Errorf("schema type = %q, want object", got)
	}
	props, _ := s["properties"].(map[string]any)
	if _, ok := props["verdict"]; !ok {
		t.Errorf("schema missing properties.verdict")
	}
	if _, ok := props["issues"]; !ok {
		t.Errorf("schema missing properties.issues")
	}
	required, _ := s["required"].([]string)
	if len(required) != 2 {
		t.Errorf("schema required has %d fields, want 2 (verdict, issues)", len(required))
	}
	if addl, _ := s["additionalProperties"].(bool); addl {
		t.Errorf("additionalProperties should be false (closed schema)")
	}
}

func TestCritiqueSchema_VerdictEnum(t *testing.T) {
	s := CritiqueSchema()
	props, _ := s["properties"].(map[string]any)
	verdict, _ := props["verdict"].(map[string]any)
	enum, ok := verdict["enum"].([]string)
	if !ok {
		t.Fatalf("verdict.enum is not []string; got %T", verdict["enum"])
	}
	want := []string{"pass", "fail", "issues"}
	if len(enum) != len(want) {
		t.Fatalf("verdict.enum has %d values, want %d", len(enum), len(want))
	}
	for i, v := range want {
		if enum[i] != v {
			t.Errorf("verdict.enum[%d] = %q, want %q", i, enum[i], v)
		}
	}
}

func TestCritiqueSchema_MarshalsCleanJSON(t *testing.T) {
	rf := AsResponseFormat("critique_response", CritiqueSchema())
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

// --- AllPlaybookFormats tests ---

func TestAllPlaybookFormats_CoversAllPlaybooks(t *testing.T) {
	m := AllPlaybookFormats()
	expected := []session.Playbook{
		session.PlaybookPlan,
		session.PlaybookProcess,
		session.PlaybookCritique,
		session.PlaybookVerifyGrounded,
		session.PlaybookCompose,
	}
	for _, pb := range expected {
		rf, ok := m[pb]
		if !ok {
			t.Errorf("AllPlaybookFormats missing entry for %q", pb)
			continue
		}
		// Each entry should be a valid AsResponseFormat wrapper.
		if got, _ := rf["type"].(string); got != "json_schema" {
			t.Errorf("AllPlaybookFormats[%q].type = %q, want json_schema", pb, got)
		}
	}
	if len(m) != len(expected) {
		t.Errorf("AllPlaybookFormats has %d entries, want %d", len(m), len(expected))
	}
}
