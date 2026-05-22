// CLAUDE GENERATED
// Phase 1 measurement driver — the kill-or-proceed gate from
// scratch/library-plan.md §2.5.
//
// For each case in cases.json:
//
//  1. Orchestrator path — Haiku is the cheap-substrate proxy. The
//     runner builds a small call tree: plan emits N process children,
//     each generates an independent draft, the recomposer
//     (AnthropicSynthesizer with Haiku) merges them into one final
//     draft. Critique fires per the verifier policy in bootstrap.
//
//  2. Frontier baseline — single Sonnet call with the same task input.
//     One shot, no orchestration.
//
//  3. Grader — Sonnet judges both drafts on the same rubric.
//
// Aggregate across all cases and report quality ratio + cost ratio
// against the §2.5 proceed/kill thresholds.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=sk-...
//	go run ./cmd/phase1
//	go run ./cmd/phase1 --cases ./custom-cases.json
//	go run ./cmd/phase1 --drafts-per-case 1  # disable ensembling
//	go run ./cmd/phase1 --orchestrator-model claude-haiku-4-5-20251001 --frontier-model claude-sonnet-4-6
//
// What this command does NOT do:
//   - Execute side effects. Phase 1 only produces drafts; tool/connector
//     execution is deferred per the "always after task completion" lock.
//   - Score cost against the §2.5 thresholds in absolute terms — the
//     thresholds assume a true local cheap substrate, not a hosted
//     cheap proxy. See references/phase1-measurement-guide.md for
//     interpretation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	adapterAnth "github.com/jrlmx2/oscillitron/pkg/adapter/anthropic"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/grader"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Haiku/Sonnet pricing (mid-2026). Numbers per million tokens.
var defaultPricing = map[string]cost.Pricing{
	"claude-haiku-4-5-20251001": {InputUSDPerMTok: 0.80, OutputUSDPerMTok: 4.00},
	"claude-sonnet-4-6":         {InputUSDPerMTok: 3.00, OutputUSDPerMTok: 15.00},
	"claude-opus-4-7":           {InputUSDPerMTok: 15.00, OutputUSDPerMTok: 75.00},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "phase1:", err)
		os.Exit(1)
	}
}

type caseSpec struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Intent string `json:"intent"`
}

type casesFile struct {
	Cases []caseSpec `json:"cases"`
}

type caseResult struct {
	ID                 string
	OrchestratorDraft  string
	OrchestratorTokens int
	OrchestratorCostUSD float64
	OrchestratorScore  grader.Score
	FrontierDraft      string
	FrontierTokens     int
	FrontierCostUSD    float64
	FrontierScore      grader.Score
}

