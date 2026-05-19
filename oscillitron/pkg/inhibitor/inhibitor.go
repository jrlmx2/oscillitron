// CLAUDE GENERATED
// Package inhibitor defines the contract for the circuit-breaker that
// watches a reasoning path and decides whether to continue, restart,
// or abort. Brain analog: anterior cingulate modulates signal flow
// between regions (locked as an edge property, 2026-05-18 — see
// parent CLAUDE.md).
//
// Under the call-tree model the runner invokes Check at the dispatch
// edge between a parent invocation and each child sub-AP. The slice
// passed to Check is the root→current path so far, not a linear chain;
// the signature is unchanged because the data shape is the same.
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

// Inhibitor watches a path through the call tree and decides whether
// to continue. The slice is the ordered root→current sequence of
// envelopes, most recent last.
//
// Phase 2 implementations only enforce the most basic signals (hard
// depth cap, confidence threshold). Learned drift detection —
// contradiction with earlier summaries, repetition cycling,
// parallel-specialist disagreement — grows over time.
type Inhibitor interface {
	Check(path []session.Envelope) Verdict
}
