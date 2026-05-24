// Package inhibitor defines the contract for the circuit-breaker that
// watches a reasoning path and decides whether to continue, restart,
// or abort.
//
// Inhibition is an EDGE PROPERTY, not a node (LOCKED 2026-05-18 — see
// parent CLAUDE.md). Brain analog: anterior cingulate modulates signal
// flow between regions rather than acting as a separate cortical
// region. Concretely, an Inhibitor sits on the parent→child edge in
// the call tree: it fires after a child invocation resolves and
// before that resolution flows further (into the child's own sub-AP
// dispatch, or back up into the parent's recomposition). The root has
// no incoming edge, so it is never checked.
//
// Path-stateful detectors (confidence drop windows, cumulative
// contradictions, repetition cycling) still see the full root→child
// path on the Edge; the slice shape is unchanged, what changes is
// the framing — we are judging the traversal that produced the
// current child, not the child as a standalone node.
package inhibitor

import "github.com/jrlmx2/oscillitron/pkg/session"

// Decision is the inhibitor's verdict on an edge traversal.
type Decision int

const (
	// Continue — the edge is healthy, allow the child's output to
	// flow further.
	Continue Decision = iota
	// Restart — the edge showed drift; restart from the last good
	// checkpoint with a reformulated input. v0 callers may treat this
	// as Abort if no checkpointing is implemented yet.
	Restart
	// Abort — the edge is unsalvageable, stop.
	Abort
)

// Verdict is the inhibitor's output.
type Verdict struct {
	Decision   Decision
	Reason     string
	Checkpoint int // for Restart: index in the path to restart from. 0 means start of path.
}

// Edge is a parent→child traversal in the call tree, presented to the
// inhibitor after the child has resolved. Parent is the immediate
// caller; Child is the just-resolved invocation; Path is the full
// root→child sequence (inclusive of Child at the tail) for stateful
// detectors that look back along the branch.
//
// The root has no incoming edge and is never checked, so Parent is
// always non-nil when an Inhibitor is invoked.
type Edge struct {
	Parent *session.Envelope
	Child  session.Envelope
	Path   []session.Envelope
}

// Inhibitor watches an edge traversal and decides whether the
// child's output should flow further.
//
// Phase 2 implementations only enforce the most basic signals (hard
// depth cap, confidence threshold). Learned drift detection —
// contradiction with earlier summaries, repetition cycling,
// parallel-specialist disagreement — grows over time.
type Inhibitor interface {
	Check(edge Edge) Verdict
}
