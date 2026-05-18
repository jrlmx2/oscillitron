// CLAUDE GENERATED
// Package rule is a v0 rule-based Router. It picks the highest-weight
// outgoing edge from the upstream oscillator (single destination, fast
// path). Terminal envelopes short-circuit to Decision{Terminal: true}.
//
// This is deliberately minimal. Real routing intelligence — content
// inspection, learned edge weights, ensemble fan-out — arrives in a
// later phase. The interface (router.Router) is the stable contract;
// this implementation is replaceable.
package rule

import (
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/router"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

// Router is a rule-based router.
type Router struct{}

// New constructs a rule-based router.
func New() *Router { return &Router{} }

// Route implements router.Router.
func (r *Router) Route(from oscillator.ID, env session.Envelope, topo *topology.Topology) (router.Decision, error) {
	if env.IsTerminal() {
		return router.Decision{
			Terminal: true,
			Reason:   "envelope is terminal (exit_reason=done|inhibited)",
		}, nil
	}

	edges, err := topo.OutgoingFrom(from)
	if err != nil {
		return router.Decision{}, fmt.Errorf("rule.Route: %w", err)
	}
	if len(edges) == 0 {
		return router.Decision{
			Terminal: true,
			Reason:   fmt.Sprintf("no outgoing edges from %q; treating as sink", from),
		}, nil
	}

	// Pick the highest-weight edge. Stable tie-break by first
	// occurrence (since the slice is already in insertion order).
	best := edges[0]
	for _, e := range edges[1:] {
		if e.Weight > best.Weight {
			best = e
		}
	}

	return router.Decision{
		Destinations: []router.Destination{{To: best.To, Threshold: 0}},
		Reason:       fmt.Sprintf("highest-weight edge from %q -> %q (w=%.2f)", from, best.To, best.Weight),
	}, nil
}

// Compile-time check that *Router implements router.Router.
var _ router.Router = (*Router)(nil)
