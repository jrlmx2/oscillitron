// CLAUDE GENERATED
package registry

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestRegisterAndLookup(t *testing.T) {
	r := New()
	osc := oscillator.New("reasoner-1", session.BrainReasoning,
		stub.New("stub-reasoner", stub.ModeDone), nil)
	r.Register(session.BrainReasoning, osc)

	got, err := r.Lookup(session.BrainReasoning)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != osc {
		t.Fatalf("Lookup returned wrong oscillator")
	}
}

func TestLookupUnknownBrainFunction(t *testing.T) {
	r := New()
	if _, err := r.Lookup(session.BrainCritic); err == nil {
		t.Fatal("expected error looking up unregistered brain function")
	}
}

func TestRegisterOverwrites(t *testing.T) {
	r := New()
	first := oscillator.New("a", session.BrainReasoning, stub.New("a", stub.ModeDone), nil)
	second := oscillator.New("b", session.BrainReasoning, stub.New("b", stub.ModeDone), nil)
	r.Register(session.BrainReasoning, first)
	r.Register(session.BrainReasoning, second)
	got, _ := r.Lookup(session.BrainReasoning)
	if got != second {
		t.Fatal("second Register should overwrite first")
	}
}

func TestFunctionsListsAllBindings(t *testing.T) {
	r := New()
	r.Register(session.BrainReasoning, oscillator.New("r", session.BrainReasoning, stub.New("r", stub.ModeDone), nil))
	r.Register(session.BrainCritic, oscillator.New("c", session.BrainCritic, stub.New("c", stub.ModeDone), nil))
	fns := r.Functions()
	if len(fns) != 2 {
		t.Fatalf("Functions: got %d, want 2", len(fns))
	}
}
