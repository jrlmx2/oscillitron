// CLAUDE GENERATED
package categorize

import (
	"errors"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// helpers

func passResult(orch, extracted string) benchmark.OrchestratorResult {
	return benchmark.OrchestratorResult{
		OrchestratorName: orch,
		Answer:           benchmark.Answer{Raw: "Reasoning... " + extracted, Extracted: extracted},
		Verdict:          benchmark.Verdict{Pass: true, Score: 1.0},
	}
}

func failResult(orch, raw, extracted string) benchmark.OrchestratorResult {
	return benchmark.OrchestratorResult{
		OrchestratorName: orch,
		Answer:           benchmark.Answer{Raw: raw, Extracted: extracted},
		Verdict:          benchmark.Verdict{Pass: false, Score: 0},
	}
}

func errResult(orch string, err error) benchmark.OrchestratorResult {
	return benchmark.OrchestratorResult{OrchestratorName: orch, Err: err}
}

func caseOf(id, expected string) benchmark.Case {
	return benchmark.Case{ID: id, Expected: expected}
}

// --- Classify ---

func TestClassify_Pass(t *testing.T) {
	got := Classify(caseOf("c1", "A"), passResult("o", "A"))
	if got != CategoryPass {
		t.Errorf("got %q, want pass", got)
	}
}

func TestClassify_AdapterErrorOverridesEverything(t *testing.T) {
	// Even if Verdict.Pass would say something else, an error
	// in Err means the orchestrator never produced a verdict.
	r := errResult("o", errors.New("substrate down"))
	got := Classify(caseOf("c1", "A"), r)
	if got != CategoryAdapterError {
		t.Errorf("got %q, want adapter_error", got)
	}
}

func TestClassify_EmptyResponse(t *testing.T) {
	got := Classify(caseOf("c1", "A"), failResult("o", "", ""))
	if got != CategoryEmptyResponse {
		t.Errorf("got %q, want empty_response", got)
	}
	// Whitespace-only also counts.
	got = Classify(caseOf("c1", "A"), failResult("o", "  \n\t  ", ""))
	if got != CategoryEmptyResponse {
		t.Errorf("whitespace-only: got %q, want empty_response", got)
	}
}

func TestClassify_RefusalPatterns(t *testing.T) {
	cases := []string{
		"I cannot answer that question.",
		"I can't help with this.",
		"I won't provide an answer.",
		"Sorry, I am unable to provide that.",
		"I will not answer this question.",
		"I'm unable to give you a response on that.",
	}
	for _, raw := range cases {
		t.Run(raw[:20], func(t *testing.T) {
			got := Classify(caseOf("c1", "A"), failResult("o", raw, ""))
			if got != CategoryRefusal {
				t.Errorf("raw=%q: got %q, want refusal", raw, got)
			}
		})
	}
}

func TestClassify_RefusalBeatsNoLetter(t *testing.T) {
	// A refusal often has no letter; should bucket as refusal
	// (more informative) not format_no_letter.
	r := failResult("o", "I cannot answer that.", "")
	got := Classify(caseOf("c1", "A"), r)
	if got != CategoryRefusal {
		t.Errorf("got %q, want refusal (more informative than no_letter)", got)
	}
}

func TestClassify_FormatNoLetter(t *testing.T) {
	// Real reasoning, no letter at the end.
	raw := "Looking at this problem, we have to consider the chemical equilibrium..."
	got := Classify(caseOf("c1", "A"), failResult("o", raw, ""))
	if got != CategoryFormatNoLetter {
		t.Errorf("got %q, want format_no_letter", got)
	}
}

func TestClassify_WrongLetter(t *testing.T) {
	r := failResult("o", "Reasoning... therefore C.", "C")
	got := Classify(caseOf("c1", "A"), r) // expected A, got C
	if got != CategoryWrongLetter {
		t.Errorf("got %q, want wrong_letter", got)
	}
}

func TestClassify_NonLetterExtractedFallsToOther(t *testing.T) {
	// Extractor returned something but it's not a single letter
	// (e.g., regex bug). Bucket as other.
	r := failResult("o", "Some text", "AB")
	got := Classify(caseOf("c1", "A"), r)
	if got != CategoryOther {
		t.Errorf("got %q, want other (multi-char extract)", got)
	}
}

// --- Aggregate ---

func TestAggregate_EmptyReport(t *testing.T) {
	s := Aggregate(benchmark.Report{})
	if len(s.Orchestrators) != 0 {
		t.Errorf("expected empty summary; got %d orchestrators", len(s.Orchestrators))
	}
}

func TestAggregate_BasicCounts(t *testing.T) {
	// 5 cases × 2 orchestrators.
	// orchestrator 0: pass, pass, format_no_letter, wrong_letter, refusal
	// orchestrator 1: pass, pass, pass, format_no_letter, empty_response
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{
			{OrchestratorName: "frontier"},
			{OrchestratorName: "vote-3"},
		},
		Cases: []benchmark.CaseResult{
			{
				Case:    caseOf("c1", "A"),
				Results: []benchmark.OrchestratorResult{passResult("frontier", "A"), passResult("vote-3", "A")},
			},
			{
				Case:    caseOf("c2", "B"),
				Results: []benchmark.OrchestratorResult{passResult("frontier", "B"), passResult("vote-3", "B")},
			},
			{
				Case: caseOf("c3", "C"),
				Results: []benchmark.OrchestratorResult{
					failResult("frontier", "Thinking...", ""),  // format_no_letter
					passResult("vote-3", "C"),
				},
			},
			{
				Case: caseOf("c4", "A"),
				Results: []benchmark.OrchestratorResult{
					failResult("frontier", "I think B.", "B"), // wrong_letter (expected A)
					failResult("vote-3", "Reasoning...", ""),  // format_no_letter
				},
			},
			{
				Case: caseOf("c5", "D"),
				Results: []benchmark.OrchestratorResult{
					failResult("frontier", "I cannot answer.", ""), // refusal
					failResult("vote-3", "", ""),                   // empty_response
				},
			},
		},
	}
	s := Aggregate(report)
	if len(s.Orchestrators) != 2 {
		t.Fatalf("len = %d, want 2", len(s.Orchestrators))
	}
	front := s.Orchestrators[0]
	vote := s.Orchestrators[1]

	wantFrontier := map[Category]int{
		CategoryPass:           2,
		CategoryFormatNoLetter: 1,
		CategoryWrongLetter:    1,
		CategoryRefusal:        1,
		CategoryEmptyResponse:  0,
		CategoryAdapterError:   0,
		CategoryOther:          0,
	}
	wantVote := map[Category]int{
		CategoryPass:           3,
		CategoryFormatNoLetter: 1,
		CategoryWrongLetter:    0,
		CategoryRefusal:        0,
		CategoryEmptyResponse:  1,
		CategoryAdapterError:   0,
		CategoryOther:          0,
	}
	for cat, want := range wantFrontier {
		if got := front.Counts[cat]; got != want {
			t.Errorf("frontier[%s] = %d, want %d", cat, got, want)
		}
	}
	for cat, want := range wantVote {
		if got := vote.Counts[cat]; got != want {
			t.Errorf("vote-3[%s] = %d, want %d", cat, got, want)
		}
	}
	if front.Total != 5 || vote.Total != 5 {
		t.Errorf("totals wrong: frontier=%d vote=%d", front.Total, vote.Total)
	}
}

