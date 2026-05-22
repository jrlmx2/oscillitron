// CLAUDE GENERATED
package benchmark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// stubOrchestrator returns a pre-recorded Extracted per case ID. If
// the answer key isn't present, returns an error.
type stubOrchestrator struct {
	name    string
	answers map[string]string // caseID → extracted
	errs    map[string]error
}

func (s *stubOrchestrator) Name() string { return s.name }

func (s *stubOrchestrator) Answer(_ context.Context, c Case) (Answer, error) {
	if err, ok := s.errs[c.ID]; ok {
		return Answer{}, err
	}
	a, ok := s.answers[c.ID]
	if !ok {
		return Answer{}, fmt.Errorf("no answer for %s", c.ID)
	}
	return Answer{Raw: a, Extracted: a, Calls: 1, TokensUsed: 10}, nil
}

// stubGrader passes when Answer.Extracted equals Case.Expected.
type stubGrader struct{}

func (stubGrader) Name() string { return "stub" }
func (stubGrader) Grade(_ context.Context, c Case, a Answer) (Verdict, error) {
	pass := a.Extracted == c.Expected
	score := 0.0
	if pass {
		score = 1.0
	}
	return Verdict{GraderName: "stub", Pass: pass, Score: score, TokensUsed: 1}, nil
}

// stubLoader returns a fixed slice of cases.
type stubLoader struct {
	name  string
	cases []Case
	err   error
}

func (s stubLoader) Name() string { return s.name }
func (s stubLoader) Load(_ context.Context) ([]Case, error) {
	return s.cases, s.err
}

// makeCases builds N cases with Expected = "A".
func makeCases(n int) []Case {
	out := make([]Case, n)
	for i := 0; i < n; i++ {
		out[i] = Case{ID: fmt.Sprintf("c%02d", i), Prompt: "q?", Expected: "A"}
	}
	return out
}

func TestRun_RequiresLoader(t *testing.T) {
	_, err := Run(context.Background(), RunnerConfig{
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o"}},
		Grader:        stubGrader{},
	})
	if err == nil {
		t.Fatal("expected error with nil Loader")
	}
}

func TestRun_RequiresOrchestrator(t *testing.T) {
	_, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "x", cases: makeCases(1)},
		Grader: stubGrader{},
	})
	if err == nil {
		t.Fatal("expected error with no orchestrators")
	}
}

func TestRun_RequiresGrader(t *testing.T) {
	_, err := Run(context.Background(), RunnerConfig{
		Loader:        stubLoader{name: "x", cases: makeCases(1)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o"}},
	})
	if err == nil {
		t.Fatal("expected error with nil Grader")
	}
}

func TestRun_LoaderError_Propagates(t *testing.T) {
	_, err := Run(context.Background(), RunnerConfig{
		Loader:        stubLoader{name: "boom", err: errors.New("disk gone")},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o"}},
		Grader:        stubGrader{},
	})
	if err == nil || !errors.Is(err, err) {
		t.Fatalf("expected loader error to propagate; got %v", err)
	}
}

func TestRun_HappyPath_Aggregates(t *testing.T) {
	cases := makeCases(5)
	// Orchestrator gets 3/5 right (cases 0, 2, 4)
	answers := map[string]string{
		"c00": "A", "c01": "B", "c02": "A", "c03": "C", "c04": "A",
	}
	report, err := Run(context.Background(), RunnerConfig{
		Loader:        stubLoader{name: "test", cases: cases},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "orch", answers: answers}},
		Grader:        stubGrader{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Cases) != 5 {
		t.Errorf("Cases = %d, want 5", len(report.Cases))
	}
	if len(report.Aggregates) != 1 {
		t.Fatalf("Aggregates = %d, want 1", len(report.Aggregates))
	}
	agg := report.Aggregates[0]
	if agg.Successes != 3 || agg.Failures != 2 {
		t.Errorf("Successes=%d Failures=%d, want 3/2", agg.Successes, agg.Failures)
	}
	if agg.PassRate != 0.6 {
		t.Errorf("PassRate = %f, want 0.6", agg.PassRate)
	}
	if agg.TotalCalls != 5 || agg.TotalTokens != 50 || agg.TotalGraderTokens != 5 {
		t.Errorf("call/token totals wrong: %+v", agg)
	}
}

func TestRun_OrchestratorError_RecordsAndContinues(t *testing.T) {
	cases := makeCases(3)
	answers := map[string]string{"c00": "A", "c02": "A"} // c01 missing → error
	report, err := Run(context.Background(), RunnerConfig{
		Loader:        stubLoader{name: "test", cases: cases},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "orch", answers: answers}},
		Grader:        stubGrader{},
	})
	if err != nil {
		t.Fatalf("Run: %v (orchestrator error should be recorded, not propagated)", err)
	}
	if len(report.Cases) != 3 {
		t.Errorf("Cases = %d, want 3", len(report.Cases))
	}
	agg := report.Aggregates[0]
	if agg.Successes != 2 || agg.Errors != 1 {
		t.Errorf("Successes=%d Errors=%d, want 2/1; %+v", agg.Successes, agg.Errors, agg)
	}
	// PassRate is over graded-cases (errors excluded).
	if agg.PassRate != 1.0 {
		t.Errorf("PassRate = %f, want 1.0 (errors excluded from denominator)", agg.PassRate)
	}
}

