// CLAUDE GENERATED
// Demo runner. Two modes:
//
//  1. Default: three stub oscillators in a code-analyst -> fact-check
//     -> writer chain. Proves the spike-passing architecture compiles
//     and behaves sensibly. No model is called.
//
//  2. When hermes.Config.BinPath is set (via OSCILLITRON_HERMES_BIN
//     env or --hermes-bin flag): single-node `code` topology backed
//     by the real Hermes adapter (pkg/adapter/hermes). This is the
//     Phase 2 seed-specialist demo (parent CLAUDE.md lock 2026-05-18:
//     Phase 2 ships with `code` only).
//
// All tunables (hermes, runner, cost, pool) are externalized via the
// Spring-Boot-style loader pattern. Precedence: code defaults < env
// (OSCILLITRON_*_*) < flags (--*-*). See each package's config_load.go.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/adapter/pool"
	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/cost"
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
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Externalized config across three packages — one shared FlagSet
	// so a single fs.Parse() merges env + flags for everyone.
	// Precedence: code defaults < env (OSCILLITRON_*_*) < flags (--*-*).
	//
	// IMPORTANT: each package's RegisterFlags binds pointers into the
	// caller-held config struct. We must keep those structs in scope
	// across fs.Parse() — so they live as locals here, not inside a
	// helper that returns them by value.
	fs := flag.NewFlagSet("oscillitron", flag.ExitOnError)

	hermesCfg := hermes.DefaultConfig()
	{
		var err error
		hermesCfg, err = hermes.ApplyEnv(hermesCfg)
		if err != nil {
			logger.Error("config: hermes env", "err", err)
			os.Exit(2)
		}
		hermes.RegisterFlags(fs, &hermesCfg)
	}

	runnerTunables := runner.DefaultTunables()
	{
		var err error
		runnerTunables, err = runner.ApplyEnv(runnerTunables)
		if err != nil {
			logger.Error("config: runner env", "err", err)
			os.Exit(2)
		}
		runner.RegisterFlags(fs, &runnerTunables)
	}

	pricingCfg := cost.DefaultPricingConfig()
	{
		var err error
		pricingCfg, err = cost.ApplyEnv(pricingCfg)
		if err != nil {
			logger.Error("config: cost env", "err", err)
			os.Exit(2)
		}
		cost.RegisterFlags(fs, &pricingCfg)
	}

	poolCfg := pool.DefaultPoolConfig()
	{
		var err error
		poolCfg, err = pool.ApplyEnv(poolCfg)
		if err != nil {
			logger.Error("config: pool env", "err", err)
			os.Exit(2)
		}
		pool.RegisterFlags(fs, &poolCfg)
	}

	// Top-level demo flags. --interactive turns the runner into a REPL
	// reading prompts line-by-line from stdin; without it, each path
	// runs a single hardcoded prompt for smoke-testing.
	interactive := envOrFlagBool(fs, "interactive", "OSCILLITRON_INTERACTIVE", false,
		"read prompts from stdin one line at a time; EOF or 'quit' exits")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	if hermesCfg.BinPath != "" {
		if err := runHermesPath(hermesCfg, runnerTunables, pricingCfg, poolCfg, *interactive, logger); err != nil {
			logger.Error("hermes path failed", "err", err)
			os.Exit(1)
		}
		return
	}

	if *interactive {
		if err := runStubREPL(runnerTunables, logger); err != nil {
			logger.Error("stub repl failed", "err", err)
			os.Exit(1)
		}
		return
	}

	// Topology: code-analyst -> fact-check -> writer
	//
	//   * code-analyst (budget-exhausted): produces a partial result
	//     and hands it forward.
	//   * fact-check  (budget-exhausted): also hands forward — proves
	//     the chain isn't single-hop.
	//   * writer      (done): terminal node that produces the final
	//     answer.
	topo := topology.New("code-analyst")
	must(topo.AddEdge("code-analyst", topology.Edge{To: "fact-check", Weight: 1.0}))
	must(topo.AddEdge("fact-check", topology.Edge{To: "writer", Weight: 1.0}))
	topo.AddNode("writer") // explicit sink

	oscMap := map[oscillator.ID]*oscillator.Oscillator{
		"code-analyst": oscillator.New(
			"code-analyst",
			stub.New("qwen3-coder-30b", stub.ModeBudgetExhausted).
				WithConfidence(0.6).
				WithSignals("loop bound looks suspicious"),
			logger,
		),
		"fact-check": oscillator.New(
			"fact-check",
			stub.New("qwen3-7b", stub.ModeBudgetExhausted).
				WithConfidence(0.7).
				WithSignals("confirmed: off-by-one at line 12"),
			logger,
		),
		"writer": oscillator.New(
			"writer",
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

	cfg := runnerTunables.Apply(runner.Config{
		Topology:    topo,
		Oscillators: oscMap,
		Router:      rule.New(),
		Inhibitor: composite.New(
			hardcap.New(10),
			confidence.New(0.3, 0.4, 3),
			repetition.New(5, 3),
			contradictions.New(3, 8),
		),
		Initial: initial,
		Tracer:  trace.Slog{Logger: logger},
	})
	// poolCfg is registered/parsed but unused in this demo (single
	// Adapter, no Pool). Mention it here so the reference doesn't get
	// optimized out and so the flag actually drives behavior when a
	// Pool is composed in a future demo: poolCfg.Apply(thePool).
	_ = poolCfg

	logger.Info("starting demo run",
		"entry", topo.Entry(),
		"nodes", topo.Nodes(),
		"initial_session", initial.ID,
		"buffer_size", cfg.BufferSize,
		"chain_timeout", cfg.ChainTimeout,
	)

	// Stub demo has no model latency, so 30s is plenty even if
	// --runner-chain-timeout is 0 (uncapped). Inner ChainTimeout from
	// runner.Config (if set) wraps this further.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

// envOrFlagBool registers a bool flag with a default that comes from
// an env var (parsed truthy/falsy), falling back to fallback. Returns
// a pointer the flag will write to.
func envOrFlagBool(fs *flag.FlagSet, flagName, envName string, fallback bool, usage string) *bool {
	def := fallback
	if v, ok := os.LookupEnv(envName); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			def = b
		}
	}
	return fs.Bool(flagName, def, usage)
}

