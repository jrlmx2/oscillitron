// Package stakes defines the "how careful do we need to be about
// getting this right" axis, distinct from data classification.
//
// Classification (pkg/classification) is *where can this data go* —
// confidentiality routing. Stakes is *how much effort should we
// spend ensuring correctness* — effort routing. Both axes are
// orthogonal: a Public input can be high-stakes (drafting a press
// release) and a Regulated input can be low-stakes (auto-deleting
// expired sessions per a policy you trust).
//
// v3.0 is the first runtime consumer: the bench Vote orchestrator
// scales its attempt count based on env.Stakes. Future v3 phases
// add more decisions keyed off stakes (signal-driven re-runs,
// frontier escalation, critique step injection).
//
// The zero value (empty string) is intentionally NOT a valid level
// and is treated as Medium by Effective(). This keeps existing
// callers backwards-compatible: an envelope constructed by the
// pre-v3.0 NewRoot() helper has Stakes="" which Effective() reads
// as Medium — same behavior as before v3.0 landed.
package stakes

// Level is the per-AP stakes annotation.
type Level string

const (
	// Low — "if this is wrong, the cost is small." Single-call
	// path; orchestration is overkill.
	Low Level = "low"

	// Medium — "this is the default." Whatever the bench's
	// --vote-n is set to.
	Medium Level = "medium"

	// High — "wrong here is expensive." Doubled vote count by
	// default; future phases add critique step + escalation.
	High Level = "high"
)

// Effective returns the level that consumers should act on,
// substituting Medium for the zero value (empty string).
// Use this at every read site so unset stakes degrade to the
// pre-v3.0 default behavior.
func Effective(l Level) Level {
	switch l {
	case Low, Medium, High:
		return l
	default:
		return Medium
	}
}

// Valid reports whether l is a known stakes level (not counting
// the zero value).
func Valid(l Level) bool {
	switch l {
	case Low, Medium, High:
		return true
	}
	return false
}

// AttemptScale returns the multiplier on the base attempt count
// (the bench's --vote-n) for a given stakes level:
//
//	Low    → 1   attempt regardless of base
//	Medium → base attempts
//	High   → 2× base attempts
//
// Returned value is the EFFECTIVE attempt count given base.
// Low caps at 1 because the cheap-path skips voting entirely;
// running vote-1 is a single-call path with no consensus
// machinery.
func AttemptScale(l Level, base int) int {
	if base < 1 {
		base = 1
	}
	switch Effective(l) {
	case Low:
		return 1
	case High:
		return base * 2
	default:
		return base
	}
}
