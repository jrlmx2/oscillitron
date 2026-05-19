// CLAUDE GENERATED
package pool

import (
	"context"
	"flag"
	"sync"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestPool_ParallelDefaultIsOn(t *testing.T) {
	p, _ := New("p", nil, stub.New("a", stub.ModeDone))
	if !p.Parallel() {
		t.Errorf("Parallel() default = false, want true (ParallelDefault=%v)", ParallelDefault)
	}
}

func TestPool_DisablingParallelPinsToBackendZero(t *testing.T) {
	a := stub.New("a", stub.ModeDone)
	b := stub.New("b", stub.ModeDone)
	c := stub.New("c", stub.ModeDone)
	p, _ := New("p", nil, a, b, c)
	p.SetParallel(false)

	const n = 30
	for i := 0; i < n; i++ {
		if _, err := p.Call(context.Background(), session.Envelope{Objective: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if a.Calls() != n {
		t.Errorf("backend a (index 0) got %d calls, want %d", a.Calls(), n)
	}
	if b.Calls() != 0 || c.Calls() != 0 {
		t.Errorf("backends b and c should be dormant when parallel=false: b=%d c=%d", b.Calls(), c.Calls())
	}
}

func TestPool_ToggleParallelMidstream(t *testing.T) {
	a := stub.New("a", stub.ModeDone)
	b := stub.New("b", stub.ModeDone)
	p, _ := New("p", nil, a, b)

	// First 10 with parallel ON → roughly even split.
	for i := 0; i < 10; i++ {
		_, _ = p.Call(context.Background(), session.Envelope{})
	}
	if a.Calls() == 0 || b.Calls() == 0 {
		t.Errorf("expected even split with parallel=true: a=%d b=%d", a.Calls(), b.Calls())
	}
	aBefore, bBefore := a.Calls(), b.Calls()

	// Disable parallel → next 10 all to backend a.
	p.SetParallel(false)
	for i := 0; i < 10; i++ {
		_, _ = p.Call(context.Background(), session.Envelope{})
	}
	if a.Calls() != aBefore+10 {
		t.Errorf("after disabling parallel, a should get +10: got %d, was %d", a.Calls(), aBefore)
	}
	if b.Calls() != bBefore {
		t.Errorf("after disabling parallel, b should be unchanged: got %d, was %d", b.Calls(), bBefore)
	}
}

func TestPool_DisabledParallel_GoroutineSafe(t *testing.T) {
	// Run -race scenarios: concurrent toggling + Calls. Should never
	// blow up; outcomes correct for whichever mode was active per call.
	a := stub.New("a", stub.ModeDone)
	b := stub.New("b", stub.ModeDone)
	p, _ := New("p", nil, a, b)

	const workers = 8
	const perWork = 100

	var wg sync.WaitGroup
	wg.Add(workers + 1)
	// Toggler
	go func() {
		defer wg.Done()
		for i := 0; i < perWork; i++ {
			p.SetParallel(i%2 == 0)
		}
	}()
	// Callers
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perWork; i++ {
				if _, err := p.Call(context.Background(), session.Envelope{}); err != nil {
					t.Errorf("Call: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	// Total calls accounted for.
	if a.Calls()+b.Calls() != int64(workers*perWork) {
		t.Errorf("lost dispatch: a+b = %d, want %d", a.Calls()+b.Calls(), workers*perWork)
	}
	// a always gets dispatched-to (either via strategy or via parallel-off pin);
	// b only gets calls when parallel was on at decision time.
	if a.Calls() == 0 {
		t.Errorf("backend a should always receive some calls")
	}
}

// ---- config_load.go tests ----

func clearEnv(t *testing.T) {
	t.Setenv(EnvParallel, "")
}

func TestDefaultPoolConfig(t *testing.T) {
	if !DefaultPoolConfig().Parallel {
		t.Errorf("default Parallel = false, want true")
	}
}

func TestApplyEnv_TruthyAndFalsy(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"true", true}, {"false", false},
		{"1", true}, {"0", false},
		{"yes", true}, {"no", false},
		{"on", true}, {"off", false},
		{"TRUE", true}, {"False", false},
	}
	for _, c := range cases {
		t.Setenv(EnvParallel, c.in)
		got, err := ApplyEnv(DefaultPoolConfig())
		if err != nil {
			t.Errorf("ApplyEnv(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Parallel != c.want {
			t.Errorf("ApplyEnv(%q).Parallel = %v, want %v", c.in, got.Parallel, c.want)
		}
	}
}

func TestApplyEnv_BadValue(t *testing.T) {
	t.Setenv(EnvParallel, "definitely-not-a-bool")
	if _, err := ApplyEnv(DefaultPoolConfig()); err == nil {
		t.Error("expected error on garbage value")
	}
}

func TestLoadPoolConfig_FlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvParallel, "true")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	pc, err := LoadPoolConfig(fs, []string{"--" + FlagParallel + "=false"})
	if err != nil {
		t.Fatal(err)
	}
	if pc.Parallel {
		t.Errorf("flag should beat env: got Parallel=true, want false")
	}
}

func TestPoolConfig_Apply(t *testing.T) {
	clearEnv(t)
	p, _ := New("p", nil, stub.New("a", stub.ModeDone))
	PoolConfig{Parallel: false}.Apply(p)
	if p.Parallel() {
		t.Errorf("Apply(false) didn't toggle Adapter")
	}
}

// Compile-time: stub.Adapter is an adapter.Adapter — needed by some
// test plumbing to avoid type inference surprises.
var _ adapter.Adapter = (*stub.Adapter)(nil)