// runHermesPath supervises one Hermes process and either fires a
// hardcoded smoke-test prompt (interactive=false) or loops on stdin
// (interactive=true). poolCfg is accepted for symmetry; the current
// single-Adapter wiring doesn't consume it.
func runHermesPath(cfg hermes.Config, runnerT runner.Tunables, pricing cost.PricingConfig, poolCfg pool.PoolConfig, interactive bool, logger *slog.Logger) error {
	_ = poolCfg // reserved for the next demo iteration
	// Fill Cwd if neither env nor flag set it.
	if cfg.Cwd == "" {
		var err error
		cfg.Cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}
	if !filepath.IsAbs(cfg.Cwd) {
		return fmt.Errorf("hermes cwd must be absolute, got %q", cfg.Cwd)
	}

	// Headroom above MinConnectionTimeout so the floor check has runway.
	// Without the doubling, the ctx and the floor are equal and the
	// "deadline < floor" check trips immediately on microsecond elapsed
	// time between WithTimeout and New().
	startBudget := cfg.MinConnectionTimeout
	if startBudget <= 0 {
		startBudget = hermes.DefaultMinConnectionTimeout
	}
	startCtx, startCancel := context.WithTimeout(context.Background(), 2*startBudget)
	defer startCancel()

	hermesAdapter, err := hermes.New(startCtx, cfg)
	if err != nil {
		return fmt.Errorf("hermes.New: %w", err)
	}
	defer hermesAdapter.Close()

	// Pricing built once and reused across prompts.
	if _, ok := pricing.Models[cfg.Name]; !ok {
		if pricing.Models == nil {
			pricing.Models = map[string]cost.Pricing{}
		}
		pricing.Models[cfg.Name] = cost.Pricing{}
	}
	tracker := cost.NewTrackerFromConfig(pricing)

	logger.Info("starting hermes path",
		"bin", cfg.BinPath,
		"cwd", cfg.Cwd,
		"name", cfg.Name,
		"max_context_tokens", cfg.MaxContextTokens,
		"chain_timeout", runnerT.ChainTimeout,
		"interactive", interactive,
	)

	runOnce := func(objective, content string, ord int) error {
		initial := session.Envelope{
			ID:             session.ID(fmt.Sprintf("hermes-%03d", ord)),
			Type:           session.TypeAnalyze,
			Objective:      objective,
			Classification: classification.Internal,
			Input:          session.Input{Type: "prompt", Content: content},
		}
		topo, oscMap := singleNodeTopo(cfg.Name, hermesAdapter, logger)
		runCfg := runnerT.Apply(runner.Config{
			Topology:    topo,
			Oscillators: oscMap,
			Router:      rule.New(),
			Inhibitor: composite.New(
				hardcap.New(4),
				confidence.New(0.3, 0.4, 3),
			),
			Initial: initial,
			Logger:  logger,
			Tracker: tracker,
		})
		// Outer ctx is uncapped — runner.Run honors ChainTimeout itself.
		result, err := runner.Run(context.Background(), runCfg)
		if err != nil {
			return fmt.Errorf("runner: %w", err)
		}
		printChainResult(result, tracker)
		return nil
	}

	if interactive {
		return repl(os.Stdin, "oscillitron> ", func(line string, ord int) error {
			return runOnce(line, line, ord)
		})
	}
	// Smoke-test: one hardcoded prompt.
	if err := runOnce(
		"Review the snippet for off-by-one bugs and respond concisely.",
		"for i := 0; i <= len(xs); i++ { use(xs[i]) }",
		1,
	); err != nil {
		return err
	}
	printRunningTotals(tracker)
	return nil
}

