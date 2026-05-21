// CLAUDE GENERATED
package verifier

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestDefaultConfig_v0Locks(t *testing.T) {
	c := DefaultConfig()
	if c.BootstrapThreshold != 10_000 {
		t.Errorf("BootstrapThreshold = %d, want 10_000", c.BootstrapThreshold)
	}
	if math.Abs(c.Floor-0.15) > 1e-9 {
		t.Errorf("Floor = %v, want 0.15", c.Floor)
	}
	if c.SlidingWindow != 2_000 {
		t.Errorf("SlidingWindow = %d, want 2_000", c.SlidingWindow)
	}
	if math.Abs(c.ConfidenceLevel-0.95) > 1e-9 {
		t.Errorf("ConfidenceLevel = %v, want 0.95", c.ConfidenceLevel)
	}
	if c.HappinessScope != ScopeGlobal {
		t.Errorf("HappinessScope = %q, want global", c.HappinessScope)
	}
}

func TestNew_PassesConfigVerbatim(t *testing.T) {
	// Callers wanting v0 defaults pass DefaultConfig() — New does no
	// auto-fill so tests can set BootstrapThreshold=0 to bypass the
	// bootstrap phase.
	p := New(DefaultConfig())
	if p.cfg.BootstrapThreshold != 10_000 || p.cfg.Floor != 0.15 {
		t.Errorf("DefaultConfig did not flow through: %+v", p.cfg)
	}
	p2 := New(Config{}) // explicit zero — bootstrap=0, no auto-fill
	if p2.cfg.BootstrapThreshold != 0 {
		t.Errorf("explicit zero BootstrapThreshold should not be filled: got %d", p2.cfg.BootstrapThreshold)
	}
}

func TestShouldCritique_BootstrapAlways(t *testing.T) {
	p := New(Config{BootstrapThreshold: 5, HappinessScope: ScopeGlobal})
	// 95% high happiness in the window — would normally drive rate
	// below 1.0 — but bootstrap should override.
	for i := 0; i < 100; i++ {
		p.RecordJudgeAgreement(session.PlaybookProcess, true)
	}
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 5; i++ {
		if !p.ShouldCritique(session.PlaybookProcess, false, r) {
			t.Errorf("bootstrap should force critique on invocation %d", i)
		}
	}
}

func TestShouldCritique_ParentOverrideAlwaysWinsPostBootstrap(t *testing.T) {
	p := New(Config{
		BootstrapThreshold: 0,
		Floor:              0.15,
		SlidingWindow:      100,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopeGlobal,
	})
	// Fill window with full agreement so steady-state rate drops to floor.
	for i := 0; i < 100; i++ {
		p.RecordJudgeAgreement(session.PlaybookProcess, true)
	}
	r := rand.New(rand.NewPCG(1, 2))
	// Without override, sample rate is near floor (0.15) — most rolls miss.
	hits := 0
	trials := 200
	for i := 0; i < trials; i++ {
		if p.ShouldCritique(session.PlaybookProcess, false, r) {
			hits++
		}
	}
	if hits > trials/2 {
		t.Errorf("expected sampling at floor (~%v); got %d/%d hits", 0.15, hits, trials)
	}
	// With parent override, always true.
	for i := 0; i < 50; i++ {
		if !p.ShouldCritique(session.PlaybookProcess, true, r) {
			t.Errorf("parent override should force critique")
		}
	}
}

func TestSampleRate_FloorHonored(t *testing.T) {
	p := New(Config{
		BootstrapThreshold: 0,
		Floor:              0.15,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopeGlobal,
	})
	// Saturate happiness — Wilson lower bound approaches 1, so 1-LB
	// would drop below the floor without clamping.
	for i := 0; i < 500; i++ {
		p.RecordJudgeAgreement(session.PlaybookProcess, true)
	}
	rate := p.SampleRate(session.PlaybookProcess)
	if rate < 0.149 || rate > 0.151 {
		t.Errorf("rate = %v, want clamped to floor 0.15", rate)
	}
}

func TestSampleRate_LowHappinessKeepsRateHigh(t *testing.T) {
	p := New(Config{
		BootstrapThreshold: 0,
		Floor:              0.15,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopeGlobal,
	})
	// 50% happiness. Wilson lower bound on 250/500 at 95% is ~0.457.
	// Rate = 1 - 0.457 = ~0.543.
	for i := 0; i < 500; i++ {
		p.RecordJudgeAgreement(session.PlaybookProcess, i%2 == 0)
	}
	rate := p.SampleRate(session.PlaybookProcess)
	if rate < 0.5 || rate > 0.6 {
		t.Errorf("rate = %v, want ~0.54 for 50%% happiness over 500 samples", rate)
	}
}

func TestSampleRate_EmptyWindowFallsBackToOne(t *testing.T) {
	// Post-bootstrap with no judge data yet → still cautious (1.0).
	p := New(Config{
		BootstrapThreshold: 0,
		Floor:              0.15,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopeGlobal,
	})
	if r := p.SampleRate(session.PlaybookProcess); r != 1.0 {
		t.Errorf("empty window rate = %v, want 1.0", r)
	}
}

