// CLAUDE GENERATED
package runner

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

// TestRunner_ParallelChains verifies that many runner.Run calls
// driving independent topologies can execute concurrently without
// races, deadlocks, or cross-contamination. Each Run owns its own
// oscillator goroutines, channels, and chain state — but if any
// global/shared mutable state slips into runner.go (the monotonic
// idCounter is the only one today and it's atomic), this test would
// surface a regression under -race.
func TestRunner_ParallelChains(t *testing.T) {
	const chains = 32
	const hops = 4

	build := func(id int) Config {
		entry := oscillator.ID(fmt.Sprintf("entry-%d", id))
		mid := oscillator.ID(fmt.Sprintf("mid-%d", id))
		sink := oscillator.ID(fmt.Sprintf("sink-%d", id))
		topo := topology.New(entry)
		must(topo.AddEdge(entry, topology.Edge{To: mid, Weight: 1.0}))
		must(topo.AddEdge(mid, topology.Edge{To: sink, Weight: 1.0}))

		osc := map[oscillator.ID]*oscillator.Oscillator{
			entry: oscillator.New(entry, stub.New("e", stub.ModeBudgetExhausted), nil),
			mid:   oscillator.New(mid, stub.New("m", stub.ModeBudgetExhausted), nil),
			sink:  oscillator.New(sink, stub.New("s", stub.ModeDone), nil),
		}
		return Config{
			Topology:    topo,
			Oscillators: osc,
			Router:      rule.New(),
			Inhibitor:   hardcap.New(hops + 1),
			Initial:     session.Envelope{ID: session.ID(fmt.Sprintf("init-%d", id)), Objective: "x"},
		}
	}

	var wg sync.WaitGroup
	wg.Add(chains)
	errs := make(chan error, chains)
	for i := 0; i < chains; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			res, err := Run(ctx, build(i))
			if err != nil {
				errs <- fmt.Errorf("chain %d: %w", i, err)
				return
			}
			if res.Reason != ReasonTerminalOutcome {
				errs <- fmt.Errorf("chain %d ended with reason=%s detail=%s", i, res.Reason, res.Detail)
			}
			// Verify chain isn't contaminated by another runner's session IDs.
			wantPrefix := fmt.Sprintf("init-%d", i)
			if string(res.Chain[0].ID) != wantPrefix {
				errs <- fmt.Errorf("chain %d: first envelope ID = %q, want %q",
					i, res.Chain[0].ID, wantPrefix)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// fanOutRouter sends every AP to ALL outgoing edges. Stresses the
// runner's loop that pushes to multiple destination channels in one
// emission turn — a path that's hard to exercise with the default
// highest-weight rule router.
type fanOutRouter struct{}

func (fanOutRouter) Route(from oscillator.ID, env session.Envelope, topo *topology.Topology) (router.Decision, error) {
	edges, err := topo.OutgoingFrom(from)
	if err != nil || len(edges) == 0 {
		return router.Decision{Terminal: true, Reason: "no outgoing edges"}, nil
	}
	dests := make([]router.Destination, 0, len(edges))
	for _, e := range edges {
		dests = append(dests, router.Destination{To: e.To, Threshold: 0})
	}
	return router.Decision{Destinations: dests, Reason: "fan-out to all neighbors"}, nil
}

// TestRunner_FanOutMultipleDestinations exercises the multi-destination
// path in the runner's emission loop. Without a router that returns
// >1 destination, that branch goes uncovered. We don't expect a
// "correct" final outcome here (the runner doesn't recompose; the
// first ExitDone wins) — we just want it to terminate cleanly without
// hanging on a full channel or losing an envelope.
func TestRunner_FanOutMultipleDestinations(t *testing.T) {
	topo := topology.New("entry")
	must(topo.AddEdge("entry", topology.Edge{To: "a", Weight: 1.0}))
	must(topo.AddEdge("entry", topology.Edge{To: "b", Weight: 1.0}))
	must(topo.AddEdge("entry", topology.Edge{To: "c", Weight: 1.0}))

	osc := map[oscillator.ID]*oscillator.Oscillator{
		"entry": oscillator.New("entry", stub.New("e", stub.ModeBudgetExhausted), nil),
		"a":     oscillator.New("a", stub.New("a", stub.ModeDone), nil),
		"b":     oscillator.New("b", stub.New("b", stub.ModeDone), nil),
		"c":     oscillator.New("c", stub.New("c", stub.ModeDone), nil),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, err := Run(ctx, Config{
		Topology:    topo,
		Oscillators: osc,
		Router:      fanOutRouter{},
		Inhibitor:   hardcap.New(20),
		Initial:     session.Envelope{ID: "init", Objective: "fanout"},
		BufferSize:  4,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Some terminal reason — we just don't want a deadlock or panic.
	if res.Reason == "" {
		t.Errorf("Run returned empty reason: %+v", res)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