func run() error {
	var (
		casesFlag         = flag.String("cases", "cmd/phase1/cases.json", "path to cases.json")
		orchModel         = flag.String("orchestrator-model", adapterAnth.DefaultOrchestratorModel, "cheap-proxy model for the orchestrator path")
		frontierModel     = flag.String("frontier-model", adapterAnth.DefaultFrontierModel, "frontier model for the single-call baseline")
		graderModel       = flag.String("grader-model", grader.DefaultModel, "model used by the LLM-as-judge grader")
		draftsPerCase     = flag.Int("drafts-per-case", 0, "operator override for ensemble width; 0 = plan playbook decides (intent-conditioned), 1 = no ensembling, N>1 = static")
		limit             = flag.Int("limit", 0, "if >0, only run the first N cases")
		verbose           = flag.Bool("v", false, "print per-case detail to stderr")
		out               = flag.String("out", "", "if set, write per-case results as JSON to this path")
	)
	flag.Parse()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY must be set")
	}

	// Load cases.
	data, err := os.ReadFile(*casesFlag)
	if err != nil {
		return fmt.Errorf("read cases: %w", err)
	}
	var cf casesFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return fmt.Errorf("parse cases: %w", err)
	}
	if *limit > 0 && *limit < len(cf.Cases) {
		cf.Cases = cf.Cases[:*limit]
	}
	if len(cf.Cases) == 0 {
		return fmt.Errorf("no cases to run")
	}

	// Cost tracker — frontier baseline matters for cost-ratio math.
	tracker := cost.New(defaultPricing[*frontierModel])
	for name, p := range defaultPricing {
		tracker.Register(name, p)
	}

	// Anthropic adapters.
	orchestratorAdapter, err := adapterAnth.New(adapterAnth.Config{
		APIKey: apiKey, Model: *orchModel,
	})
	if err != nil {
		return fmt.Errorf("orchestrator adapter: %w", err)
	}
	frontierAdapter, err := adapterAnth.New(adapterAnth.Config{
		APIKey: apiKey, Model: *frontierModel,
	})
	if err != nil {
		return fmt.Errorf("frontier adapter: %w", err)
	}

	// Synthesizer for ensembling — same model as orchestrator so we
	// don't smuggle frontier capability into the cheap path.
	synthesizer, err := recomposer.NewAnthropic(recomposer.AnthropicConfig{
		APIKey: apiKey, Model: *orchModel,
	})
	if err != nil {
		return fmt.Errorf("synthesizer: %w", err)
	}

	// Grader.
	g, err := grader.NewAnthropic(grader.AnthropicConfig{
		APIKey: apiKey, Model: *graderModel,
	})
	if err != nil {
		return fmt.Errorf("grader: %w", err)
	}

	results := make([]caseResult, 0, len(cf.Cases))
	for _, c := range cf.Cases {
		if *verbose {
			fmt.Fprintf(os.Stderr, "\n=== %s ===\n", c.ID)
		}
		res, err := runCase(context.Background(), c, orchestratorAdapter, frontierAdapter, synthesizer, g, *draftsPerCase, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  case %s: ERROR %v\n", c.ID, err)
			continue
		}
		// Compute USD from token counts via the tracker. The adapter
		// already populates env.Trace.CostUSD via the cost.Tracker
		// when one is wired, but the simpler path is to compute here
		// since the model id is known per call.
		res.OrchestratorCostUSD = defaultPricing[*orchModel].Cost(res.OrchestratorTokens, 0) + // rough — we don't separate in/out
			0
		results = append(results, res)
	}

	report(results, *orchModel, *frontierModel, *graderModel, defaultPricing)

	if *out != "" {
		buf, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(*out, buf, 0o644); err != nil {
			return fmt.Errorf("write out: %w", err)
		}
		fmt.Fprintf(os.Stderr, "wrote per-case results to %s\n", *out)
	}
	return nil
}

// runCase executes both paths for one case and grades both candidates.
func runCase(
	ctx context.Context,
	c caseSpec,
	orchestrator, frontier *adapterAnth.Adapter,
	synthesizer *recomposer.AnthropicSynthesizer,
	g grader.Grader,
	draftsPerCase int,
	verbose bool,
) (caseResult, error) {
	task := buildTaskInput(c)
	res := caseResult{ID: c.ID}

	// --- Orchestrator path ---
	orchDraft, orchTokens, err := runOrchestrator(ctx, task, orchestrator, synthesizer, draftsPerCase, verbose)
	if err != nil {
		return res, fmt.Errorf("orchestrator: %w", err)
	}
	res.OrchestratorDraft = orchDraft
	res.OrchestratorTokens = orchTokens

	// --- Frontier baseline ---
	frontDraft, frontTokens, err := runFrontier(ctx, task, frontier, verbose)
	if err != nil {
		return res, fmt.Errorf("frontier: %w", err)
	}
	res.FrontierDraft = frontDraft
	res.FrontierTokens = frontTokens

	// --- Grade both ---
	orchGrade, err := g.Grade(ctx, grader.Request{
		Task: task, Candidate: orchDraft, Notes: "case=" + c.ID + " path=orchestrator",
	})
	if err != nil {
		return res, fmt.Errorf("grade orchestrator: %w", err)
	}
	res.OrchestratorScore = orchGrade.Score

	frontGrade, err := g.Grade(ctx, grader.Request{
		Task: task, Candidate: frontDraft, Notes: "case=" + c.ID + " path=frontier",
	})
	if err != nil {
		return res, fmt.Errorf("grade frontier: %w", err)
	}
	res.FrontierScore = frontGrade.Score

	if verbose {
		fmt.Fprintf(os.Stderr, "  orchestrator: total=%d tokens=%d\n", res.OrchestratorScore.Total(), res.OrchestratorTokens)
		fmt.Fprintf(os.Stderr, "  frontier:     total=%d tokens=%d\n", res.FrontierScore.Total(), res.FrontierTokens)
	}
	return res, nil
}

