// CLAUDE GENERATED
package benchmark

import (
	"context"
	"strings"
	"testing"
)

func TestPricing_Cost(t *testing.T) {
	p := Pricing{USDPerMTok: 4.50}
	if got := p.Cost(1_000_000); got != 4.50 {
		t.Errorf("Cost(1M) = %v, want 4.50", got)
	}
	if got := p.Cost(0); got != 0 {
		t.Errorf("Cost(0) = %v, want 0", got)
	}
	// Half a million tokens at $4.50/Mtok = $2.25
	if got := p.Cost(500_000); got != 2.25 {
		t.Errorf("Cost(500k) = %v, want 2.25", got)
	}
}

func TestPricingMap_UnpricedReturnsZero(t *testing.T) {
	m := PricingMap{"known": {USDPerMTok: 1.0}}
	if got := m.Cost("unknown", 1_000_000); got != 0 {
		t.Errorf("Cost(unknown) = %v, want 0", got)
	}
}

func TestParsePricingFlag(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantRate float64
		wantErr  bool
	}{
		{"haiku=1.30", "haiku", 1.30, false},
		{"orchestrator-vote-3-default=0.01", "orchestrator-vote-3-default", 0.01, false},
		{"hermes-local=0", "hermes-local", 0, false},
		{"missing-equals", "", 0, true},
		{"=4.50", "", 0, true},
		{"name=not-a-number", "", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			name, p, err := ParsePricingFlag(c.in)
			if c.wantErr {
				if err == nil {
					t.Errorf("expected error parsing %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != c.wantName {
				t.Errorf("name = %q, want %q", name, c.wantName)
			}
			if p.USDPerMTok != c.wantRate {
				t.Errorf("rate = %v, want %v", p.USDPerMTok, c.wantRate)
			}
		})
	}
}

func TestRun_Pricing_PopulatesAggregateCosts(t *testing.T) {
	// 3 cases, all pass. Single orchestrator uses 30k tokens total.
	// Pricing: own=$1/Mtok → actual=$0.030. Frontier=$10/Mtok → $0.300.
	// Savings ratio = (0.300 - 0.030) / 0.300 = 0.9 (= 90%).
	cases := makeCases(3)
	answers := map[string]string{"c00": "A", "c01": "A", "c02": "A"}
	o := &stubOrchestrator{name: "vote-3", answers: answers}

	pricing := PricingMap{"vote-3": {USDPerMTok: 1.0}}
	frontier := Pricing{USDPerMTok: 10.0}

	report, err := Run(context.Background(), RunnerConfig{
		Loader:          stubLoader{name: "test", cases: cases},
		Orchestrators:   []Orchestrator{o},
		Grader:          stubGrader{},
		Pricing:         pricing,
		FrontierPricing: frontier,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	a := report.Aggregates[0]
	if a.TotalTokens != 30 { // stub uses 10 tokens per call
		t.Fatalf("TotalTokens = %d, want 30", a.TotalTokens)
	}
	wantActual := 30.0 * 1.0 / 1_000_000
	wantFrontier := 30.0 * 10.0 / 1_000_000
	wantSavings := (wantFrontier - wantActual) / wantFrontier

	if a.TotalActualUSD != wantActual {
		t.Errorf("TotalActualUSD = %v, want %v", a.TotalActualUSD, wantActual)
	}
	if a.TotalFrontierUSD != wantFrontier {
		t.Errorf("TotalFrontierUSD = %v, want %v", a.TotalFrontierUSD, wantFrontier)
	}
	if a.SavingsRatio != wantSavings {
		t.Errorf("SavingsRatio = %v, want %v", a.SavingsRatio, wantSavings)
	}
}

func TestRun_NoPricing_LeavesCostsZero(t *testing.T) {
	report, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "test", cases: makeCases(2)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o", answers: map[string]string{
			"c00": "A", "c01": "A",
		}}},
		Grader: stubGrader{},
		// no Pricing, no FrontierPricing
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	a := report.Aggregates[0]
	if a.TotalActualUSD != 0 || a.TotalFrontierUSD != 0 || a.SavingsRatio != 0 {
		t.Errorf("expected zero cost fields without pricing; got %+v", a)
	}
}

func TestRun_OnlyFrontierPricing_StillComputesCounterfactual(t *testing.T) {
	// Operator wants to see "what would this cost on the frontier?"
	// without knowing the local rate. TotalActualUSD stays 0; the
	// counterfactual still computes. SavingsRatio = 1.0 (all of the
	// frontier cost is "saved" by not paying it).
	report, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "test", cases: makeCases(2)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "local", answers: map[string]string{
			"c00": "A", "c01": "A",
		}}},
		Grader:          stubGrader{},
		FrontierPricing: Pricing{USDPerMTok: 5.0},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	a := report.Aggregates[0]
	if a.TotalActualUSD != 0 {
		t.Errorf("TotalActualUSD = %v, want 0", a.TotalActualUSD)
	}
	if a.TotalFrontierUSD == 0 {
		t.Error("TotalFrontierUSD should be set")
	}
	if a.SavingsRatio != 1.0 {
		t.Errorf("SavingsRatio = %v, want 1.0 (all frontier cost is saved)", a.SavingsRatio)
	}
}

func TestPricing_JSONSerializes(t *testing.T) {
	// Verify aggregate cost fields land in the JSON output.
	report, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "test", cases: makeCases(2)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o", answers: map[string]string{
			"c00": "A", "c01": "A",
		}}},
		Grader:          stubGrader{},
		Pricing:         PricingMap{"o": {USDPerMTok: 2.0}},
		FrontierPricing: Pricing{USDPerMTok: 10.0},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var buf strings.Builder
	if err := WriteJSON(&buf, report); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	for _, key := range []string{`"total_actual_usd"`, `"total_frontier_usd"`, `"savings_ratio"`} {
		if !strings.Contains(buf.String(), key) {
			t.Errorf("expected JSON to contain %s; got:\n%s", key, buf.String())
		}
	}
}
