// CLAUDE GENERATED
package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestNew_Validation(t *testing.T) {
	if _, err := New("", nil); err == nil {
		t.Error("expected error on empty name")
	}
	if _, err := New("p", nil); err == nil {
		t.Error("expected error on empty backends")
	}
}

func TestRoundRobin_VisitsEveryBackendInOrder(t *testing.T) {
	a := stub.New("a", stub.ModeDone)
	b := stub.New("b", stub.ModeDone)
	c := stub.New("c", stub.ModeDone)
	p, err := New("region", nil, a, b, c)
	if err != nil {
		t.Fatal(err)
	}
	const rounds = 30
	for i := 0; i < rounds; i++ {
		if _, err := p.Call(context.Background(), session.Envelope{Objective: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	// Each backend should have been visited rounds/3 = 10 times.
	if a.Calls() != rounds/3 || b.Calls() != rounds/3 || c.Calls() != rounds/3 {
		t.Errorf("uneven distribution: a=%d b=%d c=%d (want %d each)",
			a.Calls(), b.Calls(), c.Calls(), rounds/3)
	}
}

func TestPool_NamePropagates(t *testing.T) {
	a := stub.New("inner", stub.ModeDone)
	p, _ := New("brain-region", nil, a)
	if p.Name() != "brain-region" {
		t.Errorf("Name = %q, want %q (pool name, not backing adapter name)", p.Name(), "brain-region")
	}
}

// TestPool_ConcurrentCallsLoadBalance proves the pool both
// load-balances and tolerates aggressive concurrent dispatch. Under
// -race this also asserts the strategy is goroutine-safe.
func TestPool_ConcurrentCallsLoadBalance(t *testing.T) {
	const (
		backends = 4
		workers  = 16
		perWork  = 200
	)
	stubs := make([]*stub.Adapter, backends)
	backingAdapters := make([]adapter.Adapter, backends)
	for i := range stubs {
		stubs[i] = stub.New("s", stub.ModeDone)
		backingAdapters[i] = stubs[i]
	}
	p, err := New("region", nil, backingAdapters...)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perWork; i++ {
				if _, err := p.Call(context.Background(), session.Envelope{Objective: "x"}); err != nil {
					t.Errorf("Call: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	totalCalls := int64(workers * perWork)
	var sum int64
	for _, s := range stubs {
		sum += s.Calls()
	}
	if sum != totalCalls {
		t.Errorf("total calls = %d, want %d (lost dispatch indicates a race)", sum, totalCalls)
	}
	// Each backend should be within ±5% of the mean.
	mean := float64(totalCalls) / float64(backends)
	for i, s := range stubs {
		dev := float64(s.Calls()) - mean
		if dev < 0 {
			dev = -dev
		}
		if dev > mean*0.05 {
			t.Errorf("backend %d got %d calls; want within 5%% of mean %.0f", i, s.Calls(), mean)
		}
	}
}

// staticStrategy always returns 0 — proves the pool actually routes
// according to its Strategy and isn't doing its own RR internally.
type staticStrategy struct{ idx atomic.Int64 }

func (s *staticStrategy) Pick(n int) int { return int(s.idx.Load()) }

func TestPool_HonorsStrategy(t *testing.T) {
	a := stub.New("a", stub.ModeDone)
	b := stub.New("b", stub.ModeDone)
	p, _ := New("p", &staticStrategy{}, a, b)
	for i := 0; i < 10; i++ {
		_, _ = p.Call(context.Background(), session.Envelope{Objective: "x"})
	}
	if a.Calls() != 10 || b.Calls() != 0 {
		t.Errorf("staticStrategy pinned to 0 should send all to a: a=%d b=%d", a.Calls(), b.Calls())
	}
}
