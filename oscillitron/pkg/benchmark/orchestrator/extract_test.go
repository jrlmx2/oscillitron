package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

func TestDeriveGoal_ReturnsFormatDescription(t *testing.T) {
	a := &scriptAdapter{
		answers: []string{"The answer must be exactly one letter: A, B, C, or D."},
	}
	tracer := &captureTracer{}
	c := benchmark.Case{ID: "q-001", Prompt: "What is 2+2?\n(A) 3 (B) 4 (C) 5 (D) 6"}

	goal := DeriveGoal(context.Background(), a, tracer, c)
	if goal == "" {
		t.Fatal("expected non-empty goal")
	}
	if goal != "The answer must be exactly one letter: A, B, C, or D." {
		t.Errorf("goal = %q, want format description", goal)
	}

	// Verify trace event.
	derived := tracer.byName("extractor.goal_derived")
	if len(derived) != 1 {
		t.Fatalf("extractor.goal_derived count = %d, want 1", len(derived))
	}
	if derived[0].attrs["case"] != "q-001" {
		t.Errorf("trace case = %v, want q-001", derived[0].attrs["case"])
	}
}

func TestDeriveGoal_FallsBackToRawText(t *testing.T) {
	// When the adapter returns plain text (not JSON), DeriveGoal
	// falls back to using the raw text as the goal.
	a := &scriptAdapter{
		answers: []string{"The answer must be a short paragraph."},
	}
	goal := DeriveGoal(context.Background(), a, nil, benchmark.Case{ID: "q-002", Prompt: "Explain X."})
	if goal != "The answer must be a short paragraph." {
		t.Errorf("goal = %q, want raw text fallback", goal)
	}
}

func TestDeriveGoal_ErrorReturnsEmpty(t *testing.T) {
	a := &scriptAdapter{err: errors.New("substrate down")}
	tracer := &captureTracer{}

	goal := DeriveGoal(context.Background(), a, tracer, benchmark.Case{ID: "q-003"})
	if goal != "" {
		t.Errorf("goal = %q, want empty on error", goal)
	}

	// Verify error trace event.
	errs := tracer.byName("extractor.goal_derive_error")
	if len(errs) != 1 {
		t.Fatalf("extractor.goal_derive_error count = %d, want 1", len(errs))
	}
}

func TestDeriveGoal_NilTracer(t *testing.T) {
	a := &scriptAdapter{
		answers: []string{"A number."},
	}
	// Nil tracer should not panic.
	goal := DeriveGoal(context.Background(), a, nil, benchmark.Case{ID: "q-004", Prompt: "What is pi?"})
	if goal == "" {
		t.Fatal("expected non-empty goal with nil tracer")
	}
}

func TestLLMExtractor_ExtractsAnswer(t *testing.T) {
	a := &scriptAdapter{
		answers:     []string{"C"},
		confidences: []float64{0.95},
	}
	tracer := &captureTracer{}
	ext := LLMExtractor{Adapter: a, Tracer: tracer}

	result := ext.Extract(context.Background(), "The answer must be one letter.", "I think the answer is C.")
	if result != "C" {
		t.Errorf("extracted = %q, want C", result)
	}

	events := tracer.byName("extractor.llm_extract")
	if len(events) != 1 {
		t.Fatalf("extractor.llm_extract count = %d, want 1", len(events))
	}
	if events[0].attrs["extracted"] != "C" {
		t.Errorf("trace extracted = %v, want C", events[0].attrs["extracted"])
	}
}

func TestLLMExtractor_ReturnsEmptyOnError(t *testing.T) {
	a := &scriptAdapter{err: errors.New("substrate down")}
	tracer := &captureTracer{}
	ext := LLMExtractor{Adapter: a, Tracer: tracer}

	result := ext.Extract(context.Background(), "one letter", "answer is B")
	if result != "" {
		t.Errorf("extracted = %q, want empty on error", result)
	}

	errs := tracer.byName("extractor.llm_extract_error")
	if len(errs) != 1 {
		t.Fatalf("extractor.llm_extract_error count = %d, want 1", len(errs))
	}
}

func TestLLMExtractor_EmptyResponseEmitsTrace(t *testing.T) {
	a := &scriptAdapter{
		answers: []string{""},
	}
	tracer := &captureTracer{}
	ext := LLMExtractor{Adapter: a, Tracer: tracer}

	result := ext.Extract(context.Background(), "one letter", "the answer is B")
	if result != "" {
		t.Errorf("extracted = %q, want empty", result)
	}

	empties := tracer.byName("extractor.extract_empty")
	if len(empties) != 1 {
		t.Fatalf("extractor.extract_empty count = %d, want 1", len(empties))
	}
}

func TestLLMExtractor_ImplementsExtractor(t *testing.T) {
	// Compile-time check (also in extract.go; this test documents
	// the contract explicitly in the test suite).
	var _ Extractor = LLMExtractor{}
}

func TestLLMExtractor_NilGovernor(t *testing.T) {
	a := &scriptAdapter{
		answers: []string{"D"},
	}
	ext := LLMExtractor{Adapter: a}
	result := ext.Extract(context.Background(), "one letter", "answer is D")
	if result != "D" {
		t.Errorf("extracted = %q, want D", result)
	}
}

func TestLLMExtractor_IntegrationWithGoal(t *testing.T) {
	goalAdapter := &scriptAdapter{answers: []string{
		"The answer must be exactly one letter: A, B, C, or D.",
	}}
	c := benchmark.Case{
		ID:       "integ-001",
		Prompt:   "Question: X?\nA) a\nB) b\nC) c\nD) d\nAnswer with a letter.",
		Expected: "C",
	}
	goal := DeriveGoal(context.Background(), goalAdapter, trace.Discard{}, c)
	if goal == "" {
		t.Fatal("DeriveGoal returned empty")
	}
	c.Goal = goal

	response := "After careful analysis, the answer is C because it best fits."

	extractAdapter := &scriptAdapter{answers: []string{"C"}}
	ext := LLMExtractor{Adapter: extractAdapter, Tracer: trace.Discard{}}
	extracted := ext.Extract(context.Background(), c.Goal, response)
	if extracted != "C" {
		t.Errorf("extracted = %q, want C", extracted)
	}
}