func TestRun_TwoOrchestrators_CompareDirectly(t *testing.T) {
	cases := makeCases(4)
	o1 := &stubOrchestrator{name: "frontier", answers: map[string]string{
		"c00": "A", "c01": "A", "c02": "A", "c03": "A", // 4/4
	}}
	o2 := &stubOrchestrator{name: "orch-vote", answers: map[string]string{
		"c00": "A", "c01": "B", "c02": "A", "c03": "A", // 3/4
	}}
	report, err := Run(context.Background(), RunnerConfig{
		Loader:        stubLoader{name: "test", cases: cases},
		Orchestrators: []Orchestrator{o1, o2},
		Grader:        stubGrader{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Aggregates) != 2 {
		t.Fatalf("Aggregates = %d, want 2", len(report.Aggregates))
	}
	if report.Aggregates[0].PassRate != 1.0 {
		t.Errorf("frontier PassRate = %f, want 1.0", report.Aggregates[0].PassRate)
	}
	if report.Aggregates[1].PassRate != 0.75 {
		t.Errorf("orch-vote PassRate = %f, want 0.75", report.Aggregates[1].PassRate)
	}
}

func TestRun_SlidingWindow_Disabled_NoWindows(t *testing.T) {
	report, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "test", cases: makeCases(10)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o", answers: map[string]string{
			"c00": "A", "c01": "A", "c02": "A", "c03": "A", "c04": "A",
			"c05": "A", "c06": "A", "c07": "A", "c08": "A", "c09": "A",
		}}},
		Grader:            stubGrader{},
		SlidingWindowSize: 0,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Windows) != 0 {
		t.Errorf("Windows = %d, want 0 (sliding disabled)", len(report.Windows))
	}
}