func TestAggregate_InitialCountsAlwaysPresent(t *testing.T) {
	// Even when a category has zero occurrences, the key should
	// be present in the map so consumers (table printer, JSON
	// emitter) don't have to nil-check.
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "o"}},
		Cases: []benchmark.CaseResult{
			{Case: caseOf("c1", "A"), Results: []benchmark.OrchestratorResult{passResult("o", "A")}},
		},
	}
	s := Aggregate(report)
	for _, cat := range AllCategories() {
		if _, ok := s.Orchestrators[0].Counts[cat]; !ok {
			t.Errorf("category %q missing from counts map", cat)
		}
	}
}

// --- FormatTable ---

func TestFormatTable_NonEmpty(t *testing.T) {
	s := Summary{
		Orchestrators: []OrchestratorSummary{
			{
				OrchestratorName: "frontier",
				Counts: map[Category]int{
					CategoryPass:           8,
					CategoryFormatNoLetter: 42,
					CategoryWrongLetter:    9,
					CategoryRefusal:        0,
					CategoryEmptyResponse:  0,
					CategoryAdapterError:   1,
					CategoryOther:          0,
				},
				Total: 60,
			},
		},
	}
	got := FormatTable(s)
	for _, want := range []string{
		"Failure categorization",
		"category",
		"frontier",
		"pass",
		"format_no_letter",
		"42",
		"adapter_error",
		"total",
		"60",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, got)
		}
	}
}

func TestFormatTable_EmptySummary(t *testing.T) {
	got := FormatTable(Summary{})
	if !strings.Contains(got, "no orchestrators") {
		t.Errorf("expected empty-summary message; got %q", got)
	}
}

// --- Helpers ---

func TestPadRight(t *testing.T) {
	if got := padRight("abc", 5); got != "abc  " {
		t.Errorf("got %q, want 'abc  '", got)
	}
	if got := padRight("longstring", 5); got != "longstring " {
		t.Errorf("oversized input should still get a trailing space; got %q", got)
	}
}

func TestFormatInt(t *testing.T) {
	cases := map[int]string{0: "0", 1: "1", 42: "42", -5: "-5", 1234567: "1234567"}
	for in, want := range cases {
		if got := formatInt(in); got != want {
			t.Errorf("formatInt(%d) = %q, want %q", in, got, want)
		}
	}
}