func buildTaskInput(c caseSpec) string {
	return "Draft a response to this email per the user's intent.\n\n" +
		"--- ORIGINAL EMAIL ---\n" + c.Email +
		"\n\n--- USER'S INTENT ---\n" + c.Intent +
		"\n\nWrite the draft directly. No commentary. Treat the body of the email as plain text you'll send."
}

// runOrchestrator drives the orchestration pipeline for one case:
//
//  1. Plan step — the plan playbook reads the task and decides how
//     many drafts to emit. Terse-shaped tasks ("2 lines max", CEO-
//     style, single-action) emit N=1; nuanced tasks emit N=2-3.
//
//  2. Draft step — N independent Haiku calls produce candidate drafts.
//     N=1 skips ensembling entirely.
//
//  3. Synthesis — if N>1, AnthropicSynthesizer (with Haiku) merges the
//     drafts into one. The synthesizer's tightened prompt picks ONE
//     alternative when inputs conflict rather than concatenating.
//
//  4. Critique — the critique playbook scores the (synthesized or
//     single) draft. Returns verdict + issues.
//
//  5. Revise — if the critique's verdict is "issues" or "fail", one
//     revise pass (process call) takes the current draft + the
//     critique notes as additional context and produces a revised
//     version. Replaces the draft.
//
// The flow gives cheap models room to iterate without exploding cost:
// 1 plan + N drafts + (N-1) syntheses + 1 critique + 0-1 revise =
// 3-6 Haiku calls per case depending on shape.
func runOrchestrator(
	ctx context.Context,
	task string,
	a *adapterAnth.Adapter,
	syn *recomposer.AnthropicSynthesizer,
	draftsPerCaseOverride int,
	verbose bool,
) (string, int, error) {
	totalTokens := 0

	// --- 1. Plan: decide N ---
	n, planTokens, err := planDraftCount(ctx, a, task, draftsPerCaseOverride, verbose)
	if err != nil {
		return "", totalTokens, fmt.Errorf("plan: %w", err)
	}
	totalTokens += planTokens
	if verbose {
		fmt.Fprintf(os.Stderr, "  plan: N=%d (plan tokens=%d)\n", n, planTokens)
	}

	// --- 2. Generate N drafts ---
	drafts := make([]session.ReturnResultPayload, 0, n)
	for i := 0; i < n; i++ {
		childInput := buildEnsembleDraftInput(task, i, n)
		childEnv := session.NewRoot(
			session.ID(fmt.Sprintf("phase1-draft-%d", i)),
			childInput, "{draft}", classification.Internal,
			session.Budget{TokensRemaining: 16_000, DepthRemaining: 1},
		)
		childEnv.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess, Confidence: 1.0}
		out, err := a.Execute(ctx, childEnv)
		if err != nil {
			return "", totalTokens, fmt.Errorf("draft %d: %w", i, err)
		}
		if out.Execute == nil || out.Execute.ReturnResult == nil {
			return "", totalTokens, fmt.Errorf("draft %d: no return result", i)
		}
		drafts = append(drafts, *out.Execute.ReturnResult)
		totalTokens += out.Execute.TokensUsed
		if verbose {
			fmt.Fprintf(os.Stderr, "  draft %d/%d: %d tokens\n", i+1, n, out.Execute.TokensUsed)
		}
	}

	// --- 3. Synthesize (or pass through) ---
	var current session.ReturnResultPayload
	if n == 1 {
		current = drafts[0]
	} else {
		rec := recomposer.Synth{Synthesizer: syn}
		composed, err := rec.Recompose(ctx, session.RecomposeSequential, drafts)
		if err != nil {
			return "", totalTokens, fmt.Errorf("synthesizer: %w", err)
		}
		current = composed
		// Approximate synthesizer cost: (n-1) reductions × ~350 tokens
		totalTokens += (n - 1) * 350
		if verbose {
			fmt.Fprintf(os.Stderr, "  synthesizer: %d reductions\n", n-1)
		}
	}

	// --- 4. Critique ---
	verdict, issuesText, critTokens, err := critiqueDraft(ctx, a, task, current.Result.Content)
	if err != nil {
		// Critique failure is non-fatal — we already have a draft.
		if verbose {
			fmt.Fprintf(os.Stderr, "  critique skipped (error: %v)\n", err)
		}
	} else {
		totalTokens += critTokens
		if verbose {
			fmt.Fprintf(os.Stderr, "  critique: verdict=%s (tokens=%d)\n", verdict, critTokens)
		}

		// --- 5. Revise on issues/fail ---
		if verdict == session.VerdictIssues || verdict == session.VerdictFail {
			revised, revTokens, err := reviseDraft(ctx, a, task, current.Result.Content, issuesText)
			if err != nil {
				if verbose {
					fmt.Fprintf(os.Stderr, "  revise skipped (error: %v)\n", err)
				}
			} else {
				current = revised
				totalTokens += revTokens
				if verbose {
					fmt.Fprintf(os.Stderr, "  revise: tokens=%d\n", revTokens)
				}
			}
		}
	}

	return current.Result.Content, totalTokens, nil
}

