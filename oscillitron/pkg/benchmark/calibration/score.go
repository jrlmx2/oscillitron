package calibration

import (
	"fmt"
	"math"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// confOf selects which confidence column to score from an Answer.
// Two real columns exist on benchmark.Answer.
type confOf func(benchmark.Answer) float64

// SelfReported reads Answer.Confidence (the self-reported mean).
func SelfReported(a benchmark.Answer) float64 { return a.Confidence }

// SemanticEntropy reads Answer.SEConfidence (the v0 SE column).
func SemanticEntropy(a benchmark.Answer) float64 { return a.SEConfidence }

// CalibrationScore bundles the three reliability metrics for one
// (orchestrator, confidence-column) pair.
type CalibrationScore struct {
	OrchestratorName string
	Column           string  // "confidence" | "se_confidence"
	N                int     // cases scored (column > 0, no err)
	ECE              float64 // Expected Calibration Error (lower=better)
	Brier            float64 // Brier score (lower=better)
	ReliabilitySlope float64 // OLS slope of band pass-rate vs band mean-conf (steeper=more informative)
}

// Score computes ECE / Brier / reliability-slope for one confidence
// column across every orchestrator in the report, using bands for
// the ECE binning (nil → DefaultBands).
//
// Methodology:
//   - ECE (Guo et al., ICML 2017; Naeini et al., AAAI 2015):
//     ECE = Σ_b (n_b/N)·|acc(b) − conf(b)| — the weighted gap
//     between per-bin accuracy and per-bin mean confidence.
//   - Brier (Brier 1950): (1/N)·Σ_i (conf_i − pass_i)² with
//     pass_i ∈ {0,1}.
//   - ReliabilitySlope: count-weighted OLS slope of band-pass-rate
//     (y) on band-mean-confidence (x); positive + steep ⇒ the column
//     ranks correctness well (flat ⇒ noise).
//
// Cases with column value ≤ 0 (not reported / not computed) and
// cases with a non-nil Err are excluded, identical to Compute's rule.
func Score(report benchmark.Report, sel confOf, column string, bands []Band) []CalibrationScore {
	if bands == nil {
		bands = DefaultBands
	}

	type bin struct {
		count, passes int
		confSum       float64
	}
	type acc struct {
		n        int
		brierSum float64
		bins     []bin
	}
	accs := make(map[string]*acc, len(report.Aggregates))
	for _, a := range report.Aggregates {
		accs[a.OrchestratorName] = &acc{bins: make([]bin, len(bands))}
	}

	for _, cr := range report.Cases {
		for _, or := range cr.Results {
			if or.Err != nil {
				continue
			}
			conf := sel(or.Answer)
			if conf <= 0 {
				continue
			}
			a := accs[or.OrchestratorName]
			if a == nil {
				continue
			}
			pass := 0.0
			if or.Verdict.Pass {
				pass = 1.0
			}
			a.n++
			a.brierSum += (conf - pass) * (conf - pass)
			if bi := pickBand(conf, bands); bi >= 0 {
				a.bins[bi].count++
				a.bins[bi].confSum += conf
				if or.Verdict.Pass {
					a.bins[bi].passes++
				}
			}
		}
	}

	var out []CalibrationScore
	for _, agg := range report.Aggregates {
		a := accs[agg.OrchestratorName]
		if a == nil || a.n == 0 {
			continue
		}
		var ece float64
		var xs, ys, ws []float64
		for _, b := range a.bins {
			if b.count == 0 {
				continue
			}
			binAcc := float64(b.passes) / float64(b.count)
			binConf := b.confSum / float64(b.count)
			ece += (float64(b.count) / float64(a.n)) * math.Abs(binAcc-binConf)
			xs = append(xs, binConf)
			ys = append(ys, binAcc)
			ws = append(ws, float64(b.count))
		}
		out = append(out, CalibrationScore{
			OrchestratorName: agg.OrchestratorName,
			Column:           column,
			N:                a.n,
			ECE:              ece,
			Brier:            a.brierSum / float64(a.n),
			ReliabilitySlope: olsSlope(xs, ys, ws),
		})
	}
	return out
}

// olsSlope returns the count-weighted ordinary-least-squares slope of
// y on x. Returns 0 when there are < 2 distinct x values (no slope to
// estimate) or zero total weight.
func olsSlope(xs, ys, ws []float64) float64 {
	var sw, swx, swy float64
	for i := range xs {
		sw += ws[i]
		swx += ws[i] * xs[i]
		swy += ws[i] * ys[i]
	}
	if sw == 0 {
		return 0
	}
	xbar := swx / sw
	ybar := swy / sw
	var num, den float64
	for i := range xs {
		dx := xs[i] - xbar
		num += ws[i] * dx * (ys[i] - ybar)
		den += ws[i] * dx * dx
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// FormatScores renders one row per (orchestrator, column) so the
// self-reported-vs-SE head-to-head is visible in stdout. Accepts the
// per-column slices from Score; nil/empty slices are skipped.
func FormatScores(columns ...[]CalibrationScore) string {
	var rows []CalibrationScore
	for _, c := range columns {
		rows = append(rows, c...)
	}
	if len(rows) == 0 {
		return "Calibration scores: no confidence values reported (skip)\n"
	}
	var b strings.Builder
	b.WriteString("Calibration scores (lower ECE/Brier = better; steeper slope = more informative)\n")
	fmt.Fprintf(&b, "    %-26s %-15s %5s  %7s  %7s  %7s\n",
		"orchestrator", "column", "n", "ECE", "Brier", "slope")
	for _, r := range rows {
		fmt.Fprintf(&b, "    %-26s %-15s %5d  %7.4f  %7.4f  %+7.3f\n",
			r.OrchestratorName, r.Column, r.N, r.ECE, r.Brier, r.ReliabilitySlope)
	}
	return b.String()
}
