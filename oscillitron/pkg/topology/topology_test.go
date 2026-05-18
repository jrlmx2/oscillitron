// CLAUDE GENERATED
package topology

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/oscillator"
)

func TestTopologyEntryAndNodes(t *testing.T) {
	topo := New("router")
	if topo.Entry() != "router" {
		t.Errorf("Entry() = %q, want %q", topo.Entry(), "router")
	}
	if !topo.Has("router") {
		t.Error("entry should be auto-registered as a node")
	}
	topo.AddNode("code")
	if !topo.Has("code") {
		t.Error("AddNode did not register node")
	}
}

func TestTopologyAddEdge(t *testing.T) {
	topo := New("router")
	if err := topo.AddEdge("router", Edge{To: "code", Weight: 1.0}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if !topo.Has("code") {
		t.Error("AddEdge should auto-register endpoints")
	}

	edges, err := topo.OutgoingFrom("router")
	if err != nil {
		t.Fatalf("OutgoingFrom: %v", err)
	}
	if len(edges) != 1 || edges[0].To != "code" || edges[0].Weight != 1.0 {
		t.Errorf("unexpected edges: %+v", edges)
	}
}

func TestTopologyAddEdgeRejectsEmpty(t *testing.T) {
	topo := New("entry")
	if err := topo.AddEdge("", Edge{To: "x"}); err == nil {
		t.Error("expected error for empty from")
	}
	if err := topo.AddEdge("entry", Edge{To: ""}); err == nil {
		t.Error("expected error for empty To")
	}
}

func TestTopologyOutgoingFromUnknownNode(t *testing.T) {
	topo := New("router")
	if _, err := topo.OutgoingFrom(oscillator.ID("ghost")); err == nil {
		t.Error("expected error for unknown node")
	}
}

func TestTopologyOutgoingFromSinkReturnsEmpty(t *testing.T) {
	topo := New("router")
	topo.AddNode("sink")
	edges, err := topo.OutgoingFrom("sink")
	if err != nil {
		t.Fatalf("OutgoingFrom: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("sink should have no outgoing edges, got %v", edges)
	}
}
