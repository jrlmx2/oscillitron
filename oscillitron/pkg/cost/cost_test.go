package cost

import (
	"math"
	"sync"
	"testing"
)

const epsilon = 1e-9

func eq(a, b float64) bool { return math.Abs(a-b) < epsilon }

func TestPricingCost(t *testing.T) {
	p := Pricing{InputUSDPerMTok: 3.0, OutputUSDPerMTok: 15.0}
	// 1M in + 1M out → 3 + 15 = 18
	if got := p.Cost(1_000_000, 1_000_000); !eq(got, 18.0) {
		t.Errorf("Cost(1M,1M) = %f, want 18.0", got)
	}
	// 500K in + 250K out → 1.5 + 3.75 = 5.25
	if got := p.Cost(500_000, 250_000); !eq(got, 5.25) {
		t.Errorf("Cost(500K,250K) = %f, want 5.25", got)
	}
}

func TestTrackerRegistersAndRecords(t *testing.T) {
	tr := New(Pricing{InputUSDPerMTok: 15, OutputUSDPerMTok: 60}) // frontier
	tr.Register("local-cheap", Pricing{InputUSDPerMTok: 0.5, OutputUSDPerMTok: 1.5})

	e := tr.Record("local-cheap", 1_000_000, 1_000_000)
	// actual: 0.5 + 1.5 = 2.0; frontier: 15 + 60 = 75; savings: 73.
	if !eq(e.ActualUSD, 2.0) {
		t.Errorf("ActualUSD = %f, want 2.0", e.ActualUSD)
	}
	if !eq(e.FrontierUSD, 75.0) {
		t.Errorf("FrontierUSD = %f, want 75.0", e.FrontierUSD)
	}
	if !eq(e.SavingsUSD, 73.0) {
		t.Errorf("SavingsUSD = %f, want 73.0", e.SavingsUSD)
	}
}

func TestUnpricedModelMarksAndContinues(t *testing.T) {
	tr := New(Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 10})
	e := tr.Record("mystery-model", 1_000_000, 0)
	if !eq(e.ActualUSD, 0) {
		t.Errorf("ActualUSD = %f, want 0 for unpriced model", e.ActualUSD)
	}
	if !eq(e.FrontierUSD, 10.0) {
		t.Errorf("FrontierUSD should still compute: %f", e.FrontierUSD)
	}
	if e.Model == "mystery-model" {
		t.Error("unpriced model should be annotated")
	}
}

func TestSummaryAggregates(t *testing.T) {
	tr := New(Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 10})
	tr.Register("cheap", Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 1})
	tr.Record("cheap", 1_000_000, 1_000_000)
	tr.Record("cheap", 500_000, 500_000)

	s := tr.Summary()
	if len(s.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(s.Entries))
	}
	if !eq(s.TotalActualUSD, 3.0) { // 2 + 1
		t.Errorf("TotalActualUSD = %f, want 3.0", s.TotalActualUSD)
	}
	if !eq(s.TotalFrontierUSD, 30.0) { // 20 + 10
		t.Errorf("TotalFrontierUSD = %f, want 30.0", s.TotalFrontierUSD)
	}
	if !eq(s.SavingsRatio(), 27.0/30.0) {
		t.Errorf("SavingsRatio = %f, want 0.9", s.SavingsRatio())
	}
}

func TestSummaryEmptyRatioIsZero(t *testing.T) {
	tr := New(Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 10})
	if got := tr.Summary().SavingsRatio(); got != 0 {
		t.Errorf("empty SavingsRatio = %f, want 0", got)
	}
}

func TestTrackerConcurrent(t *testing.T) {
	tr := New(Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 1})
	tr.Register("m", Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 1})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Record("m", 1000, 1000)
		}()
	}
	wg.Wait()
	if got := len(tr.Summary().Entries); got != 50 {
		t.Errorf("entries = %d, want 50", got)
	}
}
