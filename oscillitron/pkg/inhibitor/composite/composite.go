// CLAUDE GENERATED
// Package composite combines multiple Inhibitors and aggregates their
// verdicts. Precedence is Abort > Restart > Continue: any member
// voting Abort produces an Abort; otherwise any Restart produces a
// Restart (with the smallest Checkpoint — the most conservative
// rewind); otherwise Continue. Reasons from contributing members are
// concatenated so the runner log shows everything that fired.
//
// This is how the inhibitor's drift-signal layer is assembled per
// design-notes.md "Inhibition as circuit-breaker": each drift signal
// lives in its own small inhibitor; the composite is the entry point.
package composite

import (
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Inhibitor runs each member and aggregates verdicts.
type Inhibitor struct {
	members []inhibitor.Inhibitor
}

// New constructs a composite from the given members. Order is not
// significant — verdicts aggregate by precedence, not by position.
func New(members ...inhibitor.Inhibitor) *Inhibitor {
	return &Inhibitor{members: members}
}

// Check implements inhibitor.Inhibitor.
func (c *Inhibitor) Check(chain []session.Envelope) inhibitor.Verdict {
	var (
		aborts        []string
		restarts      []string
		earliestCkpt  int
		restartChosen bool
	)

	for _, m := range c.members {
		v := m.Check(chain)
		switch v.Decision {
		case inhibitor.Abort:
			aborts = append(aborts, v.Reason)
		case inhibitor.Restart:
			restarts = append(restarts, v.Reason)
			if !restartChosen || v.Checkpoint < earliestCkpt {
				earliestCkpt = v.Checkpoint
				restartChosen = true
			}
		}
	}

	switch {
	case len(aborts) > 0:
		return inhibitor.Verdict{Decision: inhibitor.Abort, Reason: strings.Join(aborts, "; ")}
	case restartChosen:
		return inhibitor.Verdict{Decision: inhibitor.Restart, Reason: strings.Join(restarts, "; "), Checkpoint: earliestCkpt}
	default:
		return inhibitor.Verdict{Decision: inhibitor.Continue}
	}
}

var _ inhibitor.Inhibitor = (*Inhibitor)(nil)
