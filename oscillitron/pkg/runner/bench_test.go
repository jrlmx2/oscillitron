// CLAUDE GENERATED
package runner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router/rule"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

// BenchmarkRunner_ChainThroughput measures end-to-end chain time for
// a 3-hop topology with stub adapters. Real workloads are dominated
// by model latency; this floor captures orchestrator overhead
// (channels, router, inhibitor, oscillator goroutines, ID minting).
//
// Each iteration constructs fresh oscillators because Run consumes
// their input channels (oscillators aren't reusable post-Run); the
// construction cost is included in the benchmark — that's a fair
// reflection of the per-chain cost for short-lived APs.
func BenchmarkRunner_ChainThroughput(b *testing.B) {
	build := func() Config {
		topo := topology.New("a")
		_ = topo.AddEdge("a", topology.Edge{To: "b", Weight: 1.0})
		_ = topo.AddEdge("b", topology.Edge{To: "c", Weight: 1.0})
		return Config{
			Topology: topo,
			Oscillators: map[oscillator.ID]*oscillator.Oscillator{
				"a": oscillator.New("a", stub.New("a", stub.ModeBudgetExhausted), nil),
				"b": oscillator.New("b", stub.New("b", stub.ModeBudgetExhausted), nil),
				"c": oscillator.New("c", stub.New("c", stub.ModeDone), nil),
			},
			Router:    rule.New(),
			Inhibitor: hardcap.New(10),
			Initial:   session.Envelope{ID: "init", Objective: "x"},
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Run(ctx, build()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRunner_ParallelChains measures how the orchestrator
// scales when many independent chains run concurrently. Each chain
// is its own goroutine and its own Config. Useful as a sanity check
// against scaling regressions — e.g. if a future change introduces
// contention through a misplaced global mutex.
func BenchmarkRunner_ParallelChains(b *testing.B) {
	build := func(id int) Config {
		entry := oscillator.ID(fmt.Sprintf("a-%d", id))
		sink := oscillator.ID(fmt.Sprintf("b-%d", id))
		topo := topology.New(entry)
		_ = topo.AddEdge(entry, topology.Edge{To: sink, Weight: 1.0})
		return Config{
			Topology: topo,
			Oscillators: map[oscillator.ID]*oscillator.Oscillator{
				entry: oscillator.New(entry, stub.New("a", stub.ModeBudgetExhausted), nil),
				sink:  oscillator.New(sink, stub.New("b", stub.ModeDone), nil),
			},
			Router:    rule.New(),
			Inhibitor: hardcap.New(5),
			Initial:   session.Envelope{ID: "init", Objective: "x"},
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			if _, err := Run(ctx, build(i)); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
