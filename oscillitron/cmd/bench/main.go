// CLAUDE GENERATED
// Bench driver — runs established LLM benchmarks (GPQA Diamond first;
// MATH-500, AIME, MMLU-Pro, HLE, SWE-bench queued behind) and compares
// an orchestration arm (Vote: N cheap-model calls + majority vote)
// against a frontier baseline (single Sonnet call).
//
// Substrate plumbing mirrors cmd/phase1: each role (orchestrator,
// frontier, optional LLM judge) picks --orchestrator-substrate /
// --frontier-substrate (hermes | anthropic), --*-url, --*-model.
//
// VRAM coordination is the single key piece: ONE *vram.Governor is
// constructed at startup (from a ModelSpec given via flags) and
// threaded through both orchestrators and the grader. All inference
// against the local substrate runs against the same budget.
//
// Output: per-case pass/fail plus aggregate pass-rate + sliding-window
// evolution per orchestrator. The sliding window shows whether the
// orchestrator's quality climbs with case count — the long-term
// self-improvement claim.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	adapterAnth "github.com/jrlmx2/oscillitron/pkg/adapter/anthropic"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/grader"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/loader/gpqa"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/orchestrator"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

func run() error {
	var priceFlag stringSliceFlag
	flag.Var(&priceFlag, "price", "blended USD-per-million-tokens for one orchestrator, format NAME=RATE (e.g., 'orchestrator-vote-3-default=0.01' or 'frontier-google/gemma-4-e4b=4.50'). Repeatable.")
	var (
		benchName     = flag.String("benchmark", "gpqa", "benchmark name (gpqa)")
		casesPath     = flag.String("cases", "cmd/bench/cases/gpqa_diamond.json", "JSON snapshot path (operator-downloaded; see cmd/bench/cases/README.md)")
		limit         = flag.Int("limit", 0, "cap the number of cases (0 = all)")
		voteN         = flag.Int("vote-n", 5, "N attempts for the vote orchestrator")
		windowN       = flag.Int("sliding-window", 25, "sliding-window size in cases (0 = disable window stats)")
		reportOut     = flag.String("report-out", "", "optional: dump the full Report as indented JSON to this path after the run completes")
		streamOut     = flag.String("stream-out", "", "optional: append each CaseResult as one JSON line to this path as the run progresses (crash-safety on long runs; tail -f for live progress)")
		frontierPrice = flag.Float64("frontier-price", 0, "blended USD-per-million-tokens for the counterfactual frontier baseline (e.g., 4.50 for Sonnet 4.6). When set, each orchestrator's total tokens get re-priced through this for the savings column.")

		orchSubstrate = flag.String("orchestrator-substrate", "hermes", "orchestrator substrate (hermes|anthropic)")
		orchURL       = flag.String("orchestrator-url", "http://127.0.0.1:8642", "hermes gateway URL or anthropic BaseURL")
		orchModel     = flag.String("orchestrator-model", "", "model id for orchestrator")

		frontSubstrate = flag.String("frontier-substrate", "anthropic", "frontier substrate (hermes|anthropic)")
		frontURL       = flag.String("frontier-url", "", "hermes gateway URL or anthropic BaseURL")
		frontModel     = flag.String("frontier-model", "", "model id for frontier baseline")

		// ModelSpec for the governor (drives VRAM budgeting). Required
		// to enable governor — otherwise bench runs un-throttled.
		modelLayers   = flag.Int("model-layers", 0, "transformer layer count of the orchestrator substrate (required for governor)")
		modelKVHidden = flag.Int("model-kv-hidden", 0, "num_kv_heads × head_dim of the orchestrator substrate")
		modelKVDtype  = flag.Int("model-kv-dtype-bytes", 2, "KV cache element size (2 = fp16, 1 = fp8)")
		modelContext  = flag.Int("model-context-size", 0, "model context window tokens (required for governor)")
		modelPrefix   = flag.Int("prefix-tokens", 2000, "persona+prefix size estimate (optional)")
		modelName     = flag.String("governor-model-name", "", "ModelSpec.Name for trace records (defaults to --orchestrator-model)")
		governorCeil  = flag.Int("governor-ceiling", 0, "max concurrent leases (0 = runtime.NumCPU())")
		governorRes   = flag.Float64("governor-reserve-fraction", 0, "VRAM safety margin as fraction of available (0 = 5%)")
		vramBudget    = flag.String("vram-budget", "", "operator override for available VRAM (e.g. 16GB); pins what the governor's probe reports")

		verbose = flag.Bool("v", false, "verbose tracer (slog Info events)")
	)
	flag.Parse()

	var tracer trace.Tracer = trace.Discard{}
	if *verbose {
		tracer = trace.Slog{Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	}

	// Build the governor first — orchestrators and graders all share it.
	governor, err := maybeBuildGovernor(*modelLayers, *modelKVHidden, *modelKVDtype, *modelContext, *modelPrefix,
		firstNonEmpty(*modelName, *orchModel), *governorCeil, *governorRes, *vramBudget, tracer)
	if err != nil {
		return err
	}
	if governor == nil {
		fmt.Fprintln(os.Stderr, "bench: no VRAM governor (supply --model-layers/--model-kv-hidden/--model-context-size to enable)")
	} else {
		fmt.Fprintf(os.Stderr, "bench: governor enabled (%s, ceiling=%d)\n", governor.Model(), governor.Ceiling())
	}

	// Build adapters per role.
	orchAdapter, err := buildAdapter("orchestrator", *orchSubstrate, *orchURL, *orchModel)
	if err != nil {
		return err
	}
	frontAdapter, err := buildAdapter("frontier", *frontSubstrate, *frontURL, *frontModel)
	if err != nil {
		return err
	}

	// Build loader (gpqa for now; extensible).
	loader, err := buildLoader(*benchName, *casesPath, *limit)
	if err != nil {
		return err
	}

	// Build orchestrators.
	extractor := orchestrator.ExtractorFunc(func(raw string) string {
		return grader.ExtractLetter(raw, grader.MultichoiceLetters)
	})
	orchestrators := []benchmark.Orchestrator{
		orchestrator.Single{
			NameStr:   "frontier-" + adapterModel(*frontSubstrate, *frontModel),
			Adapter:   frontAdapter,
			Extractor: extractor,
			Governor:  governor, // shares the same budget — risky if frontier and orch are same substrate, but the governor handles it
		},
		orchestrator.Vote{
			NameStr:   fmt.Sprintf("orchestrator-vote-%d-%s", *voteN, adapterModel(*orchSubstrate, *orchModel)),
			Adapter:   orchAdapter,
			N:         *voteN,
			Extractor: extractor,
			Governor:  governor,
		},
	}

	// Grader: Multichoice as Primary; dual scaffolding is in place but
	// no Secondary wired by default (LLM-judge secondary is a future
	// flag).
	mainGrader := grader.Dual{
		Primary: grader.Multichoice{},
	}

	// Optional per-case JSONL streamer for crash-safe long runs.
	var onCase func(benchmark.CaseResult) error
	if *streamOut != "" {
		f, err := os.Create(*streamOut)
		if err != nil {
			return fmt.Errorf("--stream-out: %w", err)
		}
		defer f.Close()
		streamer := &benchmark.JSONLStreamer{W: f, Flusher: f.Sync}
		onCase = streamer.AppendCase
		fmt.Fprintf(os.Stderr, "bench: streaming per-case JSONL to %s\n", *streamOut)
	}

	pricing, err := buildPricingMap(priceFlag)
	if err != nil {
		return err
	}
	if pricing != nil {
		fmt.Fprintf(os.Stderr, "bench: pricing configured for %d orchestrators (frontier baseline = $%.4f/Mtok)\n",
			len(pricing), *frontierPrice)
	}

	report, err := benchmark.Run(context.Background(), benchmark.RunnerConfig{
		Loader:            loader,
		Orchestrators:     orchestrators,
		Grader:            mainGrader,
		SlidingWindowSize: *windowN,
		Tracer:            tracer,
		OnCase:            onCase,
		Pricing:           pricing,
		FrontierPricing:   benchmark.Pricing{USDPerMTok: *frontierPrice},
	})
	if err != nil {
		return err
	}

	printReport(os.Stdout, report)

	if *reportOut != "" {
		if err := benchmark.WriteJSONFile(*reportOut, report); err != nil {
			return fmt.Errorf("--report-out: %w", err)
		}
		fmt.Fprintf(os.Stderr, "bench: wrote JSON report to %s\n", *reportOut)
	}
	return nil
}

// maybeBuildGovernor constructs a *vram.Governor when the operator
// supplied a complete ModelSpec; returns (nil, nil) otherwise. A nil
// governor is safe to pass to orchestrators and graders — Acquire on
// nil returns a no-op lease.
func maybeBuildGovernor(layers, kvHidden, kvDtype, ctx, prefix int, name string,
	ceiling int, reserveFrac float64, vramBudget string, tracer trace.Tracer) (*vram.Governor, error) {
	if layers <= 0 || kvHidden <= 0 || ctx <= 0 {
		return nil, nil
	}
	if vramBudget != "" {
		budget, err := parseBytes(vramBudget)
		if err != nil {
			return nil, fmt.Errorf("--vram-budget: %w", err)
		}
		vram.SetOverride(budget)
	}
	return vram.NewGovernor(vram.GovernorConfig{
		Model: vram.ModelSpec{
			Name:         name,
			Layers:       layers,
			KVHiddenDim:  kvHidden,
			KVDtypeBytes: kvDtype,
			ContextSize:  ctx,
			PrefixTokens: prefix,
		},
		Ceiling:         ceiling,
		ReserveFraction: reserveFrac,
		Tracer:          tracer,
	})
}

// buildAdapter mirrors cmd/phase1's helper. Each role picks a
// substrate; defaults are sensible for "local Hermes orchestrator,
// hosted-frontier baseline".
func buildAdapter(role, substrate, url, model string) (adapter.Adapter, error) {
	switch substrate {
	case "hermes":
		if url == "" {
			url = "http://127.0.0.1:8642"
		}
		a, err := hermes.New(hermes.SingleEndpoint(url, model))
		if err != nil {
			return nil, fmt.Errorf("%s adapter (hermes %s): %w", role, url, err)
		}
		return a, nil
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("%s adapter (anthropic): ANTHROPIC_API_KEY must be set", role)
		}
		mod := model
		if mod == "" {
			if role == "frontier" {
				mod = adapterAnth.DefaultFrontierModel
			} else {
				mod = adapterAnth.DefaultOrchestratorModel
			}
		}
		a, err := adapterAnth.New(adapterAnth.Config{APIKey: key, Model: mod, BaseURL: url})
		if err != nil {
			return nil, fmt.Errorf("%s adapter (anthropic %s): %w", role, mod, err)
		}
		return a, nil
	default:
		return nil, fmt.Errorf("%s adapter: unknown substrate %q (want 'hermes' or 'anthropic')", role, substrate)
	}
}

