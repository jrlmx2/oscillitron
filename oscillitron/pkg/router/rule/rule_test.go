// CLAUDE GENERATED
package rule

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

func newTopo() *topology.Topology {
	topo := topology.New("router")
	_ = topo.AddEdge("router", topology.Edge{To: "code", Weight: 0.3})
	_ = topo.AddEdge("router", topology.Edge{To: "writer", Weight: 0.9})
	_ = topo.AddEdge("code", topology.Edge{To: "writer", Weight: 1.0})
	topo.AddNode("writer") // sink
	return topo
}

func TestRouterPicksHighestWeightEdge(t *testing.T) {
	r := New()
	topo := newTopo()
	env := session.Envelope{
		ID: "s1",
		Outcome: &session.Outcome{
			ExitReason: session.ExitBudgetExhausted, // not terminal — must route
		},
	}
	dec, err := r.Route("router", env, topo)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if dec.Terminal {
		t.Fatal("decision should not be terminal")
	}
	if len(dec.Destinations) != 1 || dec.Destinations[0].To != "writer" {
		t.Errorf("expected single destination 'writer', got %+v", dec.Destinations)
	}
}

func TestRouterShortCircuitsTerminal(t *testing.T) {
	r := New()
	topo := newTopo()
	env := session.Envelope{
		Outcome: &session.Outcome{ExitReason: session.ExitDone},
	}
	dec, _ := r.Route("code", env, topo)
	if !dec.Terminal {
		t.Error("ExitDone should yield Terminal=true")
	}
}

func TestRouterTreatsSinkAsTerminal(t *testing.T) {
	r := New()
	topo := newTopo()
	env := session.Envelope{
		Outcome: &session.Outcome{ExitReason: session.ExitBudgetExhausted},
	}
	dec, err := r.Route("writer", env, topo)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if !dec.Terminal {
		t.Error("sink with no outgoing edges should be terminal")
	}
}

func TestRouterRejectsUnknownNode(t *testing.T) {
	r := New()
	topo := newTopo()
	env := session.Envelope{
		Outcome: &session.Outcome{ExitReason: session.ExitBudgetExhausted},
	}
	if _, err := r.Route("ghost", env, topo); err == nil {
		t.Error("expected error for unknown node")
	}
}
