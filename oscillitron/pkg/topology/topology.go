// CLAUDE GENERATED
// Package topology models the directed graph between oscillators.
// Nodes are oscillator IDs; edges carry a weight and optional tags
// that the router can use when deciding where to fire next.
//
// Per CLAUDE.md: "The graph is the main learnable substrate."
// Content specialists grow within their niches via Hermes' skill
// creation; the bulk of self-improvement happens at the topology
// layer — strengthening edges, shifting thresholds, adding or
// pruning routes. Phase 2's topology is static and config-driven;
// learnable edge updates arrive in a later phase.
package topology

import (
	"errors"
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/oscillator"
)

// Edge is a directed connection from one oscillator to another.
// Weight is the router's hint about how strongly this edge should
// fire; higher = preferred. Tags are free-form labels the router
// can match on (e.g. "fast-path", "writing", "code-review").
type Edge struct {
	To     oscillator.ID
	Weight float64
	Tags   []string
}

// Topology is a directed graph of oscillator nodes and weighted edges.
type Topology struct {
	nodes map[oscillator.ID]struct{}
	edges map[oscillator.ID][]Edge
	entry oscillator.ID
}

// New constructs an empty topology with the given entry node. The
// entry node is added to the node set automatically.
func New(entry oscillator.ID) *Topology {
	t := &Topology{
		nodes: map[oscillator.ID]struct{}{},
		edges: map[oscillator.ID][]Edge{},
		entry: entry,
	}
	t.nodes[entry] = struct{}{}
	return t
}

// AddNode registers an oscillator ID. Idempotent.
func (t *Topology) AddNode(id oscillator.ID) {
	t.nodes[id] = struct{}{}
}

// AddEdge adds a directed edge from -> e.To. Both endpoints are
// registered as nodes if they were not already.
func (t *Topology) AddEdge(from oscillator.ID, e Edge) error {
	if from == "" || e.To == "" {
		return errors.New("topology: edge endpoints must be non-empty")
	}
	t.AddNode(from)
	t.AddNode(e.To)
	t.edges[from] = append(t.edges[from], e)
	return nil
}

// Entry returns the entry oscillator — where a fresh AP enters.
func (t *Topology) Entry() oscillator.ID { return t.entry }

// Has reports whether id is a node in the topology.
func (t *Topology) Has(id oscillator.ID) bool {
	_, ok := t.nodes[id]
	return ok
}

// Nodes returns all node IDs (order not stable).
func (t *Topology) Nodes() []oscillator.ID {
	ids := make([]oscillator.ID, 0, len(t.nodes))
	for id := range t.nodes {
		ids = append(ids, id)
	}
	return ids
}

// OutgoingFrom returns the edges leaving from. Returns an empty slice
// (not nil) when from has no outgoing edges. Returns an error if from
// is not a known node, so the caller catches typos at routing time
// rather than silently emitting nothing.
func (t *Topology) OutgoingFrom(from oscillator.ID) ([]Edge, error) {
	if !t.Has(from) {
		return nil, fmt.Errorf("topology: unknown node %q", from)
	}
	return t.edges[from], nil
}
