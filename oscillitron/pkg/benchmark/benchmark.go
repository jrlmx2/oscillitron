// CLAUDE GENERATED
// Package benchmark drives established LLM benchmarks (GPQA Diamond,
// MATH-500, AIME, MMLU-Pro, HLE, SWE-bench) through pluggable
// Orchestrators and Graders, producing comparable quality + cost
// reports. Each Case is run through every configured Orchestrator;
// the Grader scores each Answer against the ground truth.
//
// Two strategies for the Orchestrator slot are typical:
//
//   - "frontier baseline" — one call to a strong model. Yardstick.
//   - "orchestrator" — N cheap-model attempts + a vote/synthesis
//     step. The thing whose value we're trying to measure.
//
// Both share the same interface so the report's comparison is
// trivially apples-to-apples.
//
// **Sliding window**: cases process in deterministic order so the
// Runner can compute a per-window pass-rate as the run progresses.
// Plotting that curve shows whether the orchestrator's quality
// improves with case count — the long-term thesis claim that the
// graph self-improves via playbook accumulation, retrieval growth,
// and routing changes. v0 orchestrators have no runtime self-
// improvement loop wired yet, so the window is flat in expectation;
// the measurement scaffolding is in place for when curation lands.
//
// VRAM coordination: orchestrators and graders that hit the same
// inference substrate should share one *vram.Governor. The bench
// driver in cmd/bench builds one Governor and threads it through.
package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Case is one benchmark question.
type Case struct {
	// ID identifies the case (benchmark-specific, e.g. GPQA's case ID).
	ID string
	// Prompt is the full text presented to the Orchestrator —
	// question, MCQ options if any, any framing the benchmark
	// expects.
	Prompt string
	// Expected is the ground-truth answer in the canonical form the
	// Grader compares against (letter for MCQ, number string for math,
	// free text for HLE).
	Expected string
	// Metadata carries benchmark-specific tags (difficulty, subject,
	// source IDs). Optional; never required by the Runner.
	Metadata map[string]string
}

// Loader produces benchmark cases. Implementations read from disk —
// the bench never pulls datasets from the network at runtime
// (operator downloads via huggingface-cli, commits the JSON snapshot,
// loader reads it).
type Loader interface {
	Name() string
	Load(ctx context.Context) ([]Case, error)
}

// Answer is what an Orchestrator returns for one Case.
type Answer struct {
	// Raw is the full model response (or concatenated responses for
	// N-attempt orchestrators).
	Raw string
	// Extracted is the answer in the canonical form the Grader
	// compares against. Orchestrators with internal voting/extraction
	// populate this; simple orchestrators may equal Raw.
	Extracted string
	// Calls is the number of inference calls this Answer required.
	Calls int
	// TokensUsed sums input + output tokens across all calls.
	TokensUsed int
}

// Orchestrator turns a Case into an Answer. Implementations pick the
// strategy — single call, N-attempts + vote, full call tree, etc.
type Orchestrator interface {
	Name() string
	Answer(ctx context.Context, c Case) (Answer, error)
}

// Verdict is one Grader's outcome for an Answer.
type Verdict struct {
	GraderName string
	Pass       bool
	// Score is in [0, 1]. 1.0 = perfect. Lets benchmarks with partial
	// credit (free-form rubrics) carry more signal than pass/fail.
	// For binary graders (multichoice, numeric), use 0.0 or 1.0.
	Score float64
	// Notes is free-form context — useful for debugging mis-graded
	// cases.
	Notes string
	// TokensUsed is grader-side cost (0 for non-LLM graders like
	// multichoice).
	TokensUsed int
	// Secondary verdicts populated by dual/multi graders. The Primary
	// is the canonical verdict (Pass/Score); secondaries are sanity-
	// check signals only.
	Secondary []Verdict
}

// Grader scores an Answer against a Case.
type Grader interface {
	Name() string
	Grade(ctx context.Context, c Case, a Answer) (Verdict, error)
}

// --- Runner ---

// OrchestratorResult is one orchestrator's outcome for one case.
type OrchestratorResult struct {
	OrchestratorName string
	Answer           Answer
	Verdict          Verdict
	// Err is non-nil if Orchestrator.Answer or Grader.Grade returned
	// an error. The Runner records the error and continues with the
	// next case rather than aborting the run.
	Err error
}

// CaseResult bundles every orchestrator's outcome for one case.
type CaseResult struct {
	CaseID  string
	Case    Case
	Results []OrchestratorResult // one per Runner.Orchestrators, in order
}

// AggregateStats is one orchestrator's totals over the whole run.
type AggregateStats struct {
	OrchestratorName  string
	Cases             int
	Successes         int // verdict.Pass=true count (errors excluded)
	Failures          int // verdict.Pass=false count
	Errors            int // orchestrator or grader errors
	PassRate          float64
	AvgScore          float64
	TotalCalls        int
	TotalTokens       int
	TotalGraderTokens int
}

