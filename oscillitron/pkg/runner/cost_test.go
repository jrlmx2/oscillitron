// CLAUDE GENERATED
package runner

import (
	"context"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

func TestRunnerStampsCostFromTracker(t *testing.T) {
	topo := topology.New("only")
	topo.AddNode("only")

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"only": oscillator.New("only",
			stub.New("test-model", stub.ModeDone).WithTokens(1_000_000, 500_000),
			nil),
	}

	// Frontier: $1/MTok in, $2/MTok out -> $1 + $1 = $2 per call.
	// Test model: $0.10/MTok in, $0.20/MTok out -> $0.10 + $0.10 = $0.20 per call.
	tracker := cost.New(cost.Pricing{InputUSDPerMTok: 1.0, OutputUSDPerMTok: 2.0})
	tracker.Register("test-model", cost.Pricing{InputUSDPerMTok: 0.10, OutputUSDPerMTok: 0.20})

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(4),
		Initial:     session.Envelope{ID: "init", Objective: "test"},
		Tracker:     tracker,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Chain) != 1 {
		t.Fatalf("chain len = %d, want 1", len(res.Chain))
	}
	got := res.Chain[0]
	if got.Trace.CostUSD < 0.19 || got.Trace.CostUSD > 0.21 {
		t.Errorf("Trace.CostUSD = %v, want ~0.20", got.Trace.CostUSD)
	}
	if got.Trace.CostVsFrontierBaselineUSD < 1.99 || got.Trace.CostVsFrontierBaselineUSD > 2.01 {
		t.Errorf("Trace.CostVsFrontierBaselineUSD = %v, want ~2.00", got.Trace.CostVsFrontierBaselineUSD)
	}
	if got.Trace.TokensInput != 1_000_000 || got.Trace.TokensOutput != 500_000 {
		t.Errorf("Trace tokens = (%d, %d), want (1000000, 500000)",
			got.Trace.TokensInput, got.Trace.TokensOutput)
	}

	// Tracker summary should also see the savings.
	sum := tracker.Summary()
	if sum.SavingsRatio() < 0.85 || sum.SavingsRatio() > 0.95 {
		t.Errorf("SavingsRatio = %v, want ~0.90", sum.SavingsRatio())
	}
}

func TestRunnerNilTrackerLeavesCostUnstamped(t *testing.T) {
	topo := topology.New("only")
	topo.AddNode("only")

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"only": oscillator.New("only",
			stub.New("test-model", stub.ModeDone).WithTokens(100, 200),
			nil),
	}

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(4),
		Initial:     session.Envelope{ID: "init"},
		// Tracker intentionally nil.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.Chain[0]
	if got.Trace.CostUSD != 0 || got.Trace.CostVsFrontierBaselineUSD != 0 {
		t.Errorf("nil tracker should leave cost zero: %+v", got.Trace)
	}
	// Tokens still flow through the oscillator copy.
	if got.Trace.TokensInput != 100 || got.Trace.TokensOutput != 200 {
		t.Errorf("oscillator should copy tokens regardless of tracker: %+v", got.Trace)
	}
}
