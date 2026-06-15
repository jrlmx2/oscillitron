package calibration

import (
	"math"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// reportWith builds a one-orchestrator Report from (conf, se, pass)
// triples. conf → Answer.Confidence, se → Answer.SEConfidence.
func reportWith(name string, rows []struct {
	conf, se float64
	pass     bool
}) benchmark.Report {
	r := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: name}},
	}
	for i, row := range rows {
		r.Cases = append(r.Cases, benchmark.CaseResult{
			CaseID: string(rune('a' + i)),
			Results: []benchmark.OrchestratorResult{{
				OrchestratorName: name,
				Answer:           benchmark.Answer{Confidence: row.conf, SEConfidence: row.se},
				Verdict:          benchmark.Verdict{Pass: row.pass},
			}},
		})
	}
	return r
}

func TestScore_ECE_Brier_Slope(t *testing.T) {
	const eps = 1e-9
	// Two high-conf (0.9) cases split pass/fail, two low-conf (0.3)
	// split pass/fail. Flat reliability (both bands 50% pass).
	rep := reportWith("vote-3", []struct {
		conf, se float64
		pass     bool
	}{
		{conf: 0.9, pass: true},
		{conf: 0.9, pass: false},
		{conf: 0.3, pass: false},
		{conf: 0.3, pass: true},
	})

	scores := Score(rep, SelfReported, "confidence", nil)
	if len(scores) != 1 {
		t.Fatalf("Score returned %d rows, want 1", len(scores))
	}
	s := scores[0]
	if s.OrchestratorName != "vote-3" || s.Column != "confidence" {
		t.Errorf("got (%s,%s), want (vote-3,confidence)", s.OrchestratorName, s.Column)
	}
	if s.N != 4 {
		t.Errorf("N = %d, want 4", s.N)
	}
	// Brier = mean[(0.9-1)²,(0.9-0)²,(0.3-0)²,(0.3-1)²]
	//       = (0.01+0.81+0.09+0.49)/4 = 0.35
	if math.Abs(s.Brier-0.35) > eps {
		t.Errorf("Brier = %v, want 0.35", s.Brier)
	}
	// ECE: low band (n=2, acc .5, conf .3 → |.2|·.5=.1) +
	//      high band (n=2, acc .5, conf .9 → |.4|·.5=.2) = 0.3
	if math.Abs(s.ECE-0.3) > eps {
		t.Errorf("ECE = %v, want 0.3", s.ECE)
	}
	// Flat reliability (both bands 50% pass) → slope ≈ 0.
	if math.Abs(s.ReliabilitySlope) > eps {
		t.Errorf("ReliabilitySlope = %v, want ~0 (flat)", s.ReliabilitySlope)
	}
}

func TestScore_MonotoneSlopePositive(t *testing.T) {
	// Low band passes rarely, high band passes often → positive slope.
	rep := reportWith("vote-3", []struct {
		conf, se float64
		pass     bool
	}{
		{conf: 0.3, pass: false},
		{conf: 0.3, pass: false},
		{conf: 0.3, pass: true}, // low band: 1/3 pass, mean 0.3
		{conf: 0.9, pass: true},
		{conf: 0.9, pass: true},
		{conf: 0.9, pass: false}, // high band: 2/3 pass, mean 0.9
	})
	s := Score(rep, SelfReported, "confidence", nil)[0]
	if s.ReliabilitySlope <= 0 {
		t.Errorf("ReliabilitySlope = %v, want > 0 (monotone-up)", s.ReliabilitySlope)
	}
}

func TestScore_SelectorReadsSEColumn(t *testing.T) {
	// Confidence and SEConfidence diverge: the selector must read the
	// column it was given. Here SEConfidence is unanimous-high (1.0)
	// while Confidence is mid (0.5), so SE Brier differs from self.
	rep := reportWith("vote-3", []struct {
		conf, se float64
		pass     bool
	}{
		{conf: 0.5, se: 1.0, pass: true},
		{conf: 0.5, se: 1.0, pass: true},
	})
	self := Score(rep, SelfReported, "confidence", nil)[0]
	se := Score(rep, SemanticEntropy, "se_confidence", nil)[0]

	if se.Column != "se_confidence" {
		t.Errorf("SE column label = %q, want se_confidence", se.Column)
	}
	// Self: (0.5-1)²=0.25 → Brier 0.25. SE: (1-1)²=0 → Brier 0.
	if math.Abs(self.Brier-0.25) > 1e-9 {
		t.Errorf("self Brier = %v, want 0.25", self.Brier)
	}
	if math.Abs(se.Brier) > 1e-9 {
		t.Errorf("se Brier = %v, want 0 (SE column read)", se.Brier)
	}
}

func TestScore_ExcludesZeroAndError(t *testing.T) {
	rep := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "vote-3"}},
		Cases: []benchmark.CaseResult{
			{Results: []benchmark.OrchestratorResult{
				{OrchestratorName: "vote-3", Answer: benchmark.Answer{Confidence: 0.9}, Verdict: benchmark.Verdict{Pass: true}},
				{OrchestratorName: "vote-3", Answer: benchmark.Answer{Confidence: 0.0}, Verdict: benchmark.Verdict{Pass: true}}, // excluded (conf 0)
			}},
		},
	}
	s := Score(rep, SelfReported, "confidence", nil)[0]
	if s.N != 1 {
		t.Errorf("N = %d, want 1 (zero-confidence excluded)", s.N)
	}
}

func TestFormatScores_RendersBothColumns(t *testing.T) {
	rep := reportWith("vote-3", []struct {
		conf, se float64
		pass     bool
	}{
		{conf: 0.9, se: 0.8, pass: true},
		{conf: 0.3, se: 0.2, pass: false},
	})
	out := FormatScores(
		Score(rep, SelfReported, "confidence", nil),
		Score(rep, SemanticEntropy, "se_confidence", nil),
	)
	for _, want := range []string{"vote-3", "confidence", "se_confidence", "ECE", "Brier"} {
		if !contains(out, want) {
			t.Errorf("FormatScores output missing %q:\n%s", want, out)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
