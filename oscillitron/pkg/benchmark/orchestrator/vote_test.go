// CLAUDE GENERATED
package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// captureTracer collects emitted events for test assertions.
type captureTracer struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	level slog.Level
	name  string
	attrs map[string]any
}

func (c *captureTracer) Event(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := make(map[string]any, len(attrs))
	// Include correlation attrs so tests can assert on case +
	// orchestrator + attempt_idx regardless of which layer set them.
	for _, p := range trace.CorrelationFrom(ctx) {
		m[p.Key] = p.Value
	}
	for _, a := range attrs {
		m[a.Key] = a.Value.Any()
	}
	c.events = append(c.events, capturedEvent{level: level, name: name, attrs: m})
}

func (c *captureTracer) byName(name string) []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedEvent
	for _, e := range c.events {
		if e.name == name {
			out = append(out, e)
		}
	}
	return out
}

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

// --- Per-attempt events + tally ---

func TestVote_EmitsPerAttemptEvents(t *testing.T) {
	tracer := &captureTracer{}
	a := &scriptAdapter{answers: []string{"A", "B", "A"}}
	ext := ExtractorFunc(func(raw string) string { return strings.TrimSpace(raw) })
	v := Vote{NameStr: "vote-3-test", Adapter: a, N: 3, Extractor: ext, Tracer: tracer}

	_, err := v.Answer(context.Background(), benchmark.Case{ID: "c-001"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}

	starts := tracer.byName("vote.attempt_start")
	if len(starts) != 3 {
		t.Errorf("vote.attempt_start count = %d, want 3", len(starts))
	}
	dones := tracer.byName("vote.attempt_done")
	if len(dones) != 3 {
		t.Errorf("vote.attempt_done count = %d, want 3", len(dones))
	}
	// Each done event should carry the extracted answer + duration +
	// tokens. case/orchestrator are correlation-stamped by
	// benchmark.Run (not by Vote directly); they appear in unit
	// tests only when the test explicitly stamps the ctx.
	for i, e := range dones {
		extracted, ok := e.attrs["extracted"].(string)
		if !ok || (extracted != "A" && extracted != "B") {
			t.Errorf("done[%d] extracted = %v, want A or B", i, e.attrs["extracted"])
		}
		if tokens, _ := e.attrs["tokens"].(int64); tokens != 10 {
			t.Errorf("done[%d] tokens = %v, want 10", i, e.attrs["tokens"])
		}
		// Vote stamps attempt_idx itself.
		if e.attrs["attempt_idx"] == nil {
			t.Errorf("done[%d] missing attempt_idx correlation", i)
		}
	}
}

func TestVote_EmitsTallyWithDistribution(t *testing.T) {
	tracer := &captureTracer{}
	a := &scriptAdapter{answers: []string{"A", "A", "B", "A", "C"}}
	ext := ExtractorFunc(func(raw string) string { return strings.TrimSpace(raw) })
	v := Vote{NameStr: "vote-5", Adapter: a, N: 5, Extractor: ext, Tracer: tracer}

	_, err := v.Answer(context.Background(), benchmark.Case{ID: "c-001"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	tallies := tracer.byName("vote.tally")
	if len(tallies) != 1 {
		t.Fatalf("vote.tally count = %d, want 1", len(tallies))
	}
	tally := tallies[0]
	if tally.attrs["winning_answer"] != "A" {
		t.Errorf("winning_answer = %v, want A", tally.attrs["winning_answer"])
	}
	if got, _ := tally.attrs["winning_votes"].(int64); got != 3 {
		t.Errorf("winning_votes = %v, want 3", tally.attrs["winning_votes"])
	}
	// Distribution string is sorted alphabetically.
	if got := tally.attrs["distribution"]; got != "A=3,B=1,C=1" {
		t.Errorf("distribution = %v, want A=3,B=1,C=1", got)
	}
	if got, _ := tally.attrs["attempts"].(int64); got != 5 {
		t.Errorf("attempts = %v, want 5", got)
	}
	if got, _ := tally.attrs["successes"].(int64); got != 5 {
		t.Errorf("successes = %v, want 5", got)
	}
}

func TestVote_EmitsAttemptErrorOnFailure(t *testing.T) {
	tracer := &captureTracer{}
	a := &scriptAdapter{err: errors.New("substrate boom")}
	ext := ExtractorFunc(func(raw string) string { return raw })
	v := Vote{NameStr: "vote", Adapter: a, N: 2, Extractor: ext, Tracer: tracer}

	_, err := v.Answer(context.Background(), benchmark.Case{ID: "c-001"})
	if err == nil {
		t.Fatal("expected error when all attempts fail")
	}
	errs := tracer.byName("vote.attempt_error")
	if len(errs) != 2 {
		t.Errorf("vote.attempt_error count = %d, want 2", len(errs))
	}
	for i, e := range errs {
		if msg, _ := e.attrs["err"].(string); !strings.Contains(msg, "substrate boom") {
			t.Errorf("attempt_error[%d] err = %v, want to contain 'substrate boom'", i, e.attrs["err"])
		}
	}
	// No tally fires when all attempts fail (function returns early).
	if got := len(tracer.byName("vote.tally")); got != 0 {
		t.Errorf("expected no tally when all attempts fail; got %d", got)
	}
}

func TestVote_TallyOnAllExtractionsEmpty(t *testing.T) {
	tracer := &captureTracer{}
	a := &scriptAdapter{answers: []string{"", "", ""}}
	ext := ExtractorFunc(func(raw string) string { return strings.TrimSpace(raw) })
	v := Vote{NameStr: "vote", Adapter: a, N: 3, Extractor: ext, Tracer: tracer}

	_, err := v.Answer(context.Background(), benchmark.Case{ID: "c-001"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	tallies := tracer.byName("vote.tally")
	if len(tallies) != 1 {
		t.Fatalf("vote.tally count = %d, want 1 (empty-extractions path)", len(tallies))
	}
	if got := tallies[0].attrs["distribution"]; got != "all-extractions-empty" {
		t.Errorf("distribution = %v, want 'all-extractions-empty'", got)
	}
}

func TestVote_NoTracer_StillWorks(t *testing.T) {
	a := &scriptAdapter{answers: []string{"A", "A"}}
	ext := ExtractorFunc(func(raw string) string { return raw })
	v := Vote{NameStr: "vote", Adapter: a, N: 2, Extractor: ext} // no Tracer
	if _, err := v.Answer(context.Background(), benchmark.Case{ID: "x"}); err != nil {
		t.Fatalf("Answer with nil Tracer: %v", err)
	}
}