func buildLoader(name, path string, limit int) (benchmark.Loader, error) {
	switch name {
	case "gpqa", "gpqa-diamond":
		return gpqa.Loader{Path: path, Limit: limit}, nil
	default:
		return nil, fmt.Errorf("unknown benchmark %q (supported: gpqa)", name)
	}
}

func adapterModel(substrate, model string) string {
	if model != "" {
		return model
	}
	if substrate == "anthropic" {
		return "anthropic-default"
	}
	return "default"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// stringSliceFlag collects repeatable --price NAME=RATE flag values.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string  { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// buildPricingMap parses the --price flag values into a PricingMap.
func buildPricingMap(entries []string) (benchmark.PricingMap, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	m := make(benchmark.PricingMap, len(entries))
	for _, e := range entries {
		name, p, err := benchmark.ParsePricingFlag(e)
		if err != nil {
			return nil, err
		}
		m[name] = p
	}
	return m, nil
}

// parseBytes accepts "1024", "8KB", "16MB", "8GB", "1TB".
func parseBytes(s string) (uint64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	mult := uint64(1)
	switch {
	case strings.HasSuffix(s, "TB"):
		mult = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		mult = 1024
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", s, err)
	}
	return v * mult, nil
}

// printReport renders a human-readable summary: aggregates per
// orchestrator and the sliding-window evolution.
func printReport(w *os.File, r benchmark.Report) {
	fmt.Fprintf(w, "\n=== Benchmark: %s ===\n", r.BenchmarkName)
	fmt.Fprintf(w, "Cases: %d   Elapsed: %s\n\n", len(r.Cases), r.EndedAt.Sub(r.StartedAt).Round(time.Second))

	fmt.Fprintf(w, "--- Aggregate ---\n")
	for _, a := range r.Aggregates {
		fmt.Fprintf(w, "  %-50s  pass=%d  fail=%d  err=%d  pass_rate=%.3f  avg_score=%.3f  calls=%d  tokens=%d",
			a.OrchestratorName, a.Successes, a.Failures, a.Errors, a.PassRate, a.AvgScore, a.TotalCalls, a.TotalTokens)
		if a.TotalActualUSD > 0 || a.TotalFrontierUSD > 0 {
			fmt.Fprintf(w, "  actual=$%.4f  frontier=$%.4f  savings=%.1f%%",
				a.TotalActualUSD, a.TotalFrontierUSD, a.SavingsRatio*100)
		}
		fmt.Fprintln(w)
	}

	if len(r.Windows) > 0 {
		fmt.Fprintf(w, "\n--- Sliding window (size=%d) ---\n", r.Windows[0].Size)
		fmt.Fprintf(w, "  end_case  ")
		for _, ws := range r.Windows[0].PerOrchestrator {
			fmt.Fprintf(w, "%-50s  ", ws.OrchestratorName)
		}
		fmt.Fprintln(w)
		for _, win := range r.Windows {
			fmt.Fprintf(w, "  %8d  ", win.EndCase)
			for _, s := range win.PerOrchestrator {
				fmt.Fprintf(w, "pass_rate=%.3f avg_score=%.3f%s",
					s.PassRate, s.AvgScore, strings.Repeat(" ", 14))
			}
			fmt.Fprintln(w)
		}
	}

	// First few failures for each orchestrator — useful for spot-
	// checking the extractor / grader pipeline.
	for oIdx, a := range r.Aggregates {
		fmt.Fprintf(w, "\n--- First failures: %s ---\n", a.OrchestratorName)
		shown := 0
		for _, cr := range r.Cases {
			if shown >= 3 {
				break
			}
			if oIdx >= len(cr.Results) {
				continue
			}
			res := cr.Results[oIdx]
			if res.Err != nil {
				fmt.Fprintf(w, "  %s: ERROR %v\n", cr.CaseID, res.Err)
				shown++
				continue
			}
			if !res.Verdict.Pass {
				fmt.Fprintf(w, "  %s: extracted=%q expected=%q notes=%q\n",
					cr.CaseID, res.Answer.Extracted, cr.Case.Expected, res.Verdict.Notes)
				shown++
			}
		}
		if shown == 0 {
			fmt.Fprintln(w, "  (no failures or errors)")
		}
	}
}
