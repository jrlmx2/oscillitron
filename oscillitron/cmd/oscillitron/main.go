// CLAUDE GENERATED
// Demo runner. Wires brain-function specialists into a registry, fires
// a single root AP at the tree-walker, and prints the resolved tree.
//
// This is NOT a real Oscillitron deployment — it uses pkg/adapter/stub,
// so no model is actually called. The point is to prove the call-tree
// architecture compiles, runs, and produces a recomposed result. Real
// adapter (pkg/adapter/hermes) arrives in the next milestone.
//
// Tree exercised:
//
//	planning (root)
//	├── reasoning
//	│   └── retrieval (leaf)
//	└── critic (leaf)
//
// The planning specialist emits two SubAPs (reasoning, critic).
// Reasoning emits one further SubAP (retrieval). Retrieval and critic
// are leaves. The recomposer joins child outputs into each parent's
// composed Output, and the root's final Output is the recomposed
// product of the whole tree.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/composite"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/confidence"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/contradictions"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/repetition"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/registry"
	"github.com/jrlmx2/oscillitron/pkg/runner"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Planning specialist emits two SubAPs: one reasoning, one critic.
	planningSeeds := []session.SubAPSeed{
		{
			BrainFunction: session.BrainReasoning,
			Input:         session.Input{Type: "subap", Content: "derive the loop invariant"},
			OutputSchema:  "invariant_proven | counter_example",
		},
		{
			BrainFunction: session.BrainCritic,
			Input:         session.Input{Type: "subap", Content: "challenge the proof"},
			OutputSchema:  "objection | none",
		},
	}
	// Reasoning emits one SubAP: a retrieval for prior similar proofs.
	reasoningSeeds := []session.SubAPSeed{
		{
			BrainFunction: session.BrainRetrieval,
			Input:         session.Input{Type: "subap", Content: "find prior loop-invariant proofs"},
			OutputSchema:  "proofs_list",
		},
	}

	reg := registry.New()
	reg.Register(session.BrainPlanning, oscillator.New(
		"planner-1", session.BrainPlanning,
		stub.New("stub-planner", stub.ModeDone).
			WithConfidence(0.85).
			WithClassification("plan_emitted").
			WithSignals("split into reason+critic").
			WithSubAPs(planningSeeds...),
		logger,
	))
	reg.Register(session.BrainReasoning, oscillator.New(
		"reasoner-1", session.BrainReasoning,
		stub.New("stub-reasoner", stub.ModeDone).
			WithConfidence(0.75).
			WithClassification("invariant_proven").
			WithSignals("induction on i").
			WithSubAPs(reasoningSeeds...),
		logger,
	))
	reg.Register(session.BrainRetrieval, oscillator.New(
		"retriever-1", session.BrainRetrieval,
		stub.New("stub-retriever", stub.ModeDone).
			WithConfidence(0.9).
			WithClassification("proofs_list").
			WithSignals("3 prior proofs found"),
		logger,
	))
	reg.Register(session.BrainCritic, oscillator.New(
		"critic-1", session.BrainCritic,
		stub.New("stub-critic", stub.ModeDone).
			WithConfidence(0.7).
			WithClassification("none").
			WithSignals("base case checks out"),
		logger,
	))

	root := session.NewRoot(
		"root-001",
		session.BrainPlanning,
		"prove the loop invariant for: for i := 0; i < len(xs); i++ { ... }",
		"plan_emitted | counter_example",
		session.Budget{TokensRemaining: 8192, DepthRemaining: 5},
	)

	cfg := runner.Config{
		Registry:   reg,
		Recomposer: recomposer.Concat{Separator: "\n  + "},
		Inhibitor: composite.New(
			hardcap.New(20),
			confidence.New(0.3, 0.4, 3),
			repetition.New(5, 3),
			contradictions.New(3, 8),
		),
		Root:     root,
		Logger:   logger,
		MaxDepth: 8,
	}

	logger.Info("starting tree-walk demo",
		"root", root.ID,
		"root_brain_function", root.BrainFunction,
		"registered_functions", reg.Functions(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := runner.Run(ctx, cfg)
	if err != nil {
		logger.Error("runner failed", "err", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("====================================================")
	fmt.Printf("tree settled: reason=%s detail=%q\n", res.Reason, res.Detail)
	fmt.Println("----------------------------------------------------")
	if res.Root.Output != nil {
		fmt.Printf("root: %s (confidence=%.2f, classification=%s)\n",
			res.Root.ID, res.Root.Output.Confidence, res.Root.Output.Classification)
		fmt.Println()
		fmt.Println("recomposed content:")
		fmt.Println(res.Root.Output.Content)
		if len(res.Root.Output.Signals) > 0 {
			fmt.Println()
			fmt.Printf("aggregated signals: %v\n", res.Root.Output.Signals)
		}
	}
	fmt.Println("====================================================")
}
