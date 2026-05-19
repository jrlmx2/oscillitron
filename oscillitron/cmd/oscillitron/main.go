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
//     end.
//
// Configuration:
//
//   - CLI flags (--hermes, --hermes-model, --timeout) are the primary
//     surface and always take precedence when set.
//   - --config <path> loads a .properties file (or set OSCILLITRON_CONFIG
//     in the env). Properties file fills in any flag the user did not set
//     on the command line. See oscillitron.properties.example for the
//     supported keys; multi-endpoint Hermes (one process per brain
//     function) is configurable here via hermes.endpoints.<bf>.url.
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
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/config"
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
	cfgPath := flag.String("config", "", "Path to a .properties config file. Fills in any flag not set on the command line. Also reads OSCILLITRON_CONFIG when this flag is empty.")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	tracer := trace.Slog{Logger: logger}

	props, err := loadConfig(*cfgPath)
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}
	mergeConfig(props, hermesURL, hermesModel, timeout)

	endpoints, err := resolveEndpoints(props, *hermesURL, *hermesModel)
	if err != nil {
		logger.Error("endpoint resolution failed", "err", err)
		os.Exit(1)
	}
	usingHermes := len(endpoints) > 0

	var costTracker *cost.Tracker
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

	reg, err := buildRegistry(tracer, endpoints, costTracker)
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

// loadConfig reads the .properties file path resolved from flag,
// env, or — when both are empty — returns an empty bag.
func loadConfig(flagPath string) (config.Properties, error) {
	path := flagPath
	if path == "" {
		path = os.Getenv("OSCILLITRON_CONFIG")
	}
	if path == "" {
		return config.Properties{}, nil
	}
	return config.Load(path)
}

// mergeConfig fills in flag values that the user did NOT set on the
// command line from the properties bag. flag.Visit enumerates
// explicitly-set flags so we know which to leave alone.
func mergeConfig(props config.Properties, hermesURL, hermesModel *string, timeout *time.Duration) {
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if !set["hermes"] {
		if v := props.String("hermes.url", ""); v != "" {
			*hermesURL = v
		}
	}
	if !set["hermes-model"] {
		if v := props.String("hermes.model", ""); v != "" {
			*hermesModel = v
		}
	}
	if !set["timeout"] {
		if v := props.Duration("timeout", 0); v > 0 {
			*timeout = v
		}
	}
}

// resolveEndpoints builds the brain-function → endpoint map. Order
// of precedence (highest wins):
//
//  1. hermes.endpoints.<bf>.url in the properties file — explicit
//     per-brain-function endpoints (the locked one-per-specialist
//     shape).
//  2. hermes.url + hermes.model OR the --hermes / --hermes-model
//     flags — single-endpoint fallback. Bound to every well-known
//     brain function.
//  3. None of the above → empty map → stub mode.
func resolveEndpoints(props config.Properties, hermesURL, hermesModel string) (map[session.BrainFunction]hermes.Endpoint, error) {
	endpoints := map[session.BrainFunction]hermes.Endpoint{}

	// Multi-endpoint case. Keys look like "<bf>.url" and "<bf>.model".
	sub := props.Subset("hermes.endpoints")
	for k, v := range sub {
		if !strings.HasSuffix(k, ".url") {
			continue
		}
		bf := session.BrainFunction(strings.TrimSuffix(k, ".url"))
		ep := endpoints[bf]
		ep.BaseURL = v
		if m := sub[string(bf)+".model"]; m != "" {
			ep.Model = m
		}
		endpoints[bf] = ep
	}

	if len(endpoints) > 0 {
		return endpoints, nil
	}

	// Single-endpoint fallback.
	if hermesURL != "" {
		for _, bf := range []session.BrainFunction{
			session.BrainPlanning,
			session.BrainReasoning,
			session.BrainRetrieval,
			session.BrainCritic,
			session.BrainPerception,
			session.BrainComposition,
		} {
			endpoints[bf] = hermes.Endpoint{BaseURL: hermesURL, Model: hermesModel}
		}
	}
	return endpoints, nil
}

// buildRegistry assembles the brain-function → oscillator table. In
// Hermes mode every oscillator wraps the same hermes.Adapter (which
// itself looks up the endpoint by brain function). In stub mode each
// oscillator gets a pre-canned stub. Keeping the two branches in one
// builder makes it obvious that the registry shape is identical;
// only the adapter underneath changes.
func buildRegistry(tracer trace.Tracer, endpoints map[session.BrainFunction]hermes.Endpoint, costTracker *cost.Tracker) (*registry.Registry, error) {
	reg := registry.New()

	if len(endpoints) > 0 {
		cfg := hermes.Config{
			Endpoints: endpoints,
			Tracer:    tracer,
			Cost:      costTracker,
		}
		ad, err := hermes.New(cfg)
		if err != nil {
			return nil, fmt.Errorf("build hermes adapter: %w", err)
		}
		// Only register oscillators for brain functions that have an
		// endpoint. Brain functions absent from the config use the
		// stub demo seeds (allows mix-and-match for local dev).
		for bf := range endpoints {
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
