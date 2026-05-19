// CLAUDE GENERATED
// Package hardcap is a v0 Inhibitor that aborts a chain when it
// exceeds a fixed iteration cap. This is the "belt-and-suspenders"
// inhibition per design-notes.md "Inhibition as circuit-breaker"
// > Hard max-iteration cap. Even with inhibition, the inhibitor
// > itself can fail or drift can be subtle. A hard cap (N sessions
// > or M total tokens) is the belt-and-suspenders.
//
// Drift-signal inhibition (grounded checks, contradiction detection,
// confidence drops, repetition) grows in a later phase.
package hardcap

import (
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
)

// Inhibitor enforces a maximum chain length.
type Inhibitor struct {
	MaxSteps int
}

// New constructs a hard-cap inhibitor with the given limit.
func New(maxSteps int) *Inhibitor { return &Inhibitor{MaxSteps: maxSteps} }

// Check implements inhibitor.Inhibitor.
func (h *Inhibitor) Check(edge inhibitor.Edge) inhibitor.Verdict {
	if len(edge.Path) > h.MaxSteps {
		return inhibitor.Verdict{
			Decision: inhibitor.Abort,
			Reason:   fmt.Sprintf("hardcap: path depth %d exceeded max %d", len(edge.Path), h.MaxSteps),
		}
	}
	return inhibitor.Verdict{Decision: inhibitor.Continue}
}

// Compile-time check that *Inhibitor implements inhibitor.Inhibitor.
var _ inhibitor.Inhibitor = (*Inhibitor)(nil)
