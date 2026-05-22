// CLAUDE GENERATED
// Demo runner — exercises the uniform-node + evaluate/execute call
// tree end-to-end with the stub adapter.
//
// The demo task: "Outline and execute a small workflow." A root plan
// AP emits four siblings — three process APs that each return a
// concrete result, and one critique AP that produces a
// verifier_signal. The runner walks the call tree synchronously with
// randomized sibling dispatch (PCG-seeded for reproducibility);
// process results bubble to the recomposer, the critique's verdict
// is captured in RunState (per the locked "verifier signals go to
// the runtime, not the next AP" rule).
//
// Hermes-backed runs are deferred to Stage 5 — until then the demo
// uses pkg/adapter/stub. The --config flag and the properties loader
// are preserved so the next stage can plug a real adapter in without
// reshaping the CLI surface.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/config"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/composite"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/runner"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

// parseBytes accepts "1024", "1024B", "8KB", "16MB", "8GB", "1TB"
// (case-insensitive). Returns the byte count. Used for --vram-budget.
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		taskFlag         = flag.String("task", "draft a project plan", "root task description")
		seedFlag         = flag.Uint64("seed", 42, "PCG seed for randomized sibling dispatch (0 = non-deterministic)")
		maxDepth         = flag.Int("max-depth", 4, "absolute path-length cap (belt-and-suspenders)")
		depthBudget      = flag.Int("depth-budget", 3, "per-AP DepthRemaining for the root envelope")
		configFlag       = flag.String("config", "", "optional .properties config path")
		hermesFlag       = flag.String("hermes", "", "single-endpoint Hermes BaseURL (e.g. http://127.0.0.1:8642); pass empty to force stub mode")
		hermesModel      = flag.String("hermes-model", "", "model identifier passed to Hermes (optional)")
		strictFlag       = flag.Bool("strict", false, "require structured JSON from Hermes (error on parse failure)")
		verboseFlag      = flag.Bool("v", false, "verbose tracer (slog Info events)")
		maxConcurrency   = flag.Int("max-concurrency", 0, "sibling-concurrent dispatch cap: 0 = library-managed (auto-derived from detected VRAM, bounded by --max-concurrency-ceiling), 1 = strict serial, N>1 = static cap (still tightened by VRAM)")
		ceilingFlag      = flag.Int("max-concurrency-ceiling", 0, "hard safety cap on auto-derived concurrency (0 = use runner default of 8)")
		vramBudgetFlag   = flag.String("vram-budget", "", "operator override for available VRAM (e.g. 8GB, 4096MB, 8589934592); when set, bypasses platform auto-detection. Absent → library auto-detects via nvidia-smi / rocm-smi / Apple unified / Linux DRM / /proc/meminfo")
		modelContextSize = flag.Int("model-context-size", 0, "model context window in tokens (0 = use runner default of 4096)")
		prefixTokens     = flag.Int("prefix-tokens", 0, "estimated persona+pool+instructions prefix size in tokens; used for VRAM per-session estimate (0 = use runner default of 2000)")
		bytesPerToken    = flag.Uint64("bytes-per-token", 0, "per-token KV-cache cost (0 = use estimator default of 80000 for 4B fp16)")
		maxInputBytes    = flag.Int("max-input-bytes", 0, "inhibit any AP whose Input.Content exceeds this byte budget (0 = no cap)")
	)
	flag.Parse()

	// Config file fills in any setting the user did not pass as a CLI
	// flag. CLI flags always win.
	var props config.Properties
	if *configFlag != "" {
		p, err := config.Load(*configFlag)
		if err != nil {
			return fmt.Errorf("load %s: %w", *configFlag, err)
		}
		props = p
		if *hermesFlag == "" {
			*hermesFlag = props.String("hermes.url", "")
		}
		if *hermesModel == "" {
			*hermesModel = props.String("hermes.model", "")
		}
	}

	// Multi-endpoint takes precedence over single — if ANY
	// hermes.endpoints.<playbook>.url is set in the loaded properties,
	// build a MultiEndpoint config keyed by playbook. The hermes.url /
	// hermes.model single-endpoint settings are ignored when multi is
	// active, matching the comment in oscillitron.properties.example.
	multiCfg, hasMulti, err := buildMultiEndpointFromProps(props)
	if err != nil {
		return err
	}

	// Tracer: slog (info+) when --v, otherwise silent.
	var tracer trace.Tracer = trace.Discard{}
	if *verboseFlag {
		tracer = trace.Slog{Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	}

	var a adapter.Adapter
	switch {
	case hasMulti:
		multiCfg.Tracer = tracer
		multiCfg.RequireStructured = *strictFlag
		ha, err := hermes.New(multiCfg)
		if err != nil {
			return fmt.Errorf("hermes multi-endpoint adapter: %w", err)
		}
		a = ha
		fmt.Fprintf(os.Stderr, "demo: using Hermes multi-endpoint adapter (evaluate=%s; per-playbook endpoints configured, strict=%v)\n",
			multiCfg.EvaluateEndpoint.BaseURL, *strictFlag)
	case *hermesFlag != "":
		cfg := hermes.SingleEndpoint(*hermesFlag, *hermesModel)
		cfg.Tracer = tracer
		cfg.RequireStructured = *strictFlag
		ha, err := hermes.New(cfg)
		if err != nil {
			return fmt.Errorf("hermes adapter: %w", err)
		}
		a = ha
		fmt.Fprintf(os.Stderr, "demo: using Hermes adapter at %s (model=%q strict=%v)\n", *hermesFlag, *hermesModel, *strictFlag)
	default:
		a = buildStubAdapter()
		fmt.Fprintln(os.Stderr, "demo: using stub adapter (pass --hermes URL, set hermes.url, or configure hermes.endpoints.* in config to wire a real Hermes)")
	}

	// Inhibitor: a hard depth cap as the v0 floor; the runner also
	// honors Config.MaxDepth as a belt-and-suspenders layer.
	inh := composite.New(hardcap.New(*maxDepth))

	// PCG seed. 0 means "non-deterministic"; we still construct a
	// rand.Rand so the runner has a stable handle.
	var prng *rand.Rand
	if *seedFlag == 0 {
		prng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	} else {
		prng = rand.New(rand.NewPCG(*seedFlag, *seedFlag^0xa5a5_5a5a_a5a5_5a5a))
	}

	root := session.NewRoot(
		"ap-root",
		*taskFlag,
		"{combined_plan_output}",
		classification.Internal,
		session.Budget{TokensRemaining: 32_000, DepthRemaining: *depthBudget},
	)

	// VRAM management is library-managed by default — the runner
	// auto-constructs vram.Auto() + DefaultSlidingWindowEstimator
	// when no probe/estimator is set. Operator overrides are forwarded
	// here when supplied:
	//   --vram-budget        → vram.SetOverride (pins available bytes)
	//   --bytes-per-token    → custom estimator
	//   --model-context-size → VRAMModel.ContextSize override
	//   --prefix-tokens      → VRAMModel.PrefixTokens override
	if *vramBudgetFlag != "" {
		budget, err := parseBytes(*vramBudgetFlag)
		if err != nil {
			return fmt.Errorf("--vram-budget: %w", err)
		}
		vram.SetOverride(budget)
		fmt.Fprintf(os.Stderr, "demo: VRAM budget overridden to %d bytes\n", budget)
	}
	runnerCfg := runner.Config{
		Adapter:               a,
		Inhibitor:             inh,
		Recomposer:            recomposer.Concat{Separator: recomposer.DefaultSeparator},
		Tracer:                tracer,
		MaxDepth:              *maxDepth,
		Rand:                  prng,
		MaxConcurrency:        *maxConcurrency,
		MaxConcurrencyCeiling: *ceilingFlag,
		MaxInputBytes:         *maxInputBytes,
	}
	// Forward operator overrides to the runner's VRAMModel. Zero
	// values leave the runner's defaults intact (4096 context, 2000
	// prefix tokens — see runner.DefaultVRAMModel).
	if *modelContextSize > 0 || *prefixTokens > 0 || *hermesModel != "" {
		runnerCfg.VRAMModel = runner.VRAMModel{
			Name:         *hermesModel,
			ContextSize:  *modelContextSize,
			PrefixTokens: *prefixTokens,
		}
	}
	// Forward custom BytesPerToken to a fresh estimator. Otherwise
	// the runner's auto-default (DefaultSlidingWindowEstimator) wins.
	if *bytesPerToken > 0 {
		est := vram.DefaultSlidingWindowEstimator()
		est.BytesPerToken = *bytesPerToken
		runnerCfg.VRAMEstimator = est
	}
	switch *maxConcurrency {
	case 0:
		fmt.Fprintln(os.Stderr, "demo: library-managed concurrency (auto-derived from VRAM)")
	case 1:
		fmt.Fprintln(os.Stderr, "demo: strict serial dispatch (MaxConcurrency=1)")
	default:
		fmt.Fprintf(os.Stderr, "demo: static concurrency cap=%d (tightened by VRAM when available)\n", *maxConcurrency)
	}
	res, err := runner.Run(context.Background(), runnerCfg, root)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	printSummary(os.Stdout, res, *taskFlag)
	return nil
}

