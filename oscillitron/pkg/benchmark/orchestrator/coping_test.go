// CLAUDE GENERATED
package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/cope"
	"github.com/jrlmx2/oscillitron/pkg/stakes"
)

// stubOrch returns a canned Answer; counts calls for assertion.
type stubOrch struct {
	name   string
	answer benchmark.Answer
	err    error
	calls  int
}

func (s *stubOrch) Name() string { return s.name }

func (s *stubOrch) Answer(_ context.Context, _ benchmark.Case) (benchmark.Answer, error) {
	s.calls++
	return s.answer, s.err
}

// --- Decision routing ---

func TestCoping_HighConfidence_Ships(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "A", Confidence: 0.95, Calls: 5, TokensUsed: 500,
	}}
	front := &stubOrch{name: "front", answer: benchmark.Answer{Extracted: "B"}}
	c := Coping{NameStr: "cope", Inner: inner, Frontier: front}
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "c1", Stakes: stakes.High})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A (high conf → ship inner)", ans.Extracted)
	}
	if ans.CopeAction != string(cope.Ship) {
		t.Errorf("CopeAction = %q, want ship", ans.CopeAction)
	}
	if front.calls != 0 {
		t.Errorf("frontier should not have been called; calls=%d", front.calls)
	}
}

func TestCoping_MediumConfidence_ShipsWithCaveat(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "B", Confidence: 0.7,
	}}
	c := Coping{NameStr: "cope", Inner: inner}
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "c2", Stakes: stakes.Medium})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.CopeAction != string(cope.ShipWithCaveat) {
		t.Errorf("CopeAction = %q, want ship_with_caveat", ans.CopeAction)
	}
	if ans.Extracted != "B" {
		t.Errorf("Extracted = %q, want B (still ship)", ans.Extracted)
	}
}

func TestCoping_LowConfidence_HighStakes_EscalatesToFrontier(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "B", Confidence: 0.3, Calls: 5, TokensUsed: 500,
	}}
	front := &stubOrch{name: "front", answer: benchmark.Answer{
		Extracted: "A", Confidence: 0.95, Calls: 1, TokensUsed: 200,
	}}
	c := Coping{NameStr: "cope", Inner: inner, Frontier: front}
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "c3", Stakes: stakes.High})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if front.calls != 1 {
		t.Fatalf("frontier should be called once; calls=%d", front.calls)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A (frontier's answer)", ans.Extracted)
	}
	if ans.CopeAction != string(cope.Escalate) {
		t.Errorf("CopeAction = %q, want escalate", ans.CopeAction)
	}
	// Costs should be pooled.
	if ans.Calls != 6 {
		t.Errorf("Calls = %d, want 6 (5 inner + 1 frontier)", ans.Calls)
	}
	if ans.TokensUsed != 700 {
		t.Errorf("TokensUsed = %d, want 700 (500 + 200)", ans.TokensUsed)
	}
}

func TestCoping_LowConfidence_LowStakes_ShipsWithCaveat(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "C", Confidence: 0.3,
	}}
	front := &stubOrch{name: "front", answer: benchmark.Answer{Extracted: "Z"}}
	c := Coping{NameStr: "cope", Inner: inner, Frontier: front}
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "c4", Stakes: stakes.Low})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.CopeAction != string(cope.ShipWithCaveat) {
		t.Errorf("CopeAction = %q, want ship_with_caveat (low stakes; don't pay for frontier)", ans.CopeAction)
	}
	if front.calls != 0 {
		t.Errorf("frontier should not be called for low-stakes case; calls=%d", front.calls)
	}
	if ans.Extracted != "C" {
		t.Errorf("Extracted = %q, want C (kept inner's answer)", ans.Extracted)
	}
}

func TestCoping_NoFrontier_LowConfHighStakes_Refuses(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "B", Confidence: 0.3,
	}}
	c := Coping{NameStr: "cope", Inner: inner /* no Frontier */}
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "c5", Stakes: stakes.High})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.CopeAction != string(cope.Refuse) {
		t.Errorf("CopeAction = %q, want refuse (no frontier wired)", ans.CopeAction)
	}
	if ans.Extracted != "" {
		t.Errorf("Extracted = %q, want empty (refuse blanks the answer)", ans.Extracted)
	}
}

// --- Edge cases ---

