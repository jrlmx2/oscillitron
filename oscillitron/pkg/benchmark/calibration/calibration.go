// CLAUDE GENERATED
// Package calibration computes the v3.3 "confidence vs. pass rate"
// table from a benchmark.Report. The thesis being tested:
// **calibrated confidence means higher-confidence answers pass more
// often than lower-confidence ones**. If the table is flat (every
// band has the same pass rate), the confidence number is noise; if
// it slopes up with confidence, confidence is a useful signal.
//
// The act layer (v3.4) reads calibrated confidence to drive coping
// decisions; this package is the measurement that tells us whether
// that's worth doing.
//
// No-op (returns empty rows) when no orchestrator produced any
// answer with Confidence > 0 — pre-v3.3 reports remain readable.
package calibration

import (
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// Band defines one confidence bucket. Cases with Confidence in
// [Lo, Hi) land here; the topmost band uses [Lo, Hi].
type Band struct {
	Lo, Hi float64
	Label  string
}

// DefaultBands are the v3.3 starting buckets. Picked to roughly
// match the v3.4 coping rule table thresholds:
//   - 0.85–1.0 = "confident, ship as-is" regime
//   - 0.5–0.85 = "qualify the answer, maybe re-run" regime
//   - 0.0–0.5  = "low confidence, defer or escalate" regime
//
// Operators tune by passing custom bands to Compute().
var DefaultBands = []Band{
	{Lo: 0.0, Hi: 0.5, Label: "low (<0.50)"},
	{Lo: 0.5, Hi: 0.85, Label: "medium (0.50-0.85)"},
	{Lo: 0.85, Hi: 1.01, Label: "high (>=0.85)"}, // hi=1.01 to include exactly 1.0
}

// Row is one (orchestrator, band) cell of the calibration table.
type Row struct {
	OrchestratorName string
	Band             Band
	// Count is the number of cases that landed in this band.
	Count int
	// Passes is how many of those cases passed the grader.
	Passes int
	// MeanConfidence is the average Confidence reported across the
	// cases in this band. Useful as a sanity check (low band should
	// have low mean; high band should have high mean).
	MeanConfidence float64
}

// PassRate returns Passes/Count, or 0 if Count == 0.
func (r Row) PassRate() float64 {
	if r.Count == 0 {
		return 0
	}
	return float64(r.Passes) / float64(r.Count)
}

// Summary is the full calibration breakdown across a Report.
type Summary struct {
	// Rows is per (orchestrator, band). Ordered by orchestrator
	// then by band (low→high).
	Rows []Row
	// Empty is true when no orchestrator produced any Confidence > 0.
	// Consumers can skip rendering when this fires.
	Empty bool
}

// Compute aggregates the report into a calibration Summary. Pass
// nil bands to use DefaultBands.
func Compute(report benchmark.Report, bands []Band) Summary {
	if bands == nil {
		bands = DefaultBands
	}
	if len(report.Aggregates) == 0 {
		return Summary{Empty: true}
	}

	// Per-orchestrator, per-band accumulator.
	type cell struct {
		count, passes int
		confidenceSum float64
	}
	cells := make(map[string][]cell, len(report.Aggregates))
	for _, a := range report.Aggregates {
		cells[a.OrchestratorName] = make([]cell, len(bands))
	}

	anyConfidence := false
	for _, cr := range report.Cases {
		for _, or := range cr.Results {
			if or.Err != nil {
				continue
			}
			if or.Answer.Confidence <= 0 {
				continue
			}
			anyConfidence = true
			b := pickBand(or.Answer.Confidence, bands)
			if b < 0 {
				continue
			}
			cells[or.OrchestratorName][b].count++
			cells[or.OrchestratorName][b].confidenceSum += or.Answer.Confidence
			if or.Verdict.Pass {
				cells[or.OrchestratorName][b].passes++
			}
		}
	}

	if !anyConfidence {
		return Summary{Empty: true}
	}

	// Emit rows in stable order: orchestrator-listed-in-Aggregates,
	// then band-low-to-high.
	var rows []Row
	for _, a := range report.Aggregates {
		bs := cells[a.OrchestratorName]
		for i, band := range bands {
			c := bs[i]
			mean := 0.0
			if c.count > 0 {
				mean = c.confidenceSum / float64(c.count)
			}
			rows = append(rows, Row{
				OrchestratorName: a.OrchestratorName,
				Band:             band,
				Count:            c.count,
				Passes:           c.passes,
				MeanConfidence:   mean,
			})
		}
	}
	return Summary{Rows: rows}
}

// pickBand returns the index of the band containing conf, or -1
// when conf doesn't fall in any band.
func pickBand(conf float64, bands []Band) int {
	for i, b := range bands {
		if conf >= b.Lo && conf < b.Hi {
			return i
		}
	}
	return -1
}

// FormatTable renders a Summary as a stdout-friendly table.
// One section per orchestrator with band rows.
//
// Example output:
//
//	Confidence calibration (per orchestrator)
//	  frontier-phi4-mini:latest
//	    band                       n   pass  rate   mean_conf
//	    low (<0.50)               12      2  16.7%      0.34
//	    medium (0.50-0.85)        20      9  45.0%      0.71
//	    high (>=0.85)             40     30  75.0%      0.92
func FormatTable(s Summary) string {
	if s.Empty {
		return "Confidence calibration: no confidence values reported (skip)\n"
	}
	var b strings.Builder
	b.WriteString("Confidence calibration (per orchestrator)\n")

	// Group by orchestrator for printing. Rows already arrive in
	// report order (Compute walks Aggregates), so first-seen ordering
	// preserves it.
	byOrch := map[string][]Row{}
	var orchOrder []string
	for _, r := range s.Rows {
		if _, ok := byOrch[r.OrchestratorName]; !ok {
			orchOrder = append(orchOrder, r.OrchestratorName)
		}
		byOrch[r.OrchestratorName] = append(byOrch[r.OrchestratorName], r)
	}

	for _, name := range orchOrder {
		fmt.Fprintf(&b, "  %s\n", name)
		fmt.Fprintf(&b, "    %-26s %5s  %5s  %6s  %s\n", "band", "n", "pass", "rate", "mean_conf")
		for _, r := range byOrch[name] {
			rate := r.PassRate() * 100
			fmt.Fprintf(&b, "    %-26s %5d  %5d  %5.1f%%  %.2f\n",
				r.Band.Label, r.Count, r.Passes, rate, r.MeanConfidence)
		}
	}
	return b.String()
}
