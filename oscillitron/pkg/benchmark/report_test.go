// CLAUDE GENERATED
package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sampleReport(t *testing.T) Report {
	t.Helper()
	return Report{
		BenchmarkName: "gpqa-diamond",
		StartedAt:     time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC),
		EndedAt:       time.Date(2026, 5, 22, 10, 5, 30, 0, time.UTC),
		Aggregates: []AggregateStats{
			{
				OrchestratorName: "frontier", Cases: 3, Successes: 2, Failures: 1,
				PassRate: 0.667, AvgScore: 0.667, TotalCalls: 3, TotalTokens: 9000,
			},
			{
				OrchestratorName: "vote-3", Cases: 3, Successes: 2, Failures: 1,
				PassRate: 0.667, AvgScore: 0.667, TotalCalls: 9, TotalTokens: 27000,
			},
		},
		Windows: []WindowStats{
			{
				EndCase: 2, Size: 3,
				PerOrchestrator: []WindowOrchestratorStats{
					{OrchestratorName: "frontier", PassRate: 0.667, AvgScore: 0.667},
					{OrchestratorName: "vote-3", PassRate: 0.667, AvgScore: 0.667},
				},
			},
		},
		Cases: []CaseResult{
			{
				CaseID: "c-001",
				Case:   Case{ID: "c-001", Prompt: "Q1?", Expected: "A", Metadata: map[string]string{"subdomain": "physics"}},
				Results: []OrchestratorResult{
					{
						OrchestratorName: "frontier",
						Answer:           Answer{Raw: "The answer is A", Extracted: "A", Calls: 1, TokensUsed: 3000},
						Verdict:          Verdict{GraderName: "multichoice", Pass: true, Score: 1.0},
					},
					{
						OrchestratorName: "vote-3",
						Answer:           Answer{Raw: "A\n---\nA\n---\nB", Extracted: "A", Calls: 3, TokensUsed: 9000},
						Verdict: Verdict{
							GraderName: "multichoice", Pass: true, Score: 1.0,
							Secondary: []Verdict{
								{GraderName: "secondary-judge", Pass: true, Score: 1.0, TokensUsed: 50},
							},
						},
					},
				},
			},
			{
				CaseID: "c-002",
				Case:   Case{ID: "c-002", Prompt: "Q2?", Expected: "B"},
				Results: []OrchestratorResult{
					{
						OrchestratorName: "frontier",
						Err:              errors.New("simulated failure"),
					},
					{
						OrchestratorName: "vote-3",
						Answer:           Answer{Extracted: "C"},
						Verdict:          Verdict{GraderName: "multichoice", Pass: false, Score: 0, Notes: "extracted=C expected=B"},
					},
				},
			},
		},
	}
}

func TestWriteJSON_RoundTrip(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var decoded reportJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v\n---\n%s", err, buf.String())
	}

	if decoded.BenchmarkName != "gpqa-diamond" {
		t.Errorf("BenchmarkName = %q, want gpqa-diamond", decoded.BenchmarkName)
	}
	if decoded.ElapsedMS != 330_000 { // 5m30s
		t.Errorf("ElapsedMS = %d, want 330000", decoded.ElapsedMS)
	}
	if len(decoded.Aggregates) != 2 {
		t.Fatalf("Aggregates count = %d, want 2", len(decoded.Aggregates))
	}
	if len(decoded.Windows) != 1 {
		t.Fatalf("Windows count = %d, want 1", len(decoded.Windows))
	}
	if len(decoded.Cases) != 2 {
		t.Fatalf("Cases count = %d, want 2", len(decoded.Cases))
	}
}

func TestWriteJSON_StringifiesError(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	// case-002 frontier had an error; verify it serialized as a string.
	body := buf.String()
	if !strings.Contains(body, `"error": "simulated failure"`) {
		t.Errorf("expected error field with stringified value; output:\n%s", body)
	}
	// Successful results should NOT have an error field (omitempty).
	if strings.Count(body, `"error":`) != 1 {
		t.Errorf("expected exactly 1 error field (the case-002 failure); got %d", strings.Count(body, `"error":`))
	}
}

func TestWriteJSON_PreservesSecondaryVerdicts(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"grader_name": "secondary-judge"`) {
		t.Errorf("expected secondary verdict in output:\n%s", buf.String())
	}
}

func TestWriteJSON_OmitsEmptyWindows(t *testing.T) {
	r := sampleReport(t)
	r.Windows = nil
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	if strings.Contains(buf.String(), `"windows"`) {
		t.Errorf("empty windows should be omitted; output:\n%s", buf.String())
	}
}

func TestWriteJSON_FieldNamesAreSnakeCase(t *testing.T) {
	// Tooling downstream (Python, JS) expects snake_case. Verify key
	// fields use snake_case in the output JSON.
	r := sampleReport(t)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	body := buf.String()
	wantKeys := []string{
		`"benchmark_name"`, `"started_at"`, `"elapsed_ms"`,
		`"orchestrator_name"`, `"pass_rate"`, `"total_tokens"`,
		`"case_id"`, `"end_case"`, `"per_orchestrator"`,
	}
	for _, k := range wantKeys {
		if !strings.Contains(body, k) {
			t.Errorf("expected key %s in output", k)
		}
	}
	// Should NOT contain Go-style CamelCase keys.
	for _, k := range []string{`"BenchmarkName"`, `"PassRate"`, `"OrchestratorName"`} {
		if strings.Contains(body, k) {
			t.Errorf("unexpected CamelCase key %s in output", k)
		}
	}
}

func TestWriteJSONFile_WritesAndIsValid(t *testing.T) {
	r := sampleReport(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := WriteJSONFile(path, r); err != nil {
		t.Fatalf("WriteJSONFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded reportJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode written file: %v\n---\n%s", err, string(data))
	}
	if decoded.BenchmarkName != r.BenchmarkName {
		t.Errorf("round-trip lost benchmark name")
	}
}

func TestWriteJSONFile_BadPath(t *testing.T) {
	err := WriteJSONFile("/nonexistent-dir/report.json", sampleReport(t))
	if err == nil {
		t.Fatal("expected error writing to missing directory")
	}
}
