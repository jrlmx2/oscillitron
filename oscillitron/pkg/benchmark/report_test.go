package benchmark

import (
	"bytes"
	"context"
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

func TestAnswerJSON_HasSEConfidence(t *testing.T) {
	// SEConfidence is additive and parallel to Confidence. When set it
	// serializes as snake_case "se_confidence"; when zero it is omitted
	// (omitempty), and the existing confidence/cope_action keys remain.
	r := Report{
		BenchmarkName: "gpqa-diamond",
		StartedAt:     time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC),
		EndedAt:       time.Date(2026, 6, 15, 10, 1, 0, 0, time.UTC),
		Cases: []CaseResult{
			{
				CaseID: "c-001",
				Results: []OrchestratorResult{
					{
						OrchestratorName: "vote-3",
						Answer: Answer{
							Extracted: "A", Confidence: 0.9, SEConfidence: 0.7, CopeAction: "ship",
						},
						Verdict: Verdict{GraderName: "multichoice", Pass: true, Score: 1.0},
					},
					{
						OrchestratorName: "frontier",
						Answer:           Answer{Extracted: "A", Confidence: 0.8}, // SEConfidence 0 → omitted
						Verdict:          Verdict{GraderName: "multichoice", Pass: true, Score: 1.0},
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, r); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	body := buf.String()
	compact := strings.ReplaceAll(body, " ", "") // pretty-printer adds spaces after colons
	if !strings.Contains(compact, `"se_confidence":0.7`) {
		t.Errorf("expected se_confidence:0.7 in output, got:\n%s", body)
	}
	if !strings.Contains(compact, `"confidence":0.9`) {
		t.Errorf("expected existing confidence key still present")
	}
	if !strings.Contains(compact, `"cope_action":"ship"`) {
		t.Errorf("expected existing cope_action key still present")
	}
	// The zero-SEConfidence answer (frontier) must omit the key. Confirm
	// there is exactly one se_confidence occurrence (the vote-3 answer).
	if n := strings.Count(body, "se_confidence"); n != 1 {
		t.Errorf("expected se_confidence omitted when zero: got %d occurrences", n)
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

func TestAppendCaseJSONL_OnePerLine(t *testing.T) {
	r := sampleReport(t)
	var buf bytes.Buffer
	for _, cr := range r.Cases {
		if err := AppendCaseJSONL(&buf, cr); err != nil {
			t.Fatalf("AppendCaseJSONL: %v", err)
		}
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	// Each line must be valid JSON on its own.
	for i, line := range lines {
		var decoded caseResultJSON
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", i, err, line)
		}
	}
	// First line is case c-001 with frontier + vote-3 results.
	if !strings.Contains(lines[0], `"case_id":"c-001"`) {
		t.Errorf("line 0 should be c-001; got %q", lines[0])
	}
	// Second line is c-002 with the simulated error stringified.
	if !strings.Contains(lines[1], `"case_id":"c-002"`) {
		t.Errorf("line 1 should be c-002")
	}
	if !strings.Contains(lines[1], `"error":"simulated failure"`) {
		t.Errorf("line 1 should contain stringified error; got %q", lines[1])
	}
}

func TestJSONLStreamer_AppendsAndFlushes(t *testing.T) {
	var buf bytes.Buffer
	flushCount := 0
	s := &JSONLStreamer{
		W:       &buf,
		Flusher: func() error { flushCount++; return nil },
	}
	r := sampleReport(t)
	for _, cr := range r.Cases {
		if err := s.AppendCase(cr); err != nil {
			t.Fatalf("AppendCase: %v", err)
		}
	}
	if flushCount != 2 {
		t.Errorf("flushCount = %d, want 2", flushCount)
	}
	if strings.Count(buf.String(), "\n") != 2 {
		t.Errorf("expected 2 newlines (one per case), got %d", strings.Count(buf.String(), "\n"))
	}
}

func TestJSONLStreamer_FlusherErrorSurfaces(t *testing.T) {
	s := &JSONLStreamer{
		W:       &bytes.Buffer{},
		Flusher: func() error { return errors.New("disk full") },
	}
	err := s.AppendCase(sampleReport(t).Cases[0])
	if err == nil || err.Error() != "disk full" {
		t.Errorf("expected flusher error to surface; got %v", err)
	}
}

func TestRun_OnCase_FiresPerCase_AfterWindow(t *testing.T) {
	cases := makeCases(7)
	answers := map[string]string{
		"c00": "A", "c01": "A", "c02": "A", "c03": "A",
		"c04": "A", "c05": "A", "c06": "A",
	}
	var got []CaseResult
	report, err := Run(context.Background(), RunnerConfig{
		Loader:            stubLoader{name: "test", cases: cases},
		Orchestrators:     []Orchestrator{&stubOrchestrator{name: "o", answers: answers}},
		Grader:            stubGrader{},
		SlidingWindowSize: 5,
		OnCase: func(cr CaseResult) error {
			got = append(got, cr)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 7 {
		t.Errorf("OnCase fired %d times, want 7", len(got))
	}
	// Order matches case order.
	for i, cr := range got {
		want := cases[i].ID
		if cr.CaseID != want {
			t.Errorf("got[%d].CaseID = %q, want %q", i, cr.CaseID, want)
		}
	}
	// Sanity: report still complete.
	if len(report.Cases) != 7 {
		t.Errorf("report Cases = %d, want 7", len(report.Cases))
	}
}

func TestRun_OnCase_ErrorRecordedNotFatal(t *testing.T) {
	report, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "test", cases: makeCases(3)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o", answers: map[string]string{
			"c00": "A", "c01": "A", "c02": "A",
		}}},
		Grader: stubGrader{},
		OnCase: func(cr CaseResult) error {
			return errors.New("disk failure for " + cr.CaseID)
		},
	})
	if err != nil {
		t.Fatalf("Run should not fail on OnCase error: %v", err)
	}
	if len(report.Cases) != 3 {
		t.Errorf("Cases = %d, want 3 (all should still run)", len(report.Cases))
	}
}