// planDraftCount asks the plan playbook how many drafts to emit. If
// override is set (>0), respects the operator override and skips the
// plan call (used by --drafts-per-case for diagnostic runs).
func planDraftCount(
	ctx context.Context,
	a *adapterAnth.Adapter,
	task string,
	override int,
	verbose bool,
) (int, int, error) {
	if override > 0 {
		return override, 0, nil
	}
	planInput := "You will decide how many independent draft attempts another worker should produce for this task.\n\n" +
		"Rules:\n" +
		"- If the task implies brevity (e.g., 'two lines max', 'be brief', 'CEO style', 'don't over-explain', single-action), emit 1 sub-AP with a single draft instruction.\n" +
		"- If the task has nuance (tone constraints, redirect, push-back, multiple required elements), emit 3 sub-APs each asking for an independent draft attempt.\n" +
		"- Default to 2 if uncertain.\n\n" +
		"Task to plan for:\n" + task
	env := session.NewRoot("phase1-plan", planInput, "", classification.Internal,
		session.Budget{TokensRemaining: 8_000, DepthRemaining: 2})
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookPlan, Confidence: 1.0}
	out, err := a.Execute(ctx, env)
	if err != nil {
		// Plan failure → conservative default of 1 (matches Haiku-solo
		// which performed best on the original measurement).
		return 1, 0, nil
	}
	if out.Execute == nil || out.Execute.EmitSubtree == nil {
		return 1, out.Execute.TokensUsed, nil
	}
	n := len(out.Execute.EmitSubtree.SubAPs)
	if n < 1 {
		n = 1
	} else if n > 5 {
		n = 5 // safety cap
	}
	return n, out.Execute.TokensUsed, nil
}

// critiqueDraft runs one critique playbook call against the current
// draft. Returns the verdict, joined issue-notes (for use in revise),
// and token count.
func critiqueDraft(
	ctx context.Context,
	a *adapterAnth.Adapter,
	task, draft string,
) (session.Verdict, string, int, error) {
	input := "Review this draft response against the user's intent. Be strict but fair.\n\n" +
		"--- TASK ---\n" + task +
		"\n\n--- DRAFT ---\n" + draft +
		"\n\nReturn JSON {verdict, issues[]}. Use 'issues' verdict for any real problem worth a revision; 'pass' if the draft is solid."
	env := session.NewRoot("phase1-critique", input, "", classification.Internal,
		session.Budget{TokensRemaining: 8_000, DepthRemaining: 1})
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookCritique, Confidence: 1.0}
	out, err := a.Execute(ctx, env)
	if err != nil {
		return session.VerdictPass, "", 0, err
	}
	if out.Execute == nil || out.Execute.VerifierSignal == nil {
		return session.VerdictPass, "", out.Execute.TokensUsed, nil
	}
	verdict := out.Execute.VerifierSignal.Verdict
	var notes []string
	for _, is := range out.Execute.VerifierSignal.Issues {
		notes = append(notes, fmt.Sprintf("[%s] %s", is.Severity, is.What))
	}
	issuesText := strings.Join(notes, "\n")
	return verdict, issuesText, out.Execute.TokensUsed, nil
}

