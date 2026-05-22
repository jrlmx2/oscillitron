// CLAUDE GENERATED
package orchestrator

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

func TestSingle_RequiresAdapter(t *testing.T) {
	_, err := Single{NameStr: "s"}.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with nil Adapter")
	}
}

func TestSingle_HappyPath_NoExtractor(t *testing.T) {
	a := &scriptAdapter{answers: []string{"The answer is C"}}
	s := Single{NameStr: "s", Adapter: a}
	ans, err := s.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Raw != "The answer is C" {
		t.Errorf("Raw = %q", ans.Raw)
	}
	// Without an extractor, Extracted equals Raw.
	if ans.Extracted != ans.Raw {
		t.Errorf("Extracted = %q, want = Raw", ans.Extracted)
	}
	if ans.Calls != 1 {
		t.Errorf("Calls = %d, want 1", ans.Calls)
	}
	if ans.TokensUsed != 10 {
		t.Errorf("TokensUsed = %d, want 10", ans.TokensUsed)
	}
}

func TestSingle_AppliesExtractor(t *testing.T) {
	a := &scriptAdapter{answers: []string{"Reasoning... therefore the answer is B."}}
	ext := ExtractorFunc(func(raw string) string {
		// Trivial extractor for the test.
		if len(raw) > 0 {
			return string(raw[len(raw)-2])
		}
		return ""
	})
	s := Single{NameStr: "s", Adapter: a, Extractor: ext}
	ans, _ := s.Answer(context.Background(), benchmark.Case{ID: "x"})
	if ans.Extracted != "B" {
		t.Errorf("Extracted = %q, want B", ans.Extracted)
	}
}
