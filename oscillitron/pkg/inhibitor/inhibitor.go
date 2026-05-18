// CLAUDE GENERATED
// Package inhibitor defines the contract for the circuit-breaker that
// watches a reasoning chain and decides whether to continue, restart,
// or abort. Per design-notes.md "Inhibition as circuit-breaker":
// brain analog is anterior cingulate detecting conflict and signaling
// override to prefrontal cortex.
//
// Open in design-notes.md (and library-plan §7.11): inhibitor as a
// dedicated node vs. as a property attached to every chain edge. v0
// treats it as a process called by the runner after each hop.
package inhibitor

import "github.com/jrlmx2/oscillitron/pkg/session"

// Decision is the inhibitor's verdict on a chain.
type Decision int

const (
	// Continue — chain is healthy, keep going.
	Continue Decision = iota
	// Restart — chain showed drift; restart from the last good
	// checkpoint with a reformulated input. v0 callers may treat this
	// as Abort if no checkpointing is implemented yet.
	Restart
	// Abort — chain is unsalvageable, stop.
	Abort
)

// Verdict is the inhibitor's output.
type Verdict struct {
	Decision   Decision
	Reason     string
	Checkpoint int // for Restart: index in the chain to restart from. 0 means start of chain.
}

// Inhibitor watches a chain and decides whether to continue. Chains
// are passed as the full ordered slice of envelopes processed so far,
// most recent last.
//
// Phase 2 implementations only enforce the most basic signals (hard
// max-iteration cap, confidence threshold). Learned drift detection
// — contradiction with earlier summaries, repetition cycling,
// parallel-specialist disagreement — grows over time as the
// inhibitor's playbook expands.
type Inhibitor interface {
	Check(chain []session.Envelope) Verdict
}
