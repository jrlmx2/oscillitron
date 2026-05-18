// CLAUDE GENERATED
// Package confidence is an Inhibitor that watches per-session
// confidence scores reported by adapters via Outcome.Confidence. Two
// drift signals per design-notes.md "Inhibition as circuit-breaker":
//
//   - Floor: if the most recent envelope's confidence falls below
//     MinFloor, abort. A single very-unsure session is enough.
//   - Drop:  if confidence drops by at least MaxDrop between any two
//     envelopes inside the trailing Window (oldest → newest), restart.
//     A decaying confidence trend across sessions is the canonical
//     drift signature.
//
// Envelopes without an Outcome (or with Confidence==0, the zero
// value) are skipped — a missing signal is not a negative signal.
package confidence

import (
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Inhibitor configures the floor + drop detector.
type Inhibitor struct {
	// MinFloor is the absolute floor. Latest confidence below this
	// triggers Abort. Set to 0 to disable the floor check.
	MinFloor float64
	// MaxDrop is the maximum confidence drop tolerated inside Window.
	// A drop of MaxDrop or more triggers Restart. Set to 0 to disable
	// the drop check.
	MaxDrop float64
	// Window is the number of trailing envelopes the drop check looks
	// at. Defaults to 3 when zero (compare across last three sessions).
	Window int
}

// New constructs a confidence inhibitor with the given thresholds.
func New(minFloor, maxDrop float64, window int) *Inhibitor {
	return &Inhibitor{MinFloor: minFloor, MaxDrop: maxDrop, Window: window}
}

// Check implements inhibitor.Inhibitor.
func (c *Inhibitor) Check(chain []session.Envelope) inhibitor.Verdict {
	scores := collectScores(chain)
	if len(scores) == 0 {
		return inhibitor.Verdict{Decision: inhibitor.Continue}
	}

	// Floor: examine the most recent reported confidence.
	if c.MinFloor > 0 {
		latest := scores[len(scores)-1]
		if latest < c.MinFloor {
			return inhibitor.Verdict{
				Decision: inhibitor.Abort,
				Reason:   fmt.Sprintf("confidence: latest %.2f below floor %.2f", latest, c.MinFloor),
			}
		}
	}

	// Drop: max-minus-min inside trailing window. Captures both steady
	// decay and sudden cliffs without needing slope math.
	if c.MaxDrop > 0 {
		window := c.Window
		if window <= 0 {
			window = 3
		}
		if len(scores) >= 2 {
			start := len(scores) - window
			if start < 0 {
				start = 0
			}
			recent := scores[start:]
			// Find peak before the min that follows it — a drop only
			// counts if confidence went high then low, not low then high.
			peak := recent[0]
			worstDrop := 0.0
			for _, s := range recent[1:] {
				if s > peak {
					peak = s
					continue
				}
				if drop := peak - s; drop > worstDrop {
					worstDrop = drop
				}
			}
			if worstDrop >= c.MaxDrop {
				return inhibitor.Verdict{
					Decision: inhibitor.Restart,
					Reason: fmt.Sprintf("confidence: dropped %.2f within last %d sessions (threshold %.2f)",
						worstDrop, len(recent), c.MaxDrop),
					Checkpoint: len(chain) - len(recent),
				}
			}
		}
	}

	return inhibitor.Verdict{Decision: inhibitor.Continue}
}

func collectScores(chain []session.Envelope) []float64 {
	out := make([]float64, 0, len(chain))
	for _, e := range chain {
		if e.Outcome == nil {
			continue
		}
		if e.Outcome.Confidence == 0 {
			// Treat zero as "not reported." Avoids a stub or unset
			// adapter looking like total uncertainty.
			continue
		}
		out = append(out, e.Outcome.Confidence)
	}
	return out
}

var _ inhibitor.Inhibitor = (*Inhibitor)(nil)
