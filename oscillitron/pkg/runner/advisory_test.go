// CLAUDE GENERATED
package runner

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

// stubWithFloor wraps stub.Adapter and advertises a MinCallTimeout —
// exercising the adapter.MinTimeoutAdvisory type assertion in runner.Run
// without dragging in the real Hermes adapter (which would need a process).
type stubWithFloor struct {
	*stub.Adapter
	floor time.Duration
}

func (s stubWithFloor) MinCallTimeout() time.Duration { return s.floor }

// captureTracer records every Event call so tests can assert on what
// the runner emitted.
type captureTracer struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	name  string
	attrs []slog.Attr
}

func (c *captureTracer) Event(ctx context.Context, name string, attrs ...slog.Attr) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedEvent{name: name, attrs: append([]slog.Attr(nil), attrs...)})
}

func (c *captureTracer) byName(name string) []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedEvent
	for _, e := range c.events {
		if e.name == name {
			out = append(out, e)
		}
	}
	return out
}

func TestRunner_WarnsWhenChainTimeoutBelowAdapterFloor(t *testing.T) {
	floor := 30 * time.Second
	chainT := 5 * time.Second

	topo := topology.New("a")
	osc := map[oscillator.ID]*oscillator.Oscillator{
		"a": oscillator.NewWithTracer("a", stubWithFloor{
			Adapter: stub.New("a", stub.ModeDone),
			floor:   floor,
		}, nil),
	}

	tracer := &captureTracer{}
	cfg := Config{
		Topology:     topo,
		Oscillators:  osc,
		Router:       rule.New(),
		Inhibitor:    hardcap.New(2),
		Initial:      session.Envelope{ID: "init"},
		Tracer:       tracer,
		ChainTimeout: chainT,
	}

	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	warnings := tracer.byName("runner.chain_timeout_below_adapter_floor")
	if len(warnings) != 1 {
		t.Fatalf("expected 1 advisory warning, got %d", len(warnings))
	}
	// Check the warning carries the right attribute values.
	var foundFloor, foundChain bool
	for _, a := range warnings[0].attrs {
		switch a.Key {
		case "adapter_min_call_timeout":
			if a.Value.Duration() == floor {
				foundFloor = true
			}
		case "chain_timeout":
			if a.Value.Duration() == chainT {
				foundChain = true
			}
		case "oscillator":
			if !strings.Contains(a.Value.String(), "a") {
				t.Errorf("oscillator attr = %v, want 'a'", a.Value)
			}
		}
	}
	if !foundFloor || !foundChain {
		t.Errorf("warning missing expected attributes: %+v", warnings[0].attrs)
	}
}

func TestRunner_NoWarningWhenChainTimeoutAboveFloor(t *testing.T) {
	topo := topology.New("a")
	osc := map[oscillator.ID]*oscillator.Oscillator{
		"a": oscillator.NewWithTracer("a", stubWithFloor{
			Adapter: stub.New("a", stub.ModeDone),
			floor:   5 * time.Second,
		}, nil),
	}

	tracer := &captureTracer{}
	cfg := Config{
		Topology:     topo,
		Oscillators:  osc,
		Router:       rule.New(),
		Inhibitor:    hardcap.New(2),
		Initial:      session.Envelope{ID: "init"},
		Tracer:       tracer,
		ChainTimeout: 60 * time.Second, // generous
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if w := tracer.byName("runner.chain_timeout_below_adapter_floor"); len(w) != 0 {
		t.Errorf("expected no warning when ChainTimeout > floor, got %d", len(w))
	}
}

func TestRunner_NoWarningWhenChainTimeoutZero(t *testing.T) {
	// ChainTimeout=0 means "unbounded" — the advisory check should
	// skip entirely, since there's no chain-level timeout to compare.
	topo := topology.New("a")
	osc := map[oscillator.ID]*oscillator.Oscillator{
		"a": oscillator.NewWithTracer("a", stubWithFloor{
			Adapter: stub.New("a", stub.ModeDone),
			floor:   30 * time.Second,
		}, nil),
	}

	tracer := &captureTracer{}
	cfg := Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      rule.New(),
		Inhibitor:   hardcap.New(2),
		Initial:     session.Envelope{ID: "init"},
		Tracer:      tracer,
		// ChainTimeout: 0 (unbounded)
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if w := tracer.byName("runner.chain_timeout_below_adapter_floor"); len(w) != 0 {
		t.Errorf("expected no warning when ChainTimeout=0, got %d", len(w))
	}
}

func TestRunner_NoWarningForAdaptersWithoutAdvisory(t *testing.T) {
	// Plain stub.Adapter doesn't implement MinTimeoutAdvisory; the
	// runner should type-assert and skip without warning.
	topo := topology.New("a")
	osc := map[oscillator.ID]*oscillator.Oscillator{
		"a": oscillator.NewWithTracer("a", stub.New("a", stub.ModeDone), nil),
	}
	tracer := &captureTracer{}
	cfg := Config{
		Topology:     topo,
		Oscillators:  osc,
		Router:       rule.New(),
		Inhibitor:    hardcap.New(2),
		Initial:      session.Envelope{ID: "init"},
		Tracer:       tracer,
		ChainTimeout: 1 * time.Millisecond, // arbitrarily short, but no advertising adapter
	}
	// Use long ctx so the 1ms ChainTimeout doesn't trip immediately;
	// the stub returns almost instantly.
	if _, err := Run(context.Background(), cfg); err != nil {
		// ctx-done is acceptable here — we only care about the warning event.
	}
	if w := tracer.byName("runner.chain_timeout_below_adapter_floor"); len(w) != 0 {
		t.Errorf("plain stub adapter shouldn't trigger the advisory: %d warnings", len(w))
	}
}