// WindowOrchestratorStats is one orchestrator's snapshot in one
// sliding-window position.
type WindowOrchestratorStats struct {
	OrchestratorName string
	PassRate         float64
	AvgScore         float64
}

// WindowStats is one sliding-window position covering [EndCase-Size+1,
// EndCase] (inclusive, 0-indexed). EndCase is the most recent case
// processed at the time the window closed.
type WindowStats struct {
	EndCase         int
	Size            int
	PerOrchestrator []WindowOrchestratorStats
}

// Report is what Runner.Run returns.
type Report struct {
	BenchmarkName string
	StartedAt     time.Time
	EndedAt       time.Time
	Cases         []CaseResult
	Aggregates    []AggregateStats // one per orchestrator, in Runner.Orchestrators order
	// Windows carries the sliding-window evolution: one entry per
	// case-completion after the warmup (case index ≥ SlidingWindowSize-1).
	// Empty when SlidingWindowSize == 0.
	Windows []WindowStats
}

// RunnerConfig configures a Runner.
type RunnerConfig struct {
	// Loader produces the cases. Required.
	Loader Loader
	// Orchestrators run on each case in order; typical: ["frontier",
	// "orchestrator-vote"]. Required; must be non-empty.
	Orchestrators []Orchestrator
	// Grader scores each Answer. Required.
	Grader Grader
	// SlidingWindowSize controls the sliding-window stats:
	//   0 = no window stats, just aggregate.
	//   N>0 = after each case, snapshot a window covering the last N
	//   cases. Recommended values: 10 for ~200-case corpora, 50 for
	//   ~1k+ corpora.
	SlidingWindowSize int
	// Tracer emits structured per-case events. Defaults to trace.Discard.
	Tracer trace.Tracer
	// OnCase, if set, is invoked synchronously after each CaseResult
	// completes (i.e., every orchestrator + grader returned for the
	// case). Fires AFTER the window snapshot for that case so the
	// callback sees the final state.
	//
	// Typical uses: append the case to a JSONL stream for crash
	// safety on long runs (see AppendCaseJSONL); render a live
	// progress line; drive a plotting/dashboard sidecar.
	//
	// If OnCase returns an error, the runner records a
	// `benchmark.on_case_callback_error` trace event and continues —
	// callback failures don't kill the run. A panic propagates.
	OnCase func(CaseResult) error
}

