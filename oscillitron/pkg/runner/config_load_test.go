// CLAUDE GENERATED
package runner

import (
	"context"
	"flag"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

func clearEnv(t *testing.T) {
	for _, k := range []string{EnvBufferSize, EnvChainTimeout} {
		t.Setenv(k, "")
	}
}

func TestDefaultTunables(t *testing.T) {
	tn := DefaultTunables()
	if tn.BufferSize != 8 {
		t.Errorf("BufferSize default = %d, want 8", tn.BufferSize)
	}
	if tn.ChainTimeout != 0 {
		t.Errorf("ChainTimeout default = %v, want 0 (unwrapped)", tn.ChainTimeout)
	}
}

func TestApplyEnv_OverridesDefaults(t *testing.T) {
	t.Setenv(EnvBufferSize, "32")
	t.Setenv(EnvChainTimeout, "2m30s")
	got, err := ApplyEnv(DefaultTunables())
	if err != nil {
		t.Fatal(err)
	}
	if got.BufferSize != 32 {
		t.Errorf("BufferSize = %d, want 32", got.BufferSize)
	}
	if got.ChainTimeout != 150*time.Second {
		t.Errorf("ChainTimeout = %v, want 2m30s", got.ChainTimeout)
	}
}

func TestApplyEnv_BadValues(t *testing.T) {
	t.Setenv(EnvBufferSize, "not-an-int")
	if _, err := ApplyEnv(DefaultTunables()); err == nil {
		t.Error("expected error on non-integer BufferSize")
	}
	t.Setenv(EnvBufferSize, "")
	t.Setenv(EnvChainTimeout, "not-a-duration")
	if _, err := ApplyEnv(DefaultTunables()); err == nil {
		t.Error("expected error on bad ChainTimeout")
	}
}

func TestLoadTunables_FlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvBufferSize, "16")
	t.Setenv(EnvChainTimeout, "10s")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	got, err := LoadTunables(fs, []string{
		"--" + FlagBufferSize, "64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.BufferSize != 64 {
		t.Errorf("BufferSize = %d, want 64 (flag should beat env)", got.BufferSize)
	}
	if got.ChainTimeout != 10*time.Second {
		t.Errorf("ChainTimeout = %v, want 10s (env, no flag passed)", got.ChainTimeout)
	}
}

func TestTunables_ApplyMergesIntoConfig(t *testing.T) {
	cfg := Config{Logger: nil}
	got := Tunables{BufferSize: 99, ChainTimeout: 7 * time.Second}.Apply(cfg)
	if got.BufferSize != 99 || got.ChainTimeout != 7*time.Second {
		t.Errorf("Apply didn't write through: %+v", got)
	}
}

// TestRun_HonorsChainTimeout proves ChainTimeout actually wraps the
// runner's ctx. Build a topology that would loop forever (router cycle),
// then verify the run terminates within the configured window.
func TestRun_HonorsChainTimeout(t *testing.T) {
	clearEnv(t)
	topo := topology.New("a")
	if err := topo.AddEdge("a", topology.Edge{To: "b", Weight: 1.0}); err != nil {
		t.Fatal(err)
	}
	if err := topo.AddEdge("b", topology.Edge{To: "a", Weight: 1.0}); err != nil {
		t.Fatal(err)
	}

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"a": oscillator.New("a", stub.New("a", stub.ModeBudgetExhausted), nil),
		"b": oscillator.New("b", stub.New("b", stub.ModeBudgetExhausted), nil),
	}
	cfg := Config{
		Topology:     topo,
		Oscillators:  osc,
		Router:       rule.New(),
		Inhibitor:    hardcap.New(1_000_000), // effectively unbounded
		Initial:      session.Envelope{ID: "init", Objective: "loop"},
		ChainTimeout: 100 * time.Millisecond,
	}

	start := time.Now()
	res, err := Run(context.Background(), cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Run took %v despite ChainTimeout=100ms — timeout didn't fire", elapsed)
	}
	if res.Reason != ReasonContextDone {
		t.Errorf("Reason = %q, want %q (ctx deadline should fire)", res.Reason, ReasonContextDone)
	}
}