func TestPerActionScope_IsolatesByPlaybook(t *testing.T) {
	p := New(Config{
		BootstrapThreshold: 0,
		Floor:              0.15,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopePerAction,
	})
	// Saturate process happiness; compose has nothing.
	for i := 0; i < 500; i++ {
		p.RecordJudgeAgreement(session.PlaybookProcess, true)
	}
	pr := p.SampleRate(session.PlaybookProcess)
	cr := p.SampleRate(session.PlaybookCompose)
	if pr > 0.2 {
		t.Errorf("process rate = %v, want at floor", pr)
	}
	if cr != 1.0 {
		t.Errorf("compose rate = %v, want 1.0 (no data)", cr)
	}
}

func TestTelemetry_TracksBothScopesRegardlessOfActive(t *testing.T) {
	p := New(Config{
		BootstrapThreshold: 0,
		Floor:              0.15,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		HappinessScope:     ScopeGlobal, // global drives rate
	})
	for i := 0; i < 100; i++ {
		p.RecordJudgeAgreement(session.PlaybookProcess, true)
	}
	for i := 0; i < 100; i++ {
		p.RecordJudgeAgreement(session.PlaybookCompose, false)
	}
	tel := p.Telemetry()
	if tel.Scope != ScopeGlobal {
		t.Errorf("Scope = %q, want global", tel.Scope)
	}
	// Global counts both streams = 200 samples, 100 successes → ~50%.
	if tel.Global.WindowCount != 200 {
		t.Errorf("global window count = %d, want 200", tel.Global.WindowCount)
	}
	if pa, ok := tel.PerAction[session.PlaybookProcess]; !ok || pa.WindowCount != 100 {
		t.Errorf("per-action process telemetry missing or wrong: %+v", pa)
	}
	if pa, ok := tel.PerAction[session.PlaybookCompose]; !ok || pa.WindowCount != 100 {
		t.Errorf("per-action compose telemetry missing or wrong: %+v", pa)
	}
}

func TestShouldCritique_IncrementsInvocations(t *testing.T) {
	p := New(Config{
		BootstrapThreshold: 3,
		HappinessScope:     ScopeGlobal,
	})
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 10; i++ {
		p.ShouldCritique(session.PlaybookProcess, false, r)
	}
	tel := p.Telemetry()
	if tel.Global.Invocations != 10 {
		t.Errorf("Global.Invocations = %d, want 10", tel.Global.Invocations)
	}
	if tel.PerAction[session.PlaybookProcess].Invocations != 10 {
		t.Errorf("PerAction[process].Invocations = %d, want 10",
			tel.PerAction[session.PlaybookProcess].Invocations)
	}
}

// --- Wilson math direct tests ---

func TestWilsonLowerBound_KnownInputs(t *testing.T) {
	cases := []struct {
		name string
		s, n int
		conf float64
		want float64 // tolerance is 0.005
	}{
		{"0/0", 0, 0, 0.95, 0.0},
		{"1/1@95", 1, 1, 0.95, 0.207},  // Wilson LB ≈ 0.207
		{"50/100@95", 50, 100, 0.95, 0.402},
		{"99/100@95", 99, 100, 0.95, 0.946},
		{"100/100@95", 100, 100, 0.95, 0.964},
		{"0/100@95", 0, 100, 0.95, 0.0},
		{"500/500@95", 500, 500, 0.95, 0.9924},
	}
	for _, tc := range cases {
		got := wilsonLowerBound(tc.s, tc.n, tc.conf)
		if math.Abs(got-tc.want) > 0.005 {
			t.Errorf("%s: got %v, want ~%v", tc.name, got, tc.want)
		}
	}
}

func TestNormalQuantile_KnownValues(t *testing.T) {
	cases := []struct {
		p    float64
		want float64 // tol 0.001
	}{
		{0.5, 0.0},
		{0.975, 1.959964},  // z_{.025}
		{0.025, -1.959964},
		{0.995, 2.575829},  // z_{.005}
	}
	for _, tc := range cases {
		got := normalQuantile(tc.p)
		if math.Abs(got-tc.want) > 0.001 {
			t.Errorf("p=%v: got %v, want ~%v", tc.p, got, tc.want)
		}
	}
}

// --- Ring window tests ---

func TestRingWindow_FillThenOverwrite(t *testing.T) {
	w := newRingWindow(3)
	w.add(true)
	w.add(false)
	w.add(true)
	if s, n := w.stats(); s != 2 || n != 3 {
		t.Errorf("after 3 adds: %d/%d, want 2/3", s, n)
	}
	// Overflow — oldest (true) evicted, new (false) added.
	w.add(false)
	if s, n := w.stats(); s != 1 || n != 3 {
		t.Errorf("after overflow: %d/%d, want 1/3", s, n)
	}
	// Overflow again — evict the older false, add a true.
	w.add(true)
	if s, n := w.stats(); s != 2 || n != 3 {
		t.Errorf("after 2nd overflow: %d/%d, want 2/3", s, n)
	}
}

func TestRingWindow_PartialFillCount(t *testing.T) {
	w := newRingWindow(5)
	w.add(true)
	w.add(true)
	if s, n := w.stats(); s != 2 || n != 2 {
		t.Errorf("partial fill: %d/%d, want 2/2", s, n)
	}
}
