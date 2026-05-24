// Cmd curate is the cold-path curation driver — reads a bench's
// --stream-out JSONL, asks the cold-path session ("you are the
// {action} specialist") to identify which AP outcomes are worth
// preserving as few-shot exemplars, and writes the selections to
// the per-action exemplar.Store.
//
// Operators run this offline after a bench completes (or via
// cmd/bench --curate-store-dir for the convenience path that
// auto-curates with the same orchestrator adapter).
//
// Usage:
//
//	go run ./cmd/curate \
//	  --stream /tmp/bench-stream.jsonl \
//	  --store-dir /tmp/exemplars \
//	  --substrate hermes --url http://127.0.0.1:8642 \
//	  --action process \
//	  --batch-size 20 \
//	  --orchestrator-allowlist frontier-default \
//	  -v
//
// Substrate plumbing mirrors cmd/bench. Trace + properties config
// follow the same pattern.
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
	adapterAnth "github.com/jrlmx2/oscillitron/pkg/adapter/anthropic"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/config"
	"github.com/jrlmx2/oscillitron/pkg/curation"
	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/trace/otel"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "curate:", err)
		os.Exit(1)
	}
}

func run() error {
	var allowlistFlag stringSliceFlag
	flag.Var(&allowlistFlag, "orchestrator-allowlist", "restrict candidates to this orchestrator name; repeatable")
	var (
		streamPath    = flag.String("stream", "", "JSONL stream file produced by cmd/bench --stream-out (required)")
		storeDir      = flag.String("store-dir", "", "exemplar.FileStore directory; created if missing (required)")
		action        = flag.String("action", curation.DefaultAction, "exemplar.Action key for everything written this run")
		batchSize     = flag.Int("batch-size", curation.DefaultBatchSize, "cases per cold-path call (smaller = faster + more calls)")
		minScore      = flag.Float64("min-score", 0, "candidate quality floor (0 = include any Pass=true, 1.0 = perfect-pass only)")
		maxTokens     = flag.Int("max-tokens-per-action", 0, "store GC budget per action (0 = no cap)")
		sessionPrefix = flag.String("session-id-prefix", curation.DefaultSessionIDPrefix, "Hermes session_id prefix for cold-path memory continuity")

		substrate = flag.String("substrate", "hermes", "cold-path substrate (hermes|anthropic)")
		url       = flag.String("url", "http://127.0.0.1:8642", "hermes gateway URL or anthropic BaseURL")
		model     = flag.String("model", "", "model id (default per substrate)")

		verbose     = flag.Bool("v", false, "verbose tracer (slog Info events to stderr); props: trace.verbose")
		otelEnable  = flag.Bool("otel", false, "ship trace events to OTel via OTLP HTTP; props: trace.otel.enabled")
		otelService = flag.String("otel-service-name", "oscillitron-curate", "OTLP resource attribute service.name; props: trace.otel.service_name")
		configFlag  = flag.String("config", "", "optional .properties config path; CLI flags always win")
	)
	flag.Parse()

	// Properties file: fills in any unset flag. CLI wins.
	//
	// Keys mirror cmd/bench's namespace under "curate.*":
	//   curate.stream / curate.store_dir / curate.action / curate.batch_size /
	//   curate.min_score / curate.max_tokens_per_action / curate.session_id_prefix
	//   curate.substrate / curate.url / curate.model
	//   trace.verbose / trace.otel.enabled / trace.otel.service_name
	if *configFlag != "" {
		props, err := config.Load(*configFlag)
		if err != nil {
			return fmt.Errorf("load %s: %w", *configFlag, err)
		}
		if !flagPassed("stream") {
			*streamPath = props.String("curate.stream", "")
		}
		if !flagPassed("store-dir") {
			*storeDir = props.String("curate.store_dir", "")
		}
		if !flagPassed("action") {
			*action = props.String("curate.action", *action)
		}
		if !flagPassed("batch-size") {
			*batchSize = props.Int("curate.batch_size", *batchSize)
		}
		if !flagPassed("min-score") {
			if s := props.String("curate.min_score", ""); s != "" {
				_, _ = fmt.Sscanf(s, "%f", minScore)
			}
		}
		if !flagPassed("max-tokens-per-action") {
			*maxTokens = props.Int("curate.max_tokens_per_action", *maxTokens)
		}
		if !flagPassed("session-id-prefix") {
			*sessionPrefix = props.String("curate.session_id_prefix", *sessionPrefix)
		}
		if !flagPassed("substrate") {
			*substrate = props.String("curate.substrate", *substrate)
		}
		if !flagPassed("url") {
			*url = props.String("curate.url", *url)
		}
		if !flagPassed("model") {
			*model = props.String("curate.model", *model)
		}
		for _, key := range props.PrefixedKeys("curate.allowlist.") {
			allowlistFlag = append(allowlistFlag, props.String(key, ""))
		}
		if !flagPassed("v") {
			*verbose = props.Bool("trace.verbose", false)
		}
		if !flagPassed("otel") {
			*otelEnable = props.Bool("trace.otel.enabled", false)
		}
		if !flagPassed("otel-service-name") {
			*otelService = props.String("trace.otel.service_name", *otelService)
		}
	}

	if *streamPath == "" {
		return fmt.Errorf("--stream is required (or set curate.stream in --config)")
	}
	if *storeDir == "" {
		return fmt.Errorf("--store-dir is required (or set curate.store_dir in --config)")
	}
	if err := os.MkdirAll(*storeDir, 0o755); err != nil {
		return fmt.Errorf("--store-dir %s: %w", *storeDir, err)
	}

	// Tracer wiring — same Multi composition as cmd/bench.
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
				fmt.Fprintf(os.Stderr, "curate: OTel shutdown: %v\n", err)
			}
		}()
		tracers = append(tracers, trace.Slog{Logger: provider.Logger})
		fmt.Fprintf(os.Stderr, "curate: shipping trace events to OTel (service.name=%s)\n", *otelService)
	}
	switch len(tracers) {
	case 0: // keep Discard
	case 1:
		tracer = tracers[0]
	default:
		tracer = tracers
	}

	// Cold-path adapter.
	coldAdapter, err := buildAdapter(*substrate, *url, *model)
	if err != nil {
		return err
	}

	// Exemplar store on disk.
	store := &exemplar.FileStore{
		Dir:                *storeDir,
		MaxTokensPerAction: *maxTokens,
	}

	fmt.Fprintf(os.Stderr, "curate: stream=%s store=%s action=%s substrate=%s batch=%d min_score=%.2f\n",
		*streamPath, *storeDir, *action, *substrate, *batchSize, *minScore)
	if len(allowlistFlag) > 0 {
		fmt.Fprintf(os.Stderr, "curate: orchestrator allowlist: %s\n", strings.Join(allowlistFlag, ", "))
	}

	result, err := curation.Run(context.Background(), curation.Config{
		Adapter:               coldAdapter,
		Store:                 store,
		StreamPath:            *streamPath,
		Action:                *action,
		OrchestratorAllowlist: []string(allowlistFlag),
		BatchSize:             *batchSize,
		MinScore:              *minScore,
		SessionIDPrefix:       *sessionPrefix,
		Tracer:                tracer,
	})
	if err != nil {
		return fmt.Errorf("curation.Run: %w", err)
	}

	// Best-effort GC after the run so the store doesn't keep growing.
	if *maxTokens > 0 {
		if dropped, err := store.GC(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "curate: GC: %v\n", err)
		} else if dropped > 0 {
			fmt.Fprintf(os.Stderr, "curate: GC dropped %d exemplars\n", dropped)
		}
	}

	printResult(os.Stdout, result)
	return nil
}

