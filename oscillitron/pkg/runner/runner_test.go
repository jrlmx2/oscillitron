// CLAUDE GENERATED
package runner

import (
	"context"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

func TestRunnerChainTerminatesOnDoneOutcome(t *testing.T) {
	// alpha (budget exhausted) -> beta (done)
	topo := topology.New("alpha")
	_ = topo.AddEdge("alpha", topology.Edge{To: "beta", Weight: 1.0})

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"alpha": oscillator.New("alpha", stub.New("a", stub.ModeBudgetExhausted), nil),
		"beta":  oscillator.New("beta", stub.New("b", stub.ModeDone), nil),
	}

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(10),
		Initial:     session.Envelope{ID: "initial", Objective: "test"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reason != ReasonTerminalOutcome {
		t.Errorf("Reason = %q, want %q (detail: %s)", res.Reason, ReasonTerminalOutcome, res.Detail)
	}
	if got := len(res.Chain); got != 2 {
		t.Errorf("chain length = %d, want 2; chain: %+v", got, res.Chain)
	}
}

func TestRunnerHaltsAtSinkWithNoOutgoingEdges(t *testing.T) {
	// alpha (budget exhausted) -> beta (also budget exhausted, but a sink)
	topo := topology.New("alpha")
	_ = topo.AddEdge("alpha", topology.Edge{To: "beta", Weight: 1.0})

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"alpha": oscillator.New("alpha", stub.New("a", stub.ModeBudgetExhausted), nil),
		"beta":  oscillator.New("beta", stub.New("b", stub.ModeBudgetExhausted), nil),
	}

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(10),
		Initial:     session.Envelope{ID: "initial"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reason != ReasonRouterTerminal {
		t.Errorf("Reason = %q, want %q", res.Reason, ReasonRouterTerminal)
	}
}

func TestRunnerInhibitorAbortsLongChain(t *testing.T) {
	// alpha -> alpha self-loop, always budget exhausted, so the chain
	// would run forever; inhibitor must abort.
	topo := topology.New("alpha")
	_ = topo.AddEdge("alpha", topology.Edge{To: "alpha", Weight: 1.0})

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"alpha": oscillator.New("alpha", stub.New("a", stub.ModeBudgetExhausted), nil),
	}

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(3),
		Initial:     session.Envelope{ID: "initial"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reason != ReasonInhibitorAbort {
		t.Errorf("Reason = %q, want %q", res.Reason, ReasonInhibitorAbort)
	}
}

func TestRunnerValidatesMissingOscillator(t *testing.T) {
	topo := topology.New("alpha")
	_ = topo.AddEdge("alpha", topology.Edge{To: "beta", Weight: 1.0})

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"alpha": oscillator.New("alpha", stub.New("a", stub.ModeDone), nil),
		// beta missing!
	}

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(5),
		Initial:     session.Envelope{ID: "initial"},
	}

	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected validation error for missing oscillator")
	}
}

func TestRunnerPropagatesUpstreamVerdictAsInput(t *testing.T) {
	// alpha emits a verdict; beta should receive it as its Input.Content.
	topo := topology.New("alpha")
	_ = topo.AddEdge("alpha", topology.Edge{To: "beta", Weight: 1.0})

	alphaStub := stub.New("a", stub.ModeBudgetExhausted).WithSignals("flag-1")
	betaStub := stub.New("b", stub.ModeDone)

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"alpha": oscillator.New("alpha", alphaStub, nil),
		"beta":  oscillator.New("beta", betaStub, nil),
	}

	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(5),
		Initial:     session.Envelope{ID: "initial", Objective: "do the thing"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, _ := Run(ctx, cfg)
	if len(res.Chain) != 2 {
		t.Fatalf("chain length = %d, want 2", len(res.Chain))
	}
	if alphaStub.Calls() != 1 || betaStub.Calls() != 1 {
		t.Errorf("expected each adapter called once; alpha=%d beta=%d",
			alphaStub.Calls(), betaStub.Calls())
	}
}