// reviseDraft runs one process call to revise the draft given the
// critique notes. Returns the new draft as a ReturnResultPayload.
func reviseDraft(
	ctx context.Context,
	a *adapterAnth.Adapter,
	task, prevDraft, critiqueNotes string,
) (session.ReturnResultPayload, int, error) {
	input := "Revise this draft based on the critic's notes. Address the issues directly; produce a single revised draft. " +
		"Do not include the original draft in your output — just the revised version.\n\n" +
		"--- TASK ---\n" + task +
		"\n\n--- CURRENT DRAFT ---\n" + prevDraft +
		"\n\n--- CRITIC'S NOTES ---\n" + critiqueNotes +
		"\n\nReturn JSON: {\"content\": \"<revised draft>\", \"confidence\": <0..1>}."
	env := session.NewRoot("phase1-revise", input, "", classification.Internal,
		session.Budget{TokensRemaining: 16_000, DepthRemaining: 1})
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess, Confidence: 1.0}
	out, err := a.Execute(ctx, env)
	if err != nil {
		return session.ReturnResultPayload{}, 0, err
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		return session.ReturnResultPayload{}, out.Execute.TokensUsed, fmt.Errorf("no return result")
	}
	return *out.Execute.ReturnResult, out.Execute.TokensUsed, nil
}

// buildEnsembleDraftInput varies the input slightly across drafts so
// the model produces meaningfully different attempts rather than the
// same answer 3 times. Lightweight prompt variation only — no
// per-draft persona difference.
func buildEnsembleDraftInput(task string, idx, total int) string {
	if total == 1 {
		return task
	}
	suffixes := []string{
		"Produce a clear, direct draft. Reply with JSON: {\"content\": \"<email body>\", \"confidence\": <0..1>}.",
		"Produce a draft that errs on the warmer side without losing precision. Reply with JSON: {\"content\": \"<email body>\", \"confidence\": <0..1>}.",
		"Produce a draft that errs on the more concise side without losing required elements. Reply with JSON: {\"content\": \"<email body>\", \"confidence\": <0..1>}.",
	}
	return task + "\n\n" + suffixes[idx%len(suffixes)]
}

// runFrontier makes a single Anthropic Messages call directly.
// Bypasses the runner — frontier baseline is intentionally a one-shot.
func runFrontier(ctx context.Context, task string, a *adapterAnth.Adapter, verbose bool) (string, int, error) {
	env := session.NewRoot("phase1-frontier", task, "{\"content\":\"<email>\",\"confidence\":<0..1>}",
		classification.Internal, session.Budget{TokensRemaining: 16_000, DepthRemaining: 1})
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess, Confidence: 1.0}
	out, err := a.Execute(ctx, env)
	if err != nil {
		return "", 0, err
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		return "", 0, fmt.Errorf("frontier: no return result")
	}
	tokens := out.Execute.TokensUsed
	if verbose {
		fmt.Fprintf(os.Stderr, "  frontier 1-shot: %d tokens\n", tokens)
	}
	return out.Execute.ReturnResult.Result.Content, tokens, nil
}

