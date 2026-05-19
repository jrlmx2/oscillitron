// CLAUDE GENERATED
// Demo runner. Wires brain-function specialists into a registry, fires
// a single root AP at the tree-walker, and prints the resolved tree.
//
// Two modes:
//
//   - default (no flag) — uses pkg/adapter/stub. No model is called.
//     Proves the call-tree architecture compiles, runs, and produces a
//     recomposed result.
//   - --hermes <baseURL> — uses pkg/adapter/hermes against a real local
//     Hermes gateway (typically http://127.0.0.1:8642 after running
//     `hermes gateway start`). One Hermes instance serves every brain
//     function via SingleEndpoint; cost is tracked and printed at the
//     end. For the locked one-per-specialist shape, edit the endpoint
//     map by hand.
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
// are leaves. In Hermes mode, this tree is only honored if Hermes
// returns sub_aps in its structured envelope; weak models often emit
// the prose answer without decomposition, and the demo just prints
// whatever the leaf returns.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/cost"
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
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

func main() {
	hermesURL := flag.String("hermes", "", "Hermes gateway base URL (e.g. http://127.0.0.1:8642). If empty, runs with stub adapters.")
	hermesModel := flag.String("hermes-model", "", "Model identifier sent in the Hermes /v1/runs request body; empty leaves it to the Hermes instance's own config.")
	timeout := flag.Duration("timeout", 5*time.Minute, "Wall-clock ceiling on the whole tree walk.")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	tracer := trace.Slog{Logger: logger}

	var (
		costTracker *cost.Tracker
		usingHermes = *hermesURL != ""
	)
	if usingHermes {
		// Frontier baseline — placeholder; real numbers when an
		// adapter actually charges for them. Keeping the ratio
		// directional rather than precise.
		costTracker = cost.New(cost.Pricing{InputUSDPerMTok: 15, OutputUSDPerMTok: 60})
		if *hermesModel != "" {
			costTracker.Register(*hermesModel, cost.Pricing{InputUSDPerMTok: 0.5, OutputUSDPerMTok: 1.5})
		} else {
			costTracker.Register("hermes", cost.Pricing{InputUSDPerMTok: 0.5, OutputUSDPerMTok: 1.5})
		}
	}

	reg, err := buildRegistry(tracer, *hermesURL, *hermesModel, costTracker)
	if err != nil {
		logger.Error("registry build failed", "err", err)
		os.Exit(1)
	}

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
		Tracer:   tracer,
		MaxDepth: 8,
	}

	logger.Info("starting tree-walk demo",
		"root", root.ID,
		"root_brain_function", root.BrainFunction,
		"registered_functions", reg.Functions(),
		"adapter_mode", adapterMode(usingHermes),
	)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	res, err := runner.Run(ctx, cfg)
	if err != nil {
		logger.Error("runner failed", "err", err)
		os.Exit(1)
	}

	printResult(res, costTracker)
}

func adapterMode(usingHermes bool) string {
	if usingHermes {
		return "hermes"
	}
	return "stub"
}

// buildRegistry assembles the brain-function → oscillator table. In
// Hermes mode every oscillator points at the same hermes.Adapter
// (which itself looks up the endpoint by brain function). In stub
// mode each oscillator gets a pre-canned stub. Keeping the two
// branches in one builder makes it obvious that the registry shape
// is identical; only the adapter underneath changes.
func buildRegistry(tracer trace.Tracer, hermesURL, hermesModel string, costTracker *cost.Tracker) (*registry.Registry, error) {
	reg := registry.New()

	if hermesURL != "" {
		cfg := hermes.SingleEndpoint(hermesURL, hermesModel)
		cfg.Tracer = tracer
		cfg.Cost = costTracker
		ad, err := hermes.New(cfg)
		if err != nil {
			return nil, fmt.Errorf("build hermes adapter: %w", err)
		}
		for _, bf := range []session.BrainFunction{
			session.BrainPlanning,
			session.BrainReasoning,
			session.BrainRetrieval,
			session.BrainCritic,
		} {
			reg.Register(bf, oscillator.New(oscillator.ID(string(bf)+"-hermes"), bf, ad, tracer))
		}
		return reg, nil
	}

	// Stub mode: pre-canned outputs that exercise the call-tree shape.
	planningSeeds := []session.SubAPSeed{
		{BrainFunction: session.BrainReasoning, Input: session.Input{Type: "subap", Content: "derive the loop invariant"}, OutputSchema: "invariant_proven | counter_example"},
		{BrainFunction: session.BrainCritic, Input: session.Input{Type: "subap", Content: "challenge the proof"}, OutputSchema: "objection | none"},
	}
	reasoningSeeds := []session.SubAPSeed{
		{BrainFunction: session.BrainRetrieval, Input: session.Input{Type: "subap", Content: "find prior loop-invariant proofs"}, OutputSchema: "proofs_list"},
	}

	register := func(bf session.BrainFunction, name string, ad adapter.Adapter) {
		reg.Register(bf, oscillator.New(oscillator.ID(name), bf, ad, tracer))
	}
	register(session.BrainPlanning, "planner-1",
		stub.New("stub-planner", stub.ModeDone).
			WithConfidence(0.85).
			WithClassification("plan_emitted").
			WithSignals("split into reason+critic").
			WithSubAPs(planningSeeds...))
	register(session.BrainReasoning, "reasoner-1",
		stub.New("stub-reasoner", stub.ModeDone).
			WithConfidence(0.75).
			WithClassification("invariant_proven").
			WithSignals("induction on i").
			WithSubAPs(reasoningSeeds...))
	register(session.BrainRetrieval, "retriever-1",
		stub.New("stub-retriever", stub.ModeDone).
			WithConfidence(0.9).
			WithClassification("proofs_list").
			WithSignals("3 prior proofs found"))
	register(session.BrainCritic, "critic-1",
		stub.New("stub-critic", stub.ModeDone).
			WithConfidence(0.7).
			WithClassification("none").
			WithSignals("base case checks out"))
	return reg, nil
}

func printResult(res runner.Result, ct *cost.Tracker) {
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
		if len(res.Root.Output.Contradictions) > 0 {
			fmt.Printf("contradictions: %v\n", res.Root.Output.Contradictions)
		}
		if len(res.Root.Output.OpenQuestions) > 0 {
			fmt.Printf("open questions: %v\n", res.Root.Output.OpenQuestions)
		}
	}
	if ct != nil {
		s := ct.Summary()
		fmt.Println("----------------------------------------------------")
		fmt.Printf("cost: %d calls — actual=$%.6f, frontier=$%.6f, savings=$%.6f (%.1f%%)\n",
			len(s.Entries), s.TotalActualUSD, s.TotalFrontierUSD, s.TotalSavingsUSD, s.SavingsRatio()*100)
	}
	fmt.Println("====================================================")
}
