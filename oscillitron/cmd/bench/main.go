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
	"github.com/jrlmx2/oscillitron/pkg/adapter/curated"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/adapter/ollama"
	"github.com/jrlmx2/oscillitron/pkg/adapter/vllm"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/categorize"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/grader"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/loader/gpqa"
	"github.com/jrlmx2/oscillitron/pkg/benchmark/orchestrator"
	"github.com/jrlmx2/oscillitron/pkg/config"
	"github.com/jrlmx2/oscillitron/pkg/curation"
	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/trace/otel"
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

		// Curation post-hook: when --curate-store-dir is set, run
		// pkg/curation against --stream-out after the bench completes,
		// writing exemplars to the store dir. Closes the self-improvement
		// loop in a single command.
		curateStoreDir  = flag.String("curate-store-dir", "", "optional: after the bench completes, run pkg/curation on --stream-out and write exemplars to this directory; requires --stream-out")
		curateAction    = flag.String("curate-action", "process", "exemplar.Action key for post-hook curation; props: bench.curate.action")
		curateBatchSize = flag.Int("curate-batch-size", 20, "cold-path batch size for post-hook curation")
		curateMinScore  = flag.Float64("curate-min-score", 0, "post-hook curation candidate quality floor")

		// Warm-path curation: wrap orchestrator adapters with
		// pkg/adapter/curated so retrieved exemplars get prepended to
		// each call's prompt. The other half of the self-improvement
		// loop — read what --curate-store-dir wrote (this run or a
		// prior one).
		useStoreDir       = flag.String("use-store", "", "optional: wrap orchestrator adapters with pkg/adapter/curated using this exemplar.FileStore directory; retrieved exemplars prepended to each warm-path call's prompt")
		useStoreAction    = flag.String("use-store-action", "process", "exemplar.Action key the warm-path retrieval reads from (must match what curation wrote)")
		useStoreTopK      = flag.Int("use-store-topk", 3, "max exemplars retrieved per call when --use-store is set")
		useStoreMaxTokens = flag.Int("use-store-max-tokens", 0, "token-budget cap on retrieved exemplar block (0 = no cap; TopK still bounds)")

		orchSubstrate = flag.String("orchestrator-substrate", "auto", "orchestrator substrate: auto | hermes | ollama | vllm | anthropic. 'auto' inspects --orchestrator-model and picks ollama for small models (phi*, llama3.2:3b, llama3.2:1b, gemma:7b, qwen2.5:7b/3b) — see references/substrate-routing.md — and hermes for everything else. vllm is explicit-only (auto never picks it).")
		orchURL       = flag.String("orchestrator-url", "", "substrate base URL (default: hermes=http://127.0.0.1:8642, ollama=http://127.0.0.1:11434, vllm=http://127.0.0.1:8000, anthropic=cloud API)")
		orchModel     = flag.String("orchestrator-model", "", "model id for orchestrator")

		frontSubstrate = flag.String("frontier-substrate", "anthropic", "frontier substrate (hermes|ollama|vllm|anthropic — 'auto' is not used for the frontier baseline)")
		frontURL       = flag.String("frontier-url", "", "substrate base URL for frontier (default: hermes=http://127.0.0.1:8642, ollama=http://127.0.0.1:11434, vllm=http://127.0.0.1:8000, anthropic=cloud API)")
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

		verbose     = flag.Bool("v", false, "verbose tracer (slog Info events to stderr); props: trace.verbose")
		otelEnable  = flag.Bool("otel", false, "ship trace events to OpenTelemetry via OTLP HTTP (operator configures endpoint + auth via OTEL_EXPORTER_OTLP_* env vars; combine with -v to keep stderr output too); props: trace.otel.enabled")
		otelService = flag.String("otel-service-name", "oscillitron-bench", "value sent as the OTLP resource attribute service.name; props: trace.otel.service_name")
		configFlag  = flag.String("config", "", "optional .properties config path; props fill in any flag the user did not pass on the CLI (CLI always wins)")
	)
	flag.Parse()

	// Properties file: fills in unset CLI flags. CLI wins. Keys:
	//
	//   bench.benchmark             (default for --benchmark)
	//   bench.cases                 (--cases)
	//   bench.limit                 (--limit)
	//   bench.vote_n                (--vote-n)
	//   bench.sliding_window        (--sliding-window)
	//   bench.report_out            (--report-out)
	//   bench.stream_out            (--stream-out)
	//   bench.frontier_price        (--frontier-price)
	//   bench.orchestrator.substrate / url / model
	//   bench.frontier.substrate / url / model
	//   trace.verbose               (--v)
	//   trace.otel.enabled          (--otel)
	//   trace.otel.service_name     (--otel-service-name)
	props := config.Properties{}
	if *configFlag != "" {
		p, err := config.Load(*configFlag)
		if err != nil {
			return fmt.Errorf("load %s: %w", *configFlag, err)
		}
		props = p
		// Trace settings.
		if !flagPassed("v") {
			*verbose = props.Bool("trace.verbose", false)
		}
		if !flagPassed("otel") {
			*otelEnable = props.Bool("trace.otel.enabled", false)
		}
		if !flagPassed("otel-service-name") {
			*otelService = props.String("trace.otel.service_name", "oscillitron-bench")
		}
		// Bench settings.
		if !flagPassed("benchmark") {
			*benchName = props.String("bench.benchmark", *benchName)
		}
		if !flagPassed("cases") {
			*casesPath = props.String("bench.cases", *casesPath)
		}
		if !flagPassed("limit") {
			*limit = props.Int("bench.limit", *limit)
		}
		if !flagPassed("vote-n") {
			*voteN = props.Int("bench.vote_n", *voteN)
		}
		if !flagPassed("sliding-window") {
			*windowN = props.Int("bench.sliding_window", *windowN)
		}
		if !flagPassed("report-out") {
			*reportOut = props.String("bench.report_out", "")
		}
		if !flagPassed("stream-out") {
			*streamOut = props.String("bench.stream_out", "")
		}
		if !flagPassed("frontier-price") {
			if s := props.String("bench.frontier_price", ""); s != "" {
				if v, perr := strconv.ParseFloat(s, 64); perr == nil {
					*frontierPrice = v
				}
			}
		}
		// Pricing entries from props: bench.price.<name> = rate.
		for _, key := range props.PrefixedKeys("bench.price.") {
			name := strings.TrimPrefix(key, "bench.price.")
			priceFlag = append(priceFlag, name+"="+props.String(key, "0"))
		}
		// Curation post-hook.
		if !flagPassed("curate-store-dir") {
			*curateStoreDir = props.String("bench.curate.store_dir", "")
		}
		if !flagPassed("curate-action") {
			*curateAction = props.String("bench.curate.action", *curateAction)
		}
		if !flagPassed("curate-batch-size") {
			*curateBatchSize = props.Int("bench.curate.batch_size", *curateBatchSize)
		}
		if !flagPassed("curate-min-score") {
			if s := props.String("bench.curate.min_score", ""); s != "" {
				if v, perr := strconv.ParseFloat(s, 64); perr == nil {
					*curateMinScore = v
				}
			}
		}
		// Warm-path retrieval (--use-store).
		if !flagPassed("use-store") {
			*useStoreDir = props.String("bench.use_store.dir", "")
		}
		if !flagPassed("use-store-action") {
			*useStoreAction = props.String("bench.use_store.action", *useStoreAction)
		}
		if !flagPassed("use-store-topk") {
			*useStoreTopK = props.Int("bench.use_store.topk", *useStoreTopK)
		}
		if !flagPassed("use-store-max-tokens") {
			*useStoreMaxTokens = props.Int("bench.use_store.max_tokens", *useStoreMaxTokens)
		}
		// Substrate routing.
		if !flagPassed("orchestrator-substrate") {
			*orchSubstrate = props.String("bench.orchestrator.substrate", *orchSubstrate)
		}
		if !flagPassed("orchestrator-url") {
			*orchURL = props.String("bench.orchestrator.url", *orchURL)
		}
		if !flagPassed("orchestrator-model") {
			*orchModel = props.String("bench.orchestrator.model", *orchModel)
		}
		if !flagPassed("frontier-substrate") {
			*frontSubstrate = props.String("bench.frontier.substrate", *frontSubstrate)
		}
		if !flagPassed("frontier-url") {
			*frontURL = props.String("bench.frontier.url", *frontURL)
		}
		if !flagPassed("frontier-model") {
			*frontModel = props.String("bench.frontier.model", *frontModel)
		}
	}
	_ = props // future expansion

	var tracer trace.Tracer = trace.Discard{}
	var tracers trace.Multi
	if *verbose {
		tracers = append(tracers, trace.Slog{
			Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		})
	}
	if *otelEnable {
		provider, err := otel.New(context.Background(), *otelService)
		if err != nil {
			return fmt.Errorf("--otel: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := provider.Shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "bench: OTel shutdown: %v\n", err)
			}
		}()
		tracers = append(tracers, trace.Slog{Logger: provider.Logger})
		fmt.Fprintf(os.Stderr, "bench: shipping trace events to OTel (service.name=%s; configure endpoint via OTEL_EXPORTER_OTLP_ENDPOINT)\n", *otelService)
	}
	switch len(tracers) {
	case 0:
		// keep Discard
	case 1:
		tracer = tracers[0]
	default:
		tracer = tracers
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

	// Warm-path retrieval: when --use-store is set, wrap both
	// orchestrator adapters with pkg/adapter/curated so each call's
	// prompt gets prepended with the top-K relevant exemplars from
	// the per-action FileStore. Same wrapper on both arms keeps the
	// frontier vs vote comparison apples-to-apples.
	if *useStoreDir != "" {
		if err := os.MkdirAll(*useStoreDir, 0o755); err != nil {
			return fmt.Errorf("--use-store: ensure dir %s: %w", *useStoreDir, err)
		}
		store := &exemplar.FileStore{Dir: *useStoreDir}
		wrap := func(name string, inner adapter.Adapter) adapter.Adapter {
			return &curated.Adapter{
				Inner:             inner,
				Store:             store,
				TopK:              *useStoreTopK,
				MaxExemplarTokens: *useStoreMaxTokens,
				ActionOverride:    *useStoreAction,
				Tracer:            tracer,
			}
		}
		orchAdapter = wrap("orchestrator", orchAdapter)
		frontAdapter = wrap("frontier", frontAdapter)
		fmt.Fprintf(os.Stderr,
			"bench: warm-path retrieval enabled (store=%s, action=%s, topk=%d, max_tokens=%d)\n",
			*useStoreDir, *useStoreAction, *useStoreTopK, *useStoreMaxTokens)
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
			Tracer:    tracer,
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

	// Post-hook curation. Reuses the orchestrator adapter as the
	// cold-path session (operators who want a different cold-path
	// substrate should run cmd/curate separately). Requires
	// --stream-out so there's a JSONL file to read from.
	if *curateStoreDir != "" {
		if *streamOut == "" {
			return fmt.Errorf("--curate-store-dir requires --stream-out (curation reads the JSONL stream)")
		}
		if err := os.MkdirAll(*curateStoreDir, 0o755); err != nil {
			return fmt.Errorf("--curate-store-dir %s: %w", *curateStoreDir, err)
		}
		store := &exemplar.FileStore{Dir: *curateStoreDir}
		fmt.Fprintf(os.Stderr, "bench: post-hook curating %s → %s (action=%s)\n",
			*streamOut, *curateStoreDir, *curateAction)
		curResult, cerr := curation.Run(context.Background(), curation.Config{
			Adapter:    orchAdapter,
			Store:      store,
			StreamPath: *streamOut,
			Action:     *curateAction,
			BatchSize:  *curateBatchSize,
			MinScore:   *curateMinScore,
			Tracer:     tracer,
		})
		if cerr != nil {
			return fmt.Errorf("post-hook curation: %w", cerr)
		}
		fmt.Fprintf(os.Stderr,
			"bench: curation done: candidates=%d batches=%d/%d exemplars=%d tokens=%d elapsed=%s\n",
			curResult.CandidatesFiltered,
			curResult.BatchesProcessed, curResult.BatchesProcessed+curResult.BatchesFailed,
			curResult.ExemplarsAdded, curResult.AdapterTokens,
			curResult.Elapsed.Round(time.Second))
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
// hosted-frontier baseline". substrate="auto" defers to
// resolveSubstrate which inspects the model name.
func buildAdapter(role, substrate, url, model string) (adapter.Adapter, error) {
	if substrate == "auto" {
		substrate = resolveSubstrate(role, model)
	}
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
	case "ollama":
		if url == "" {
			url = ollama.DefaultBaseURL
		}
		if model == "" {
			return nil, fmt.Errorf("%s adapter (ollama): --%s-model is required (ollama has no server-side default)", role, role)
		}
		a, err := ollama.New(ollama.SingleEndpoint(url, model))
		if err != nil {
			return nil, fmt.Errorf("%s adapter (ollama %s): %w", role, url, err)
		}
		return a, nil
	case "vllm":
		if url == "" {
			url = vllm.DefaultBaseURL
		}
		if model == "" {
			return nil, fmt.Errorf("%s adapter (vllm): --%s-model is required (vllm has no server-side default)", role, role)
		}
		a, err := vllm.New(vllm.SingleEndpoint(url, model))
		if err != nil {
			return nil, fmt.Errorf("%s adapter (vllm %s): %w", role, url, err)
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
		return nil, fmt.Errorf("%s adapter: unknown substrate %q (want 'auto', 'hermes', 'ollama', 'vllm', or 'anthropic')", role, substrate)
	}
}

// resolveSubstrate is the auto-routing heuristic. Small open-weight
// models (3B–7B class) get degraded by Hermes's agentic envelope —
// Hermes's own docs recommend ≥30B for tool-call work. For those
// substrates, route through pkg/adapter/ollama (direct, no
// envelope). Everything else stays on Hermes.
//
// Frontier role bypasses ollama entirely — the frontier baseline is
// always a hosted model where Hermes-or-Anthropic is the right call,
// and an "auto" frontier without a hint should fail loud rather than
// silently picking local Ollama.
//
// The heuristic is intentionally a list, not a parser: model size is
// not always encoded in the tag (phi4-mini has no size in the name),
// and false-positive misrouting is more painful than false-negative.
// Extend the list as small models are added to the deployment.
func resolveSubstrate(role, model string) string {
	if role == "frontier" {
		// Frontier auto = hermes (which can pick anthropic via its
		// own config); never silently downgrade the baseline.
		return "hermes"
	}
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return "hermes"
	}
	for _, sub := range smallModelSubstrings {
		if strings.Contains(m, sub) {
			return "ollama"
		}
	}
	return "hermes"
}

// smallModelSubstrings is the small-substrate allowlist for auto
// routing. Match is case-insensitive substring. Reviewed against
// Hermes's own docs (~/hermes-agent/website/docs/guides/local-ollama-setup.md):
// 3B–7B models are documented as unreliable with Hermes's tool-call
// envelope.
var smallModelSubstrings = []string{
	"phi",            // phi4-mini, phi3, phi2 — all ≤4B
	"llama3.2:3b",    // Hermes explicitly flags this as "no tool calling"
	"llama3.2:1b",
	"gemma:7b",
	"gemma2:9b",
	"gemma2:2b",
	"qwen2.5:3b",
	"qwen2.5:7b",
	"qwen2.5-coder:3b",
	"qwen2.5-coder:7b",
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

// flagPassed reports whether the given flag was set explicitly on
// the CLI. Used to decide whether a properties value should override
// (it shouldn't — CLI wins).
func flagPassed(name string) bool {
	passed := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})
	return passed
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
// orchestrator, failure categorization, and the sliding-window
// evolution.
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

	// Failure categorization — surfaces WHY cases failed beyond
	// pass/fail (format vs reasoning vs refusal vs operational).
	fmt.Fprintln(w)
	fmt.Fprintln(w, "--- "+strings.TrimRight(categorize.FormatTable(categorize.Aggregate(r)), "\n"))

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