// singleNodeTopo builds a fresh single-node topology and oscillator
// map around an adapter. Used per-Run inside the REPL loop because
// runner.Run consumes the oscillator goroutines (closes their input
// channels) — but the underlying Adapter persists across prompts.
func singleNodeTopo(name string, a adapter.Adapter, logger *slog.Logger) (*topology.Topology, map[oscillator.ID]*oscillator.Oscillator) {
	id := oscillator.ID(name)
	topo := topology.New(id)
	topo.AddNode(id) // explicit sink
	return topo, map[oscillator.ID]*oscillator.Oscillator{
		id: oscillator.New(id, a, logger),
	}
}

// printChainResult renders a single chain's outcome to stdout.
func printChainResult(result runner.Result, tracker *cost.Tracker) {
	fmt.Println()
	fmt.Println("====================================================")
	fmt.Printf("chain settled: reason=%s detail=%q\n", result.Reason, result.Detail)
	fmt.Printf("chain length:  %d hops\n", len(result.Chain))
	fmt.Println("----------------------------------------------------")
	for i, env := range result.Chain {
		fmt.Printf("[%d] session=%s model=%s reason=%s\n",
			i, env.ID, env.Routing.Model, env.Routing.Reason)
		if env.Outcome != nil {
			fmt.Printf("    exit=%s verdict=%q\n",
				env.Outcome.ExitReason, env.Outcome.Verdict)
			if env.Trace.TokensInput > 0 || env.Trace.CostUSD > 0 || env.Trace.CostVsFrontierBaselineUSD > 0 {
				fmt.Printf("    tokens=(in=%d,out=%d) cost=$%.4f frontier=$%.4f savings=$%.4f\n",
					env.Trace.TokensInput, env.Trace.TokensOutput,
					env.Trace.CostUSD, env.Trace.CostVsFrontierBaselineUSD,
					env.Trace.CostVsFrontierBaselineUSD-env.Trace.CostUSD)
			}
		}
	}
	if tracker != nil {
		printRunningTotals(tracker)
	}
	fmt.Println("====================================================")
}

func printRunningTotals(tracker *cost.Tracker) {
	if tracker == nil {
		return
	}
	s := tracker.Summary()
	if s.TotalFrontierUSD == 0 && s.TotalActualUSD == 0 {
		return
	}
	fmt.Println("----------------------------------------------------")
	fmt.Printf("totals: actual=$%.4f frontier=$%.4f savings=$%.4f (%.1f%% off)\n",
		s.TotalActualUSD, s.TotalFrontierUSD, s.TotalSavingsUSD, s.SavingsRatio()*100)
}

// repl reads lines from r and calls handler for each non-blank,
// non-quit line. ord is a 1-based ordinal incremented per dispatched
// line. EOF, "quit", and "exit" terminate the loop cleanly.
func repl(r *os.File, prompt string, handler func(line string, ord int) error) error {
	fmt.Println("Oscillitron interactive mode. Enter a prompt and press return. EOF or 'quit' exits.")
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	ord := 0
	for {
		fmt.Print(prompt)
		if !sc.Scan() {
			fmt.Println() // newline on EOF
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == "quit" || line == "exit" {
			break
		}
		ord++
		if err := handler(line, ord); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
	return sc.Err()
}

// runStubREPL drives the stub topology in interactive mode. Each
// prompt fans through the 3-stub chain so the user can see the
// orchestration shape without a real model running.
func runStubREPL(runnerT runner.Tunables, logger *slog.Logger) error {
	return repl(os.Stdin, "oscillitron[stub]> ", func(line string, ord int) error {
		topo := topology.New("code-analyst")
		must(topo.AddEdge("code-analyst", topology.Edge{To: "fact-check", Weight: 1.0}))
		must(topo.AddEdge("fact-check", topology.Edge{To: "writer", Weight: 1.0}))
		topo.AddNode("writer")

		oscMap := map[oscillator.ID]*oscillator.Oscillator{
			"code-analyst": oscillator.New("code-analyst",
				stub.New("qwen3-coder-30b", stub.ModeBudgetExhausted).WithConfidence(0.6), logger),
			"fact-check": oscillator.New("fact-check",
				stub.New("qwen3-7b", stub.ModeBudgetExhausted).WithConfidence(0.7), logger),
			"writer": oscillator.New("writer",
				stub.New("qwen3-4b", stub.ModeDone).WithConfidence(0.85), logger),
		}

		initial := session.Envelope{
			ID:             session.ID(fmt.Sprintf("stub-%03d", ord)),
			Type:           session.TypeAnalyze,
			Objective:      line,
			Classification: classification.Internal,
			Input:          session.Input{Type: "prompt", Content: line},
		}
		runCfg := runnerT.Apply(runner.Config{
			Topology:    topo,
			Oscillators: oscMap,
			Router:      rule.New(),
			Inhibitor: composite.New(
				hardcap.New(10),
				confidence.New(0.3, 0.4, 3),
				repetition.New(5, 3),
				contradictions.New(3, 8),
			),
			Initial: initial,
			Tracer:  trace.Slog{Logger: logger},
		})

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := runner.Run(ctx, runCfg)
		if err != nil {
			return err
		}
		printChainResult(result, nil)
		return nil
	})
}