// --- substrate plumbing (mirrors cmd/bench) ---

func buildAdapter(substrate, url, model string) (adapter.Adapter, error) {
	switch substrate {
	case "hermes":
		if url == "" {
			url = "http://127.0.0.1:8642"
		}
		a, err := hermes.New(hermes.SingleEndpoint(url, model))
		if err != nil {
			return nil, fmt.Errorf("hermes adapter (%s): %w", url, err)
		}
		return a, nil
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("anthropic adapter: ANTHROPIC_API_KEY must be set")
		}
		mod := model
		if mod == "" {
			// Curation needs a moderately capable model; default to Sonnet.
			mod = adapterAnth.DefaultFrontierModel
		}
		a, err := adapterAnth.New(adapterAnth.Config{APIKey: key, Model: mod, BaseURL: url})
		if err != nil {
			return nil, fmt.Errorf("anthropic adapter (%s): %w", mod, err)
		}
		return a, nil
	default:
		return nil, fmt.Errorf("unknown substrate %q (want 'hermes' or 'anthropic')", substrate)
	}
}

// --- flag helpers ---

func flagPassed(name string) bool {
	passed := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})
	return passed
}

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// --- output ---

func printResult(w *os.File, r curation.Result) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "=== Curation summary ===")
	fmt.Fprintf(w, "  cases scanned:        %d\n", r.CasesScanned)
	fmt.Fprintf(w, "  candidates filtered:  %d\n", r.CandidatesFiltered)
	fmt.Fprintf(w, "  batches processed:    %d\n", r.BatchesProcessed)
	fmt.Fprintf(w, "  batches failed:       %d\n", r.BatchesFailed)
	fmt.Fprintf(w, "  exemplars selected:   %d (cold-path picks)\n", r.ExemplarsSelected)
	fmt.Fprintf(w, "  exemplars added:      %d (written to store)\n", r.ExemplarsAdded)
	fmt.Fprintf(w, "  adapter tokens:       %d\n", r.AdapterTokens)
	fmt.Fprintf(w, "  elapsed:              %s\n", r.Elapsed.Round(time.Second))
}