// buildStubAdapter constructs the deterministic stub the demo uses
// when no Hermes endpoint is configured. The evaluator picks plan at
// the root, critique for inputs starting "verify:", and process
// otherwise; plan emits four sibling APs covering all three Execute
// categories.
func buildStubAdapter() adapter.Adapter {
	return stub.New("demo").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			c := env.Input.Content
			switch {
			case env.ParentID == nil:
				return session.PlaybookPlan, 0.9
			case strings.HasPrefix(c, "verify:"):
				return session.PlaybookCritique, 0.9
			default:
				return session.PlaybookProcess, 0.85
			}
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential,
			session.SubAPSeed{
				Input:          session.Payload{Kind: "task", Content: "outline goals"},
				OutputSchema:   "{outline}",
				Classification: classification.Internal,
			},
			session.SubAPSeed{
				Input:          session.Payload{Kind: "task", Content: "list dependencies"},
				OutputSchema:   "{deps}",
				Classification: classification.Internal,
			},
			session.SubAPSeed{
				Input:          session.Payload{Kind: "task", Content: "estimate effort"},
				OutputSchema:   "{estimate}",
				Classification: classification.Internal,
			},
			session.SubAPSeed{
				Input:          session.Payload{Kind: "task", Content: "verify: estimate looks reasonable"},
				OutputSchema:   "pass|issues",
				Classification: classification.Internal,
			},
		).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "[stub-process-output]"},
			0.82,
		).
		WithVerifierSignal(session.PlaybookCritique, session.VerdictPass)
}