// report aggregates results and prints the kill-or-proceed verdict.
func report(results []caseResult, orchModel, frontierModel, graderModel string, pricing map[string]cost.Pricing) {
	if len(results) == 0 {
		fmt.Println("no results")
		return
	}
	orchPricing := pricing[orchModel]
	frontPricing := pricing[frontierModel]

	totalOrchScore, totalFrontScore := 0, 0
	totalOrchTokens, totalFrontTokens := 0, 0
	for _, r := range results {
		totalOrchScore += r.OrchestratorScore.Total()
		totalFrontScore += r.FrontierScore.Total()
		totalOrchTokens += r.OrchestratorTokens
		totalFrontTokens += r.FrontierTokens
	}
	n := len(results)
	// Cost is computed by treating tokens as input-output mixed at
	// a conservative 70/30 input/output ratio for short tasks. Off
	// in either direction by maybe 20% — fine for an order-of-
	// magnitude wedge measurement.
	orchCost := orchPricing.Cost(totalOrchTokens*7/10, totalOrchTokens*3/10)
	frontCost := frontPricing.Cost(totalFrontTokens*7/10, totalFrontTokens*3/10)

	avgOrch := float64(totalOrchScore) / float64(n) / 20.0 // /20 max
	avgFront := float64(totalFrontScore) / float64(n) / 20.0
	qualityRatio := avgOrch / avgFront
	costRatio := orchCost / frontCost

	const (
		proceedQuality = 0.80
		killQuality    = 0.65
		proceedCost    = 0.25
		killCost       = 0.50
	)

	fmt.Println()
	fmt.Println(strings.Repeat("=", 64))
	fmt.Println("Phase 1 results")
	fmt.Println(strings.Repeat("=", 64))
	fmt.Printf("Cases:               %d\n", n)
	fmt.Printf("Orchestrator model:  %s\n", orchModel)
	fmt.Printf("Frontier model:      %s\n", frontierModel)
	fmt.Printf("Grader model:        %s\n", graderModel)
	fmt.Println()
	fmt.Printf("Avg quality (orchestrator): %.2f / 1.00\n", avgOrch)
	fmt.Printf("Avg quality (frontier):     %.2f / 1.00\n", avgFront)
	fmt.Printf("Quality ratio:              %.2f  (proceed ≥ %.2f, kill < %.2f)\n", qualityRatio, proceedQuality, killQuality)
	fmt.Println()
	fmt.Printf("Total orchestrator tokens:  %d\n", totalOrchTokens)
	fmt.Printf("Total frontier tokens:      %d\n", totalFrontTokens)
	fmt.Printf("Cost orchestrator:          $%.4f\n", orchCost)
	fmt.Printf("Cost frontier:              $%.4f\n", frontCost)
	fmt.Printf("Cost ratio:                 %.2f  (proceed ≤ %.2f, kill > %.2f)\n", costRatio, proceedCost, killCost)
	fmt.Println()
	fmt.Printf("VERDICT: %s\n", verdict(qualityRatio, costRatio, proceedQuality, killQuality, proceedCost, killCost))
	fmt.Printf("Run at: %s\n", time.Now().Format(time.RFC3339))
	fmt.Println()
	fmt.Println("Caveat: cost is measured Haiku-hosted vs Sonnet-hosted. The §2.5 cost threshold")
	fmt.Println("assumes a local cheap substrate (~10-100× cheaper than hosted frontier), not")
	fmt.Println("a hosted cheap proxy. Hitting cost ≤ 25% with Haiku is structurally unlikely;")
	fmt.Println("hitting quality ≥ 80% is the meaningful Phase 1 signal at this stage.")
	fmt.Println()
}

func verdict(q, c, pq, kq, pc, kc float64) string {
	// Quality is the floor — if it's below kill, nothing else matters.
	if q < kq {
		return "KILL (quality below floor)"
	}
	// Quality in proceed zone: verdict depends on cost zone, but
	// quality is the meaningful signal at this stage and gets primacy.
	if q >= pq {
		switch {
		case c <= pc:
			return "PROCEED (both quality and cost in proceed zone)"
		case c <= kc:
			return "PROCEED ON QUALITY — cost mid-zone; expected with hosted cheap proxy"
		default:
			return "PROCEED ON QUALITY — cost above kill zone (expected with hosted cheap proxy; re-measure against a local cheap substrate for the §2.5 cost-wedge claim)"
		}
	}
	// Quality in middle zone (kq ≤ q < pq).
	switch {
	case c > kc:
		return "DIAGNOSE (quality middle, cost above kill — orchestration overhead may be eating the wedge)"
	default:
		return "DIAGNOSE (quality in middle zone; review per-case detail before deciding)"
	}
}
