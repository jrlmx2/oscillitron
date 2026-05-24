package calibration

import (
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

func mkCase(id string) benchmark.Case {
	return benchmark.Case{ID: id, Expected: "A"}
}

func mkResult(orch string, conf float64, pass bool) benchmark.OrchestratorResult {
	return benchmark.OrchestratorResult{
		OrchestratorName: orch,
		Answer:           benchmark.Answer{Confidence: conf},
		Verdict:          benchmark.Verdict{Pass: pass},
	}
}

func TestCompute_Empty_NoConfidence(t *testing.T) {
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "x"}},
		Cases: []benchmark.CaseResult{
			{Case: mkCase("c1"), Results: []benchmark.OrchestratorResult{mkResult("x", 0, true)}},
		},
	}
	s := Compute(report, nil)
	if !s.Empty {
		t.Errorf("expected Empty=true when no confidence reported")
	}
}

func TestCompute_BandAssignment(t *testing.T) {
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "x"}},
		Cases: []benchmark.CaseResult{
			{Case: mkCase("c1"), Results: []benchmark.OrchestratorResult{mkResult("x", 0.3, false)}}, // low
			{Case: mkCase("c2"), Results: []benchmark.OrchestratorResult{mkResult("x", 0.6, true)}},  // medium
			{Case: mkCase("c3"), Results: []benchmark.OrchestratorResult{mkResult("x", 0.95, true)}}, // high
			{Case: mkCase("c4"), Results: []benchmark.OrchestratorResult{mkResult("x", 1.0, true)}},  // high (inclusive)
		},
	}
	s := Compute(report, nil)
	if s.Empty {
		t.Fatalf("expected non-empty summary")
	}
	if len(s.Rows) != 3 {
		t.Fatalf("expected 3 rows (3 bands); got %d", len(s.Rows))
	}
	if s.Rows[0].Count != 1 || s.Rows[0].Passes != 0 {
		t.Errorf("low band wrong: count=%d, passes=%d", s.Rows[0].Count, s.Rows[0].Passes)
	}
	if s.Rows[1].Count != 1 || s.Rows[1].Passes != 1 {
		t.Errorf("medium band wrong: count=%d, passes=%d", s.Rows[1].Count, s.Rows[1].Passes)
	}
	if s.Rows[2].Count != 2 || s.Rows[2].Passes != 2 {
		t.Errorf("high band wrong: count=%d, passes=%d (should include conf=1.0)", s.Rows[2].Count, s.Rows[2].Passes)
	}
}

func TestCompute_CalibrationSlopeFromIdealRun(t *testing.T) {
	// Construct an "ideally calibrated" run: low-band cases mostly
	// fail, high-band cases mostly pass. Verify pass rates slope up
	// with confidence.
	cases := []benchmark.CaseResult{}
	add := func(conf float64, pass bool) {
		cases = append(cases, benchmark.CaseResult{
			Case:    mkCase("c"),
			Results: []benchmark.OrchestratorResult{mkResult("x", conf, pass)},
		})
	}
	for i := 0; i < 10; i++ {
		add(0.3, i < 2) // low: 20% pass
	}
	for i := 0; i < 10; i++ {
		add(0.7, i < 5) // medium: 50% pass
	}
	for i := 0; i < 10; i++ {
		add(0.9, i < 9) // high: 90% pass
	}
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "x"}},
		Cases:      cases,
	}
	s := Compute(report, nil)
	if s.Empty {
		t.Fatal("expected non-empty")
	}
	if s.Rows[0].PassRate() != 0.2 {
		t.Errorf("low pass rate = %v, want 0.2", s.Rows[0].PassRate())
	}
	if s.Rows[1].PassRate() != 0.5 {
		t.Errorf("medium pass rate = %v, want 0.5", s.Rows[1].PassRate())
	}
	if s.Rows[2].PassRate() != 0.9 {
		t.Errorf("high pass rate = %v, want 0.9", s.Rows[2].PassRate())
	}
	// Slope check: rates strictly increasing.
	if !(s.Rows[0].PassRate() < s.Rows[1].PassRate() && s.Rows[1].PassRate() < s.Rows[2].PassRate()) {
		t.Errorf("expected pass rates to slope up with confidence; got %v / %v / %v",
			s.Rows[0].PassRate(), s.Rows[1].PassRate(), s.Rows[2].PassRate())
	}
}

func TestCompute_SkipsErroredAttempts(t *testing.T) {
	// Errored results shouldn't count toward any band, even if they
	// happen to have a Confidence set on the Answer.
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "x"}},
		Cases: []benchmark.CaseResult{
			{Case: mkCase("c1"), Results: []benchmark.OrchestratorResult{
				{OrchestratorName: "x", Err: errString("boom"), Answer: benchmark.Answer{Confidence: 0.9}},
			}},
			{Case: mkCase("c2"), Results: []benchmark.OrchestratorResult{mkResult("x", 0.9, true)}},
		},
	}
	s := Compute(report, nil)
	if s.Rows[2].Count != 1 {
		t.Errorf("errored result with confidence should not count; got count=%d", s.Rows[2].Count)
	}
}

func TestCompute_MultipleOrchestrators_StableOrder(t *testing.T) {
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{
			{OrchestratorName: "first"},
			{OrchestratorName: "second"},
		},
		Cases: []benchmark.CaseResult{
			{Case: mkCase("c1"), Results: []benchmark.OrchestratorResult{
				mkResult("first", 0.9, true),
				mkResult("second", 0.3, false),
			}},
		},
	}
	s := Compute(report, nil)
	if len(s.Rows) != 6 {
		t.Fatalf("expected 6 rows (2 orchestrators × 3 bands); got %d", len(s.Rows))
	}
	// First three rows = "first" orchestrator, in band order.
	for i := 0; i < 3; i++ {
		if s.Rows[i].OrchestratorName != "first" {
			t.Errorf("row %d: expected 'first', got %q", i, s.Rows[i].OrchestratorName)
		}
	}
	for i := 3; i < 6; i++ {
		if s.Rows[i].OrchestratorName != "second" {
			t.Errorf("row %d: expected 'second', got %q", i, s.Rows[i].OrchestratorName)
		}
	}
}

func TestFormatTable_EmptySummary(t *testing.T) {
	s := Compute(benchmark.Report{}, nil)
	out := FormatTable(s)
	if !strings.Contains(out, "no confidence") {
		t.Errorf("expected empty-message output; got %q", out)
	}
}

func TestFormatTable_RealOutput(t *testing.T) {
	report := benchmark.Report{
		Aggregates: []benchmark.AggregateStats{{OrchestratorName: "vote-5"}},
		Cases: []benchmark.CaseResult{
			{Case: mkCase("c1"), Results: []benchmark.OrchestratorResult{mkResult("vote-5", 0.9, true)}},
			{Case: mkCase("c2"), Results: []benchmark.OrchestratorResult{mkResult("vote-5", 0.4, false)}},
		},
	}
	out := FormatTable(Compute(report, nil))
	for _, want := range []string{"vote-5", "band", "pass", "rate", "mean_conf", "low", "high"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

// errString is a tiny error type to avoid importing errors in tests
// just for this.
type errString string

func (e errString) Error() string { return string(e) }
