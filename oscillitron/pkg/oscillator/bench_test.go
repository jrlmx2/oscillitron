// CLAUDE GENERATED
package oscillator

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// BenchmarkOscillator_Dispatch measures the per-call overhead of an
// oscillator wrapping a no-op adapter. Real workloads are gated by
// model latency, but this number sets a floor: anything we add to
// the dispatch path (more tracing, more validation, …) shows up here.
func BenchmarkOscillator_Dispatch(b *testing.B) {
	o := NewWithTracer("bench", stub.New("noop", stub.ModeDone), nil)
	env := session.Envelope{
		ID:        "bench",
		Type:      session.TypeAnalyze,
		Objective: "noop",
		Input:     session.Input{Type: "prompt", Content: "x"},
	}
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = o.dispatch(ctx, env)
	}
}
