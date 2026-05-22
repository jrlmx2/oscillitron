// CLAUDE GENERATED
package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// scriptAdapter returns a pre-recorded sequence of answers. Each
// Execute call pops the next answer; concurrent calls share the
// sequence safely via mutex. Useful for deterministic Vote tests.
type scriptAdapter struct {
	mu      sync.Mutex
	answers []string
	idx     int
	calls   atomic.Int64
	err     error
}

func (s *scriptAdapter) Name() string { return "script" }

func (s *scriptAdapter) Evaluate(_ context.Context, env session.Envelope) (session.Envelope, error) {
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess, Confidence: 1.0}
	return env, nil
}

func (s *scriptAdapter) Execute(_ context.Context, env session.Envelope) (session.Envelope, error) {
	s.calls.Add(1)
	if s.err != nil {
		return env, s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.answers) {
		return env, errors.New("script exhausted")
	}
	ans := s.answers[s.idx]
	s.idx++
	env.Execute = &session.Execute{
		Category: session.CategoryReturnResult,
		ReturnResult: &session.ReturnResultPayload{
			Result: session.Payload{Kind: "result", Content: ans},
		},
		TokensUsed: 10,
	}
	return env, nil
}

var _ adapter.Adapter = (*scriptAdapter)(nil)

func TestVote_RequiresAdapter(t *testing.T) {
	_, err := Vote{N: 3, Extractor: ExtractorFunc(func(s string) string { return s })}.
		Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with nil Adapter")
	}
}

func TestVote_RequiresPositiveN(t *testing.T) {
	a := &scriptAdapter{}
	_, err := Vote{Adapter: a, N: 0, Extractor: ExtractorFunc(func(s string) string { return s })}.
		Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with N=0")
	}
}

func TestVote_RequiresExtractor(t *testing.T) {
	a := &scriptAdapter{answers: []string{"A"}}
	_, err := Vote{Adapter: a, N: 1}.
		Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with nil Extractor")
	}
}

func TestVote_MajorityWins(t *testing.T) {
	a := &scriptAdapter{answers: []string{"A", "A", "B", "A", "C"}}
	// Extractor returns first character (trimmed).
	ext := ExtractorFunc(func(raw string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return ""
		}
		return string(raw[0])
	})
	v := Vote{NameStr: "vote", Adapter: a, N: 5, Extractor: ext}
	ans, err := v.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A (3 votes vs 1+1)", ans.Extracted)
	}
	if ans.Calls != 5 {
		t.Errorf("Calls = %d, want 5", ans.Calls)
	}
	if a.calls.Load() != 5 {
		t.Errorf("adapter call count = %d, want 5", a.calls.Load())
	}
}

func TestVote_TieBreakAlphabetical(t *testing.T) {
	a := &scriptAdapter{answers: []string{"B", "B", "A", "A"}}
	ext := ExtractorFunc(func(raw string) string { return strings.TrimSpace(raw) })
	v := Vote{NameStr: "vote", Adapter: a, N: 4, Extractor: ext}
	ans, err := v.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A (alphabetical tie-break)", ans.Extracted)
	}
}

func TestVote_EmptyExtractions_ExcludedFromTally(t *testing.T) {
	// Three attempts: "A", "", "A". Empty doesn't count; majority = A.
	a := &scriptAdapter{answers: []string{"A", "", "A"}}
	ext := ExtractorFunc(func(raw string) string { return strings.TrimSpace(raw) })
	v := Vote{NameStr: "vote", Adapter: a, N: 3, Extractor: ext}
	ans, err := v.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A", ans.Extracted)
	}
}

func TestVote_AllExtractionsEmpty_ReturnsEmpty(t *testing.T) {
	a := &scriptAdapter{answers: []string{"", "", ""}}
	ext := ExtractorFunc(func(raw string) string { return strings.TrimSpace(raw) })
	v := Vote{NameStr: "vote", Adapter: a, N: 3, Extractor: ext}
	ans, err := v.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "" {
		t.Errorf("Extracted = %q, want empty (all extractions failed)", ans.Extracted)
	}
	if ans.Calls != 3 {
		t.Errorf("Calls = %d, want 3 (attempts still succeeded)", ans.Calls)
	}
}

func TestVote_AllAttemptsErrored_ReturnsError(t *testing.T) {
	a := &scriptAdapter{err: errors.New("substrate down")}
	ext := ExtractorFunc(func(raw string) string { return raw })
	v := Vote{NameStr: "vote", Adapter: a, N: 3, Extractor: ext}
	_, err := v.Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error when all attempts fail")
	}
	if !strings.Contains(err.Error(), "substrate down") {
		t.Errorf("err = %q, expected to wrap 'substrate down'", err.Error())
	}
}

func TestVote_TokensAccumulated(t *testing.T) {
	a := &scriptAdapter{answers: []string{"A", "A", "A"}}
	ext := ExtractorFunc(func(raw string) string { return raw })
	v := Vote{NameStr: "vote", Adapter: a, N: 3, Extractor: ext}
	ans, _ := v.Answer(context.Background(), benchmark.Case{ID: "x"})
	if ans.TokensUsed != 30 {
		t.Errorf("TokensUsed = %d, want 30 (3 × 10)", ans.TokensUsed)
	}
}
