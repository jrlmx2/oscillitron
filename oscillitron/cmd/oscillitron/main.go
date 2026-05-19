// CLAUDE GENERATED
// Demo runner. Wires three stub oscillators into a small topology,
// fires a single AP at the entry, and logs every hop.
//
// This is NOT a real Oscillitron deployment — it uses
// pkg/adapter/stub, so no model is actually called. The point is to
// prove the spike-passing architecture compiles, runs, and behaves
// sensibly. Real specialist adapters (pkg/adapter/hermes) arrive in
// the next milestone — see CLAUDE.md "Pointers for Claude Code's
// first session".
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/composite"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/confidence"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/contradictions"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/repetition"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/runner"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Topology: reasoner -> critic -> composer
	//
	// Nodes are typed by brain function, never by subject domain
	// (see ../../../CLAUDE.md "Architecture" — LOCKED 2026-05-18).
	//
	//   * reasoner (budget-exhausted): applies transformation / derives
	//     a partial result and hands it forward.
	//   * critic   (budget-exhausted): verifies, flags issues, hands
	//     forward — proves the chain isn't single-hop.
	//   * composer (done): terminal node that produces the final
	//     output artifact.
	topo := topology.New("reasoner")
	must(topo.AddEdge("reasoner", topology.Edge{To: "critic", Weight: 1.0}))
	must(topo.AddEdge("critic", topology.Edge{To: "composer", Weight: 1.0}))
	topo.AddNode("composer") // explicit sink

	oscMap := map[oscillator.ID]*oscillator.Oscillator{
		"reasoner": oscillator.New(
			"reasoner",
			stub.New("qwen3-coder-30b", stub.ModeBudgetExhausted).
				WithConfidence(0.6).
				WithSignals("loop bound looks suspicious"),
			logger,
		),
		"critic": oscillator.New(
			"critic",
			stub.New("qwen3-7b", stub.ModeBudgetExhausted).
				WithConfidence(0.7).
				WithSignals("confirmed: off-by-one at line 12"),
			logger,
		),
		"composer": oscillator.New(
			"composer",
			stub.New("qwen3-4b", stub.ModeDone).
				WithConfidence(0.85),
			logger,
		),
	}

	initial := session.Envelope{
		ID:             "initial-001",
		Type:           session.TypeAnalyze,
		Objective:      "review function f for off-by-one bugs",
		Classification: classification.Internal,
		Input: session.Input{
			Type:    "prompt",
			Content: "for i := 0; i <= len(xs); i++ { ... }",
		},
	}

	cfg := runner.Config{
		Topology:    topo,
		Oscillators: oscMap,
		Router:      rule.New(),
		Inhibitor: composite.New(
			hardcap.New(10),
			confidence.New(0.3, 0.4, 3),
			repetition.New(5, 3),
			contradictions.New(3, 8),
		),
		Initial:     initial,
		Logger:      logger,
		BufferSize:  4,
	}

	logger.Info("starting demo run",
		"entry", topo.Entry(),
		"nodes", topo.Nodes(),
		"initial_session", initial.ID,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := runner.Run(ctx, cfg)
	if err != nil {
		logger.Error("runner failed", "err", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("====================================================")
	fmt.Printf("chain settled: reason=%s detail=%q\n", result.Reason, result.Detail)
	fmt.Printf("chain length:  %d hops\n", len(result.Chain))
	fmt.Println("----------------------------------------------------")
	for i, env := range result.Chain {
		fmt.Printf("[%d] session=%s model=%s reason=%s\n",
			i, env.ID, env.Routing.Model, env.Routing.Reason)
		if env.Outcome != nil {
			fmt.Printf("    exit=%s confidence=%.2f verdict=%q\n",
				env.Outcome.ExitReason, env.Outcome.Confidence, env.Outcome.Verdict)
			if len(env.Outcome.Signals) > 0 {
				fmt.Printf("    signals=%v\n", env.Outcome.Signals)
			}
		}
	}
	fmt.Println("====================================================")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