func TestCoping_RequiresInner(t *testing.T) {
	c := Coping{NameStr: "cope"}
	_, err := c.Answer(context.Background(), benchmark.Case{ID: "c"})
	if err == nil {
		t.Fatal("expected error when Inner is nil")
	}
}

func TestCoping_FrontierError_FallsBackToInner(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "B", Confidence: 0.3,
	}}
	front := &stubOrch{name: "front", err: errors.New("frontier down")}
	c := Coping{NameStr: "cope", Inner: inner, Frontier: front}
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "c6", Stakes: stakes.High})
	if err != nil {
		t.Fatalf("Coping should not error when frontier fails; got %v", err)
	}
	if ans.Extracted != "B" {
		t.Errorf("Extracted = %q, want B (fell back to inner on frontier failure)", ans.Extracted)
	}
	if ans.CopeAction != "escalate-failed" {
		t.Errorf("CopeAction = %q, want escalate-failed", ans.CopeAction)
	}
}

func TestCoping_InnerError_Propagates(t *testing.T) {
	inner := &stubOrch{name: "inner", err: errors.New("inner down")}
	c := Coping{NameStr: "cope", Inner: inner}
	_, err := c.Answer(context.Background(), benchmark.Case{ID: "c"})
	if err == nil {
		t.Fatal("expected inner error to propagate")
	}
}

func TestCoping_ZeroValueRules_UsesDefaults(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "A", Confidence: 0.9,
	}}
	c := Coping{NameStr: "cope", Inner: inner /* zero Rules */}
	ans, _ := c.Answer(context.Background(), benchmark.Case{ID: "c"})
	if ans.CopeAction != string(cope.Ship) {
		t.Errorf("zero-value Rules should use defaults; got %q (want ship at conf=0.9)", ans.CopeAction)
	}
}

func TestCoping_NameImplemented(t *testing.T) {
	c := Coping{NameStr: "cope-vote-5"}
	if c.Name() != "cope-vote-5" {
		t.Errorf("Name = %q, want cope-vote-5", c.Name())
	}
}

// TestCoping_PartialRuleTable_FillsMissingDefaults guards bug #1
// from the 2026-05-23 code review: an operator who customized only
// one RuleTable field bypassed default-fill on the others. With
// only HighConfidence set, LowConfidence collapsed to 0 (caveat
// band erased) and EscalateAllowed stayed at false-zero (escalation
// silently disabled). Fix: per-field default-fill.
func TestCoping_PartialRuleTable_FillsMissingDefaults(t *testing.T) {
	inner := &stubOrch{name: "inner", answer: benchmark.Answer{
		Extracted: "B", Confidence: 0.6,
	}}
	front := &stubOrch{name: "front", answer: benchmark.Answer{
		Extracted: "A", Confidence: 0.95, Calls: 1, TokensUsed: 200,
	}}
	c := Coping{
		NameStr:  "cope",
		Inner:    inner,
		Frontier: front,
		Rules: cope.RuleTable{
			HighConfidence: 0.9, // customized; LowConfidence + EscalateAllowed at zero
		},
	}

	// Conf 0.6 sits between the default LowConfidence (0.5) and the
	// customized HighConfidence (0.9) — should land in the caveat
	// band. Pre-fix: LowConfidence collapsed to 0, making this Ship.
	ans, err := c.Answer(context.Background(), benchmark.Case{ID: "p1", Stakes: stakes.Medium})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.CopeAction != string(cope.ShipWithCaveat) {
		t.Errorf("CopeAction = %q, want ship_with_caveat (conf 0.6 should land in caveat band; pre-fix had LowConfidence=0 collapsing the band)", ans.CopeAction)
	}

	// Low-conf high-stakes case should escalate. Pre-fix:
	// EscalateAllowed=false zero-value blocked escalation even with
	// Frontier wired.
	inner.answer = benchmark.Answer{Extracted: "B", Confidence: 0.3}
	inner.calls = 0
	front.calls = 0
	ans, err = c.Answer(context.Background(), benchmark.Case{ID: "p2", Stakes: stakes.High})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.CopeAction != string(cope.Escalate) {
		t.Errorf("CopeAction = %q, want escalate (low conf + high stakes + frontier wired; pre-fix had EscalateAllowed=false-zero blocking)", ans.CopeAction)
	}
	if front.calls != 1 {
		t.Errorf("frontier should have been called once; calls=%d", front.calls)
	}
}
