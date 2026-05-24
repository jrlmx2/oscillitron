// Package contradictions is an Inhibitor that watches the
// return_result Signals.Contradictions field accumulating along a
// path through the call tree. Per design-notes.md "Drift signals to
// watch": contradiction with an earlier summary is a primary drift
// indicator. APs that did not produce a return_result execute payload
// contribute nothing to the count.
//
// Two thresholds:
//
//   - Spike: any single session reporting at least Spike
//     contradictions triggers immediate Abort. A burst is more
//     suspicious than slow accretion.
//   - MaxTotal: cumulative contradictions across the chain at or
//     above MaxTotal triggers Abort. Catches slow drift that never
//     spikes.
//
// Either threshold can be set to 0 to disable that check.
package contradictions

import (
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
)

// Inhibitor configures the contradiction detector.
type Inhibitor struct {
	Spike    int
	MaxTotal int
}

// New constructs a contradictions inhibitor.
func New(spike, maxTotal int) *Inhibitor {
	return &Inhibitor{Spike: spike, MaxTotal: maxTotal}
}

// Check implements inhibitor.Inhibitor.
func (c *Inhibitor) Check(edge inhibitor.Edge) inhibitor.Verdict {
	total := 0
	for i, e := range edge.Path {
		if e.Execute == nil || e.Execute.ReturnResult == nil {
			continue
		}
		n := len(e.Execute.ReturnResult.Signals.Contradictions)
		total += n
		if c.Spike > 0 && n >= c.Spike {
			return inhibitor.Verdict{
				Decision: inhibitor.Abort,
				Reason: fmt.Sprintf("contradictions: invocation %d reported %d (spike threshold %d)",
					i, n, c.Spike),
			}
		}
	}
	if c.MaxTotal > 0 && total >= c.MaxTotal {
		return inhibitor.Verdict{
			Decision: inhibitor.Abort,
			Reason:   fmt.Sprintf("contradictions: %d total along path (threshold %d)", total, c.MaxTotal),
		}
	}
	return inhibitor.Verdict{Decision: inhibitor.Continue}
}

var _ inhibitor.Inhibitor = (*Inhibitor)(nil)