// buildMultiEndpointFromProps reads hermes.endpoints.<key>.url and
// hermes.endpoints.<key>.model from the loaded properties and builds
// a multi-endpoint hermes.Config when ANY per-playbook endpoint URL
// is present. The "evaluate" key is the evaluate endpoint; the other
// keys are playbook names from hermes.AllPlaybooks. If none of those
// keys are set, returns (zero, false, nil) so the caller falls back
// to the single-endpoint path.
func buildMultiEndpointFromProps(props config.Properties) (hermes.Config, bool, error) {
	if props == nil {
		return hermes.Config{}, false, nil
	}
	evalURL := props.String("hermes.endpoints.evaluate.url", "")
	evalModel := props.String("hermes.endpoints.evaluate.model", "")
	perPlaybook := make(map[session.Playbook]hermes.Endpoint, len(hermes.AllPlaybooks))
	anyURL := evalURL != ""
	missing := make([]string, 0, len(hermes.AllPlaybooks))
	for _, pb := range hermes.AllPlaybooks {
		k := "hermes.endpoints." + string(pb)
		url := props.String(k+".url", "")
		model := props.String(k+".model", "")
		if url == "" {
			missing = append(missing, string(pb))
			continue
		}
		anyURL = true
		perPlaybook[pb] = hermes.Endpoint{BaseURL: url, Model: model}
	}
	if !anyURL {
		return hermes.Config{}, false, nil
	}
	if evalURL == "" {
		return hermes.Config{}, false, fmt.Errorf("hermes.endpoints.evaluate.url is required when any hermes.endpoints.<playbook>.url is set")
	}
	if len(missing) > 0 {
		return hermes.Config{}, false, fmt.Errorf("hermes.endpoints.<playbook>.url missing for: %s", strings.Join(missing, ", "))
	}
	cfg, err := hermes.MultiEndpoint(hermes.Endpoint{BaseURL: evalURL, Model: evalModel}, perPlaybook)
	if err != nil {
		return hermes.Config{}, false, err
	}
	return cfg, true, nil
}

