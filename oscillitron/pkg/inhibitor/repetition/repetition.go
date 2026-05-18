// CLAUDE GENERATED
// Package repetition is an Inhibitor that detects a specialist
// cycling — re-emitting the same verdict across recent sessions. Per
// design-notes.md "Drift signals to watch": repetition / cycling is
// the specialist re-trying the same approach.
//
// v0 detection is intentionally simple: count exact verdict
// duplicates inside a trailing Window. If any verdict appears
// MinRepeats or more times, abort. Empty verdicts (e.g. no Outcome
// yet) are skipped — a missing payload isn't a repetition.
//
// Future work: near-duplicate detection (cosine on small embeddings,
// token-set Jaccard) once the project takes on a dependency that
// makes that cheap. Exact match catches the most common cycling
// failure mode and is dependency-free.
package repetition

import (
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Inhibitor configures the repetition detector.
type Inhibitor struct {
	// Window is the size of the trailing slice examined. Defaults to 5
	// when zero.
	Window int
	// MinRepeats is how many times a verdict must appear inside Window
	// to trigger Abort. Must be >= 2; defaults to 3 when zero or less.
	MinRepeats int
}

// New constructs a repetition inhibitor.
func New(window, minRepeats int) *Inhibitor {
	return &Inhibitor{Window: window, MinRepeats: minRepeats}
}

// Check implements inhibitor.Inhibitor.
func (r *Inhibitor) Check(chain []session.Envelope) inhibitor.Verdict {
	window := r.Window
	if window <= 0 {
		window = 5
	}
	minRepeats := r.MinRepeats
	if minRepeats < 2 {
		minRepeats = 3
	}
	if len(chain) < minRepeats {
		return inhibitor.Verdict{Decision: inhibitor.Continue}
	}

	start := len(chain) - window
	if start < 0 {
		start = 0
	}
	counts := make(map[string]int, window)
	for _, e := range chain[start:] {
		if e.Outcome == nil || e.Outcome.Verdict == "" {
			continue
		}
		counts[e.Outcome.Verdict]++
	}
	for verdict, n := range counts {
		if n >= minRepeats {
			return inhibitor.Verdict{
				Decision: inhibitor.Abort,
				Reason: fmt.Sprintf("repetition: verdict repeated %d times in last %d sessions (%q)",
					n, len(chain[start:]), preview(verdict)),
			}
		}
	}
	return inhibitor.Verdict{Decision: inhibitor.Continue}
}

func preview(s string) string {
	const n = 48
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ inhibitor.Inhibitor = (*Inhibitor)(nil)
