package grader

import (
	"context"
	"fmt"
	"sync"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// Dual wraps a Primary grader and N Secondary graders. Every Answer
// gets graded by all of them in parallel; the Primary verdict is
// returned canonically (Pass/Score), while Secondary verdicts are
// attached as Verdict.Secondary for sanity-check comparison.
//
// Use cases:
//
//   - Bootstrap a local LLM judge by running it alongside a frontier
//     judge until the operator trusts the agreement rate.
//   - Run an MCQ extract-and-compare grader as Primary plus an LLM
//     judge as Secondary, to flag cases where the extractor gets a
//     wrong letter that nonetheless reflects a correct answer
//     (model said "the answer is the second option" without naming
//     the letter).
//
// Errors are non-fatal at the Secondary level — a failing secondary
// is recorded as a Verdict with GraderName="<error: …>" and Pass=false.
// A failing Primary propagates as the function-level error.
type Dual struct {
	// Primary is the canonical verdict source. Required.
	Primary benchmark.Grader
	// Secondary graders run alongside; their verdicts are reported
	// but never override Primary's Pass/Score. May be empty (in
	// which case Dual is just a passthrough to Primary).
	Secondary []benchmark.Grader
	// NameStr appears in benchmark reports. Defaults to
	// "dual(<primary>+<N secondary>)".
	NameStr string
}

// Name implements benchmark.Grader.
func (d Dual) Name() string {
	if d.NameStr != "" {
		return d.NameStr
	}
	primaryName := ""
	if d.Primary != nil {
		primaryName = d.Primary.Name()
	}
	return fmt.Sprintf("dual(%s+%d-secondary)", primaryName, len(d.Secondary))
}

// Grade implements benchmark.Grader. Runs Primary inline (this
// goroutine) and Secondaries in parallel goroutines, then assembles
// the final Verdict.
func (d Dual) Grade(ctx context.Context, c benchmark.Case, a benchmark.Answer) (benchmark.Verdict, error) {
	if d.Primary == nil {
		return benchmark.Verdict{}, fmt.Errorf("dual: Primary is required")
	}

	type secResult struct {
		verdict benchmark.Verdict
		err     error
	}
	secResults := make([]secResult, len(d.Secondary))
	var wg sync.WaitGroup
	for i, s := range d.Secondary {
		i, s := i, s
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.Grade(ctx, c, a)
			secResults[i] = secResult{v, err}
		}()
	}

	primary, perr := d.Primary.Grade(ctx, c, a)
	wg.Wait()

	if perr != nil {
		return primary, fmt.Errorf("dual: primary %s: %w", d.Primary.Name(), perr)
	}

	// Attach secondaries (even errors — operator wants to see
	// which secondary failed and why).
	for i, sr := range secResults {
		if sr.err != nil {
			primary.Secondary = append(primary.Secondary, benchmark.Verdict{
				GraderName: d.Secondary[i].Name(),
				Notes:      "error: " + sr.err.Error(),
			})
			continue
		}
		primary.Secondary = append(primary.Secondary, sr.verdict)
	}
	return primary, nil
}

// Agreement reports the share of Secondary verdicts whose Pass field
// matches Primary's Pass. Returns 1.0 when all secondaries agree, 0.0
// when none, NaN-equivalent (-1) when there are no secondaries to
// compare. Useful for the bench driver's summary output ("local
// judge agrees with frontier 0.92 of the time").
func Agreement(v benchmark.Verdict) float64 {
	if len(v.Secondary) == 0 {
		return -1
	}
	agree := 0
	for _, s := range v.Secondary {
		if s.Pass == v.Pass {
			agree++
		}
	}
	return float64(agree) / float64(len(v.Secondary))
}

// Compile-time check.
var _ benchmark.Grader = Dual{}