// Run loads cases, runs each through every orchestrator + grader,
// and produces the Report. Cases process sequentially — sliding-
// window math assumes a temporal order. Errors on individual cases
// are recorded in the report (not propagated) so a single bad case
// can't kill a long benchmark run; the only error Run itself returns
// is from the Loader.
//
// Cancellation: ctx.Done aborts mid-run and returns the partial
// Report with whatever cases completed.
func Run(ctx context.Context, cfg RunnerConfig) (Report, error) {
	if cfg.Loader == nil {
		return Report{}, fmt.Errorf("benchmark.Run: Loader is required")
	}
	if len(cfg.Orchestrators) == 0 {
		return Report{}, fmt.Errorf("benchmark.Run: at least one Orchestrator is required")
	}
	if cfg.Grader == nil {
		return Report{}, fmt.Errorf("benchmark.Run: Grader is required")
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}

	cases, err := cfg.Loader.Load(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("benchmark.Run: loader %s: %w", cfg.Loader.Name(), err)
	}
	if len(cases) == 0 {
		return Report{}, fmt.Errorf("benchmark.Run: loader %s returned 0 cases", cfg.Loader.Name())
	}

	trace.Info(tracer, ctx, "benchmark.start",
		slog.String("benchmark", cfg.Loader.Name()),
		slog.Int("cases", len(cases)),
		slog.Int("orchestrators", len(cfg.Orchestrators)),
		slog.Int("sliding_window", cfg.SlidingWindowSize),
	)

	report := Report{
		BenchmarkName: cfg.Loader.Name(),
		StartedAt:     time.Now(),
	}

	for caseIdx, c := range cases {
		if err := ctx.Err(); err != nil {
			trace.Error(tracer, ctx, "benchmark.cancelled",
				slog.Int("completed_cases", len(report.Cases)),
				slog.String("err", err.Error()),
			)
			break
		}
		cr := CaseResult{CaseID: c.ID, Case: c}
		for _, o := range cfg.Orchestrators {
			or := OrchestratorResult{OrchestratorName: o.Name()}
			answer, oerr := o.Answer(ctx, c)
			if oerr != nil {
				or.Err = fmt.Errorf("orchestrator %s: %w", o.Name(), oerr)
				trace.Error(tracer, ctx, "benchmark.orchestrator_error",
					slog.String("case", c.ID),
					slog.String("orchestrator", o.Name()),
					slog.String("err", oerr.Error()),
				)
				cr.Results = append(cr.Results, or)
				continue
			}
			or.Answer = answer
			verdict, gerr := cfg.Grader.Grade(ctx, c, answer)
			if gerr != nil {
				or.Err = fmt.Errorf("grader %s: %w", cfg.Grader.Name(), gerr)
				trace.Error(tracer, ctx, "benchmark.grader_error",
					slog.String("case", c.ID),
					slog.String("orchestrator", o.Name()),
					slog.String("err", gerr.Error()),
				)
				cr.Results = append(cr.Results, or)
				continue
			}
			or.Verdict = verdict
			cr.Results = append(cr.Results, or)
			trace.Info(tracer, ctx, "benchmark.case_graded",
				slog.String("case", c.ID),
				slog.String("orchestrator", o.Name()),
				slog.Bool("pass", verdict.Pass),
				slog.Float64("score", verdict.Score),
				slog.String("extracted", answer.Extracted),
				slog.String("expected", c.Expected),
			)
		}
		report.Cases = append(report.Cases, cr)

		// Sliding window snapshot. Open a new window every time we
		// have at least Size completed cases; the window covers
		// [caseIdx-Size+1, caseIdx].
		if cfg.SlidingWindowSize > 0 && len(report.Cases) >= cfg.SlidingWindowSize {
			report.Windows = append(report.Windows,
				computeWindow(report.Cases, cfg.Orchestrators, cfg.SlidingWindowSize, caseIdx))
		}

		// Per-case callback. Fires after the window snapshot so the
		// caller sees the final case state. Errors are non-fatal —
		// recorded as a trace event and the run continues.
		if cfg.OnCase != nil {
			if err := cfg.OnCase(cr); err != nil {
				trace.Error(tracer, ctx, "benchmark.on_case_callback_error",
					slog.String("case", c.ID),
					slog.String("err", err.Error()),
				)
			}
		}
	}

	report.EndedAt = time.Now()
	report.Aggregates = computeAggregates(report.Cases, cfg.Orchestrators)

	trace.Info(tracer, ctx, "benchmark.done",
		slog.String("benchmark", cfg.Loader.Name()),
		slog.Int("cases", len(report.Cases)),
		slog.Duration("elapsed", report.EndedAt.Sub(report.StartedAt)),
	)
	return report, nil
}

// computeWindow snapshots one sliding-window position.
func computeWindow(allCases []CaseResult, orchs []Orchestrator, size int, endCaseIdx int) WindowStats {
	start := endCaseIdx - size + 1
	w := WindowStats{EndCase: endCaseIdx, Size: size}
	for oIdx, o := range orchs {
		passes := 0
		scoreSum := 0.0
		counted := 0
		for i := start; i <= endCaseIdx; i++ {
			r := allCases[i].Results[oIdx]
			if r.Err != nil {
				continue
			}
			counted++
			if r.Verdict.Pass {
				passes++
			}
			scoreSum += r.Verdict.Score
		}
		stat := WindowOrchestratorStats{OrchestratorName: o.Name()}
		if counted > 0 {
			stat.PassRate = float64(passes) / float64(counted)
			stat.AvgScore = scoreSum / float64(counted)
		}
		w.PerOrchestrator = append(w.PerOrchestrator, stat)
	}
	return w
}

// computeAggregates produces one AggregateStats per orchestrator.
func computeAggregates(cases []CaseResult, orchs []Orchestrator) []AggregateStats {
	aggs := make([]AggregateStats, len(orchs))
	for oIdx, o := range orchs {
		aggs[oIdx].OrchestratorName = o.Name()
		aggs[oIdx].Cases = len(cases)
		var scoreSum float64
		var graded int
		for _, cr := range cases {
			r := cr.Results[oIdx]
			if r.Err != nil {
				aggs[oIdx].Errors++
				continue
			}
			if r.Verdict.Pass {
				aggs[oIdx].Successes++
			} else {
				aggs[oIdx].Failures++
			}
			scoreSum += r.Verdict.Score
			aggs[oIdx].TotalCalls += r.Answer.Calls
			aggs[oIdx].TotalTokens += r.Answer.TokensUsed
			aggs[oIdx].TotalGraderTokens += r.Verdict.TokensUsed
			graded++
		}
		if graded > 0 {
			aggs[oIdx].PassRate = float64(aggs[oIdx].Successes) / float64(graded)
			aggs[oIdx].AvgScore = scoreSum / float64(graded)
		}
	}
	return aggs
}