// printSummary renders the result in a human-friendly form. Useful
// for `go run ./cmd/oscillitron` smoke testing; the structured trace
// (--v) is the machine-friendly view.
func printSummary(w *os.File, res runner.Result, task string) {
	fmt.Fprintf(w, "task:        %s\n", task)
	fmt.Fprintf(w, "root id:     %s\n", res.Root.ID)
	fmt.Fprintf(w, "exit reason: %s\n", res.Root.ExitReason)

	if res.Root.Execute != nil {
		fmt.Fprintf(w, "root playbook: %s (category %s)\n",
			res.Root.Evaluate.Playbook, res.Root.Execute.Category)
	}

	if children, ok := res.Subtree[res.Root.ID]; ok && len(children) > 0 {
		fmt.Fprintf(w, "\nchildren (%d, dispatch was randomized):\n", len(children))
		for i, child := range children {
			pb := "—"
			cat := "—"
			if child.Evaluate != nil {
				pb = string(child.Evaluate.Playbook)
			}
			if child.Execute != nil {
				cat = string(child.Execute.Category)
			}
			fmt.Fprintf(w, "  %d. %s  playbook=%s  category=%s  exit=%s\n",
				i, child.ID, pb, cat, child.ExitReason)
			fmt.Fprintf(w, "     input:  %s\n", child.Input.Content)
			if child.Execute != nil && child.Execute.ReturnResult != nil {
				fmt.Fprintf(w, "     result: %s (conf %.2f)\n",
					child.Execute.ReturnResult.Result.Content,
					child.Execute.ReturnResult.Confidence)
			}
			if child.Execute != nil && child.Execute.VerifierSignal != nil {
				fmt.Fprintf(w, "     verdict: %s\n", child.Execute.VerifierSignal.Verdict)
			}
		}
	}

	fmt.Fprintln(w, "\nrecomposed result:")
	fmt.Fprintf(w, "  content:    %s\n", res.ResolvedPayload.Result.Content)
	fmt.Fprintf(w, "  confidence: %.2f\n", res.ResolvedPayload.Confidence)

	fmt.Fprintln(w, "\nruntime state:")
	fmt.Fprintf(w, "  evaluates:  %d\n", res.State.EvaluateCount)
	fmt.Fprintf(w, "  executes:   %d\n", res.State.ExecuteCount)
	fmt.Fprintf(w, "  inhibits:   %d\n", res.State.InhibitCount)
	if len(res.State.VerifierSignals) > 0 {
		fmt.Fprintf(w, "  verifier signals (%d):\n", len(res.State.VerifierSignals))
		for _, v := range res.State.VerifierSignals {
			fmt.Fprintf(w, "    %s → %s\n", v.APID, v.Verdict)
		}
	}
}
