package cost

import (
	"math"
	"sync"
	"testing"
	"time"
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

func TestHardwarePricingCost(t *testing.T) {
	hp := HardwarePricing{USDPerHour: 3.60}
	// 1 hour → $3.60
	if got := hp.Cost(time.Hour); !eq(got, 3.60) {
		t.Errorf("Cost(1h) = %f, want 3.60", got)
	}
	// 30 minutes → $1.80
	if got := hp.Cost(30 * time.Minute); !eq(got, 1.80) {
		t.Errorf("Cost(30m) = %f, want 1.80", got)
	}
	// 0 duration → $0
	if got := hp.Cost(0); !eq(got, 0) {
		t.Errorf("Cost(0) = %f, want 0", got)
	}
}

func TestRecordWithDurationHardwareCost(t *testing.T) {
	// $3.60/hr hardware, cheap token pricing, frontier expensive
	tr := New(
		Pricing{InputUSDPerMTok: 15, OutputUSDPerMTok: 60},
		HardwarePricing{USDPerHour: 3.60},
	)
	tr.Register("local", Pricing{InputUSDPerMTok: 0, OutputUSDPerMTok: 0})

	e := tr.RecordWithDuration("local", 1_000_000, 1_000_000, 10*time.Minute)

	// token cost = 0 (free local model)
	if !eq(e.ActualUSD, 0) {
		t.Errorf("ActualUSD = %f, want 0", e.ActualUSD)
	}
	// hardware cost = 10min × $3.60/hr = $0.60
	if !eq(e.HardwareCostUSD, 0.60) {
		t.Errorf("HardwareCostUSD = %f, want 0.60", e.HardwareCostUSD)
	}
	// frontier = 15 + 60 = 75
	if !eq(e.FrontierUSD, 75.0) {
		t.Errorf("FrontierUSD = %f, want 75.0", e.FrontierUSD)
	}
	// savings = frontier - max(actual, hwCost) = 75 - 0.60 = 74.40
	if !eq(e.SavingsUSD, 74.40) {
		t.Errorf("SavingsUSD = %f, want 74.40", e.SavingsUSD)
	}
	if e.Duration != 10*time.Minute {
		t.Errorf("Duration = %v, want 10m", e.Duration)
	}
}

func TestRecordWithDurationConservativeSavings(t *testing.T) {
	// When hardware cost exceeds token cost, savings use the higher value.
	tr := New(
		Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 10},
		HardwarePricing{USDPerHour: 360}, // expensive rig
	)
	tr.Register("local", Pricing{InputUSDPerMTok: 0.5, OutputUSDPerMTok: 0.5})

	e := tr.RecordWithDuration("local", 1_000_000, 0, time.Hour)

	// token cost = 0.5
	if !eq(e.ActualUSD, 0.5) {
		t.Errorf("ActualUSD = %f, want 0.5", e.ActualUSD)
	}
	// hardware = 1hr × $360/hr = $360
	if !eq(e.HardwareCostUSD, 360.0) {
		t.Errorf("HardwareCostUSD = %f, want 360.0", e.HardwareCostUSD)
	}
	// frontier = 10
	frontier := 10.0
	if !eq(e.FrontierUSD, frontier) {
		t.Errorf("FrontierUSD = %f, want %f", e.FrontierUSD, frontier)
	}
	// savings = 10 - 360 = -350 (negative = hardware cost exceeds frontier)
	if !eq(e.SavingsUSD, frontier-360.0) {
		t.Errorf("SavingsUSD = %f, want %f", e.SavingsUSD, frontier-360.0)
	}
}

func TestRecordWithoutHardwarePricingIgnoresDuration(t *testing.T) {
	tr := New(Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 10})
	tr.Register("m", Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 1})

	e := tr.RecordWithDuration("m", 1_000_000, 1_000_000, 5*time.Minute)

	if !eq(e.HardwareCostUSD, 0) {
		t.Errorf("HardwareCostUSD = %f, want 0 (no HardwarePricing configured)", e.HardwareCostUSD)
	}
	// savings should use token cost only
	if !eq(e.SavingsUSD, 18.0) { // frontier 20 - actual 2
		t.Errorf("SavingsUSD = %f, want 18.0", e.SavingsUSD)
	}
	if e.Duration != 5*time.Minute {
		t.Errorf("Duration = %v, want 5m", e.Duration)
	}
}

func TestSummaryAggregatesHardwareCosts(t *testing.T) {
	tr := New(
		Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 10},
		HardwarePricing{USDPerHour: 36},
	)
	tr.Register("local", Pricing{InputUSDPerMTok: 0, OutputUSDPerMTok: 0})

	tr.RecordWithDuration("local", 1_000_000, 0, 10*time.Minute) // hw = 6.0
	tr.RecordWithDuration("local", 1_000_000, 0, 20*time.Minute) // hw = 12.0

	s := tr.Summary()
	if !eq(s.TotalHardwareCostUSD, 18.0) {
		t.Errorf("TotalHardwareCostUSD = %f, want 18.0", s.TotalHardwareCostUSD)
	}
	if s.TotalDuration != 30*time.Minute {
		t.Errorf("TotalDuration = %v, want 30m", s.TotalDuration)
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