func TestRun_SlidingWindow_TracksImprovementOverTime(t *testing.T) {
	// 10 cases. First 5 fail (orchestrator says B), last 5 pass.
	// Window size 5 means:
	//   first window closes at case 4 (indices 0-4), pass_rate = 0
	//   case 5 window (indices 1-5), pass_rate = 1/5 = 0.2
	//   case 6 window (indices 2-6), pass_rate = 2/5 = 0.4
	//   case 7 window, 3/5 = 0.6
	//   case 8, 4/5 = 0.8
	//   case 9, 5/5 = 1.0
	// → 6 windows total, pass_rate climbs monotonically. This is the
	// exact shape the bench should report when the orchestrator
	// improves with case count.
	cases := makeCases(10)
	answers := map[string]string{
		"c00": "B", "c01": "B", "c02": "B", "c03": "B", "c04": "B",
		"c05": "A", "c06": "A", "c07": "A", "c08": "A", "c09": "A",
	}
	report, err := Run(context.Background(), RunnerConfig{
		Loader:            stubLoader{name: "test", cases: cases},
		Orchestrators:     []Orchestrator{&stubOrchestrator{name: "improving", answers: answers}},
		Grader:            stubGrader{},
		SlidingWindowSize: 5,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Windows) != 6 {
		t.Fatalf("Windows = %d, want 6 (10 cases - 5 warmup + 1)", len(report.Windows))
	}
	wantPassRates := []float64{0.0, 0.2, 0.4, 0.6, 0.8, 1.0}
	for i, want := range wantPassRates {
		got := report.Windows[i].PerOrchestrator[0].PassRate
		if got != want {
			t.Errorf("window[%d] PassRate = %f, want %f", i, got, want)
		}
		if report.Windows[i].EndCase != i+4 {
			t.Errorf("window[%d] EndCase = %d, want %d", i, report.Windows[i].EndCase, i+4)
		}
		if report.Windows[i].Size != 5 {
			t.Errorf("window[%d] Size = %d, want 5", i, report.Windows[i].Size)
		}
	}
}

func TestRun_SlidingWindow_FewerCasesThanWindow_NoSnapshots(t *testing.T) {
	// 3 cases, window size 10 → no windows close (need 10 cases first).
	report, err := Run(context.Background(), RunnerConfig{
		Loader: stubLoader{name: "test", cases: makeCases(3)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o", answers: map[string]string{
			"c00": "A", "c01": "A", "c02": "A",
		}}},
		Grader:            stubGrader{},
		SlidingWindowSize: 10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Windows) != 0 {
		t.Errorf("Windows = %d, want 0 (no window reached warmup)", len(report.Windows))
	}
	// But the aggregate is still computed.
	if report.Aggregates[0].PassRate != 1.0 {
		t.Errorf("PassRate = %f, want 1.0", report.Aggregates[0].PassRate)
	}
}

// captureTracer collects events + correlation for assertion.
type captureTracer struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	name  string
	attrs map[string]any
}

func (c *captureTracer) Event(ctx context.Context, _ slog.Level, name string, attrs ...slog.Attr) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := make(map[string]any)
	for _, p := range trace.CorrelationFrom(ctx) {
		m[p.Key] = p.Value
	}
	for _, a := range attrs {
		m[a.Key] = a.Value.Any()
	}
	c.events = append(c.events, capturedEvent{name: name, attrs: m})
}

func TestRun_StampsCorrelation_CaseAndOrchestrator(t *testing.T) {
	// Verifies that per-case events emitted from within Run carry
	// case + orchestrator correlation, so downstream observers can
	// stitch every emit back to its originating bench case.
	cases := makeCases(2)
	answers := map[string]string{"c00": "A", "c01": "A"}
	tracer := &captureTracer{}
	_, err := Run(context.Background(), RunnerConfig{
		Loader:        stubLoader{name: "test", cases: cases},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "orch", answers: answers}},
		Grader:        stubGrader{},
		Tracer:        tracer,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Find the case_graded events.
	var graded []capturedEvent
	for _, e := range tracer.events {
		if e.name == "benchmark.case_graded" {
			graded = append(graded, e)
		}
	}
	if len(graded) != 2 {
		t.Fatalf("case_graded count = %d, want 2", len(graded))
	}
	// Each should carry case + orchestrator from correlation.
	for i, e := range graded {
		caseID := cases[i].ID
		if e.attrs["case"] != caseID {
			t.Errorf("graded[%d].case = %v, want %s", i, e.attrs["case"], caseID)
		}
		if e.attrs["orchestrator"] != "orch" {
			t.Errorf("graded[%d].orchestrator = %v, want orch", i, e.attrs["orchestrator"])
		}
	}
}

func TestRun_ContextCancellation_StopsMidRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	report, err := Run(ctx, RunnerConfig{
		Loader:        stubLoader{name: "test", cases: makeCases(5)},
		Orchestrators: []Orchestrator{&stubOrchestrator{name: "o", answers: map[string]string{}}},
		Grader:        stubGrader{},
	})
	if err != nil {
		// Run itself returns nil — the ctx error surfaces as no cases
		// completed.
		t.Fatalf("Run returned err: %v", err)
	}
	if len(report.Cases) != 0 {
		t.Errorf("Cases = %d, want 0 (cancelled before any work)", len(report.Cases))
	}
}
