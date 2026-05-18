// CLAUDE GENERATED
// Package router defines the contract for deciding which oscillators
// receive an AP, given the AP and the current topology. Per
// library-plan §3.2 and §3.3: the router specialist is owned by
// Oscillitron (not Hermes) — this is where most of the system's
// orchestration intelligence will eventually live.
//
// v0 implementation in router/rule is rule-based; a learned router
// arrives with the self-improvement loop in a later phase.
package router

import (
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/topology"
)

// Destination is a single (oscillator, firing-threshold) target. Per
// library-plan §3.3: "take an AP, return an ordered list of
// (instance_id, threshold) pairs." Multiple destinations means
// parallel ensemble; single destination is the cheap fast path.
//
// Threshold is a hint to the runner about how strongly to weight this
// destination's response when merging — only relevant for ensemble
// fan-out, ignored on single-destination routing.
type Destination struct {
	To        oscillator.ID
	Threshold float64
}

// Decision is the router's output. Terminal=true means the AP chain
// ends here (e.g. the envelope's Outcome is final). Destinations is
// the ordered list of next-hop oscillators when Terminal=false.
type Decision struct {
	Terminal     bool
	Destinations []Destination
	Reason       string
}

// Router decides where an AP goes next.
type Router interface {
	// Route inspects the envelope produced by `from` and returns a
	// routing Decision. The router consults the topology to find
	// candidate destinations and may use the envelope's Outcome
	// (Confidence, Signals, ExitReason) plus classification to choose
	// among them.
	Route(from oscillator.ID, env session.Envelope, topo *topology.Topology) (Decision, error)
}
