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
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/config"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/composite"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/runner"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		taskFlag    = flag.String("task", "draft a project plan", "root task description")
		seedFlag    = flag.Uint64("seed", 42, "PCG seed for randomized sibling dispatch (0 = non-deterministic)")
		maxDepth    = flag.Int("max-depth", 4, "absolute path-length cap (belt-and-suspenders)")
		depthBudget = flag.Int("depth-budget", 3, "per-AP DepthRemaining for the root envelope")
		configFlag  = flag.String("config", "", "optional .properties config path (Stage 5+ Hermes wiring; ignored in stub mode)")
		verboseFlag = flag.Bool("v", false, "verbose tracer (slog Info events)")
	)
	flag.Parse()

	// Load config if provided — kept live so Stage 5 can lean on it
	// without reshaping the CLI surface. The properties file is
	// presently informational only (no live Hermes wiring yet).
	if *configFlag != "" {
		props, err := config.Load(*configFlag)
		if err != nil {
			return fmt.Errorf("load %s: %w", *configFlag, err)
		}
		if u := props.String("hermes.url", ""); u != "" {
			fmt.Fprintf(os.Stderr, "demo: hermes.url=%q present in config; Hermes adapter is Stage 5, falling back to stub.\n", u)
		}
	}

	// Tracer: slog (info+) when --v, otherwise silent.
	var tracer trace.Tracer = trace.Discard{}
	if *verboseFlag {
		tracer = trace.Slog{Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	}

	// Build the stub adapter. Evaluator picks a playbook by inspecting
	// the input content: the root task triggers plan; "verify" inputs
	// trigger critique; everything else is process.
	a := stub.New("demo").
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

	res, err := runner.Run(context.Background(), runner.Config{
		Adapter:    a,
		Inhibitor:  inh,
		Recomposer: recomposer.Concat{Separator: recomposer.DefaultSeparator},
		Tracer:     tracer,
		MaxDepth:   *maxDepth,
		Rand:       prng,
	}, root)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	printSummary(os.Stdout, res, *taskFlag)
	return nil
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
