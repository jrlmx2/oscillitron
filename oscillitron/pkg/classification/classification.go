// Package classification defines data-classification levels propagated
// through session envelopes and used for routing decisions.
//
// Per library-plan §5.2. The field exists in the schema from Phase 1
// onward but is inert (not used by routing) through Phase 3. Phase 4
// activates classification-aware routing.
package classification

// Level is a data-classification level. Ordered from least to most
// restrictive: Public < Internal < Confidential < Regulated.
type Level string

const (
	Public       Level = "public"
	Internal     Level = "internal"
	Confidential Level = "confidential"
	Regulated    Level = "regulated"
)

// rank gives a comparable ordering. Higher rank = more restrictive.
// Unknown levels rank as the most restrictive (fail closed).
func rank(l Level) int {
	switch l {
	case Public:
		return 0
	case Internal:
		return 1
	case Confidential:
		return 2
	case Regulated:
		return 3
	default:
		return 99
	}
}

// Propagate returns the more-restrictive of two levels. Used when an
// AP carries data from multiple upstream sessions: the downstream
// session inherits the strictest classification of any input.
func Propagate(a, b Level) Level {
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

// Valid reports whether l is a known classification level.
func Valid(l Level) bool {
	switch l {
	case Public, Internal, Confidential, Regulated:
		return true
	}
	return false
}
