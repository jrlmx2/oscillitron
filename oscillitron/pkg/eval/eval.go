// CLAUDE GENERATED
// Package eval is the grader harness for Phase 1's empirical
// validation (library-plan §2.3, §2.5). It runs a workload of Tasks
// through a caller-supplied Runner, grades each result with a
// Grader, and produces a Report the kill-or-proceed gate reads
// against.
//
// Decoupled from the orchestrator on purpose: the Runner signature
// is `func(ctx, Task) (string, error)` so eval doesn't know whether
// the output came from a single adapter call, a full Oscillitron
// chain, or a frontier baseline. That keeps the three comparison
// arms in §9 step 11 trivially substitutable.
//
// Ships a substring-match Grader as the v0 stub. LLM-as-judge and
// rules-DSL graders are deferred (library-plan §6: deferred-but-seam
// reserved).
package eval

import (
	"context"
	"errors"
	"strings"
)

// Task is one workload item.
type Task struct {
	ID        string
	Prompt    string
	Reference string // ground-truth answer or a substring expected in the output; optional
	Tags      []string
}

// Score is the grader's verdict on a single output.
type Score struct {
	Value    float64 // 0..1
	Reason   string
	Metadata map[string]string
}

// Grader turns a (task, output) pair into a Score.
type Grader interface {
	Grade(ctx context.Context, task Task, output string) (Score, error)
}

// Runner is what eval calls to produce an output for a task. Eval
// has no opinions about what the runner does internally —
// single-shot adapter call, multi-hop chain, or frontier baseline.
type Runner func(ctx context.Context, task Task) (string, error)

// Result is one task's full record.
type Result struct {
	Task   Task
	Output string
	Score  Score
	Err    error // non-nil if Runner or Grader errored; Score may be zero
}

// Report aggregates results.
type Report struct {
	Results       []Result
	Total         int
	Errored       int
	MeanScore     float64
	PassThreshold float64 // optional; results with Value >= PassThreshold count as pass
	PassCount     int
	PassRate      float64
}

// RunOpts configures Run.
type RunOpts struct {
	PassThreshold float64 // 0 disables pass/fail counting
}

// Run drives a workload end-to-end: for each task, call runner to
// produce an output, grade it, accumulate. Continues on error
// (records the error in the Result) — a single failure shouldn't
// abort the whole eval, since cost/savings totals are still
// meaningful with partial results.
func Run(ctx context.Context, tasks []Task, runner Runner, grader Grader, opts RunOpts) (Report, error) {
	if runner == nil {
		return Report{}, errors.New("eval: runner is nil")
	}
	if grader == nil {
		return Report{}, errors.New("eval: grader is nil")
	}

	report := Report{
		Results:       make([]Result, 0, len(tasks)),
		Total:         len(tasks),
		PassThreshold: opts.PassThreshold,
	}
	var sum float64
	scored := 0

	for _, task := range tasks {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		res := Result{Task: task}
		output, runErr := runner(ctx, task)
		res.Output = output
		if runErr != nil {
			res.Err = runErr
			report.Errored++
			report.Results = append(report.Results, res)
			continue
		}
		score, gradeErr := grader.Grade(ctx, task, output)
		if gradeErr != nil {
			res.Err = gradeErr
			report.Errored++
			report.Results = append(report.Results, res)
			continue
		}
		res.Score = score
		sum += score.Value
		scored++
		if opts.PassThreshold > 0 && score.Value >= opts.PassThreshold {
			report.PassCount++
		}
		report.Results = append(report.Results, res)
	}

	if scored > 0 {
		report.MeanScore = sum / float64(scored)
	}
	if opts.PassThreshold > 0 && scored > 0 {
		report.PassRate = float64(report.PassCount) / float64(scored)
	}
	return report, nil
}

// SubstringGrader scores 1.0 if task.Reference appears in the output
// (case-insensitive), 0.0 otherwise. Tasks with an empty Reference
// score 0.0 with a "no reference" reason — they shouldn't have been
// graded.
//
// v0 stub. Useful for sanity-checking the harness wiring; replace
// with a real grader (rules DSL, LLM-as-judge) before believing the
// numbers.
type SubstringGrader struct{}

// Grade implements Grader.
func (SubstringGrader) Grade(_ context.Context, task Task, output string) (Score, error) {
	if task.Reference == "" {
		return Score{Value: 0, Reason: "no reference provided"}, nil
	}
	if strings.Contains(strings.ToLower(output), strings.ToLower(task.Reference)) {
		return Score{Value: 1.0, Reason: "reference substring matched"}, nil
	}
	return Score{Value: 0, Reason: "reference substring not found"}, nil
}

var _ Grader = SubstringGrader{}
