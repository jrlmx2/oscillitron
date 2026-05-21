// CLAUDE GENERATED
package vram

// SessionEstimate is the input to an Estimator. Fields all relate to
// one Hermes session — the persona+pool+instructions prefix it holds,
// the conversation tokens accumulated so far in that session, and the
// model's hard context-window limit.
type SessionEstimate struct {
	// Model identifies the model family for trace/log purposes. Not
	// used by SlidingWindowEstimator's math (BytesPerToken is the
	// numeric handle) but useful for per-playbook telemetry.
	Model string
	// PrefixTokens is the persona + (optional) semantic pool +
	// per-playbook instructions prefix.
	PrefixTokens int
	// ObservedTokens is the conversation tokens accumulated in this
	// session so far (sum of all turns' input + output). Pass 0 for a
	// brand-new session.
	ObservedTokens int
	// ContextSize is the model's hard context-window ceiling. KV cache
	// can't grow past it; that's the sliding-window cap.
	ContextSize int
}

// Estimator predicts the per-session VRAM footprint at steady state.
type Estimator interface {
	Estimate(s SessionEstimate) uint64
}

// SlidingWindowEstimator is the v0 estimator. Math:
//
//	est = ModelResidentBytes +
//	      min(PrefixTokens + ObservedTokens, ContextSize) × BytesPerToken
//
// The sliding-window cap (min(..., ContextSize)) reflects the
// engine's actual behavior — the KV cache cannot grow past the
// model's context window; old turns get evicted when full.
//
// When PrefixCacheGlobal is true, the prefix is subtracted from the
// per-session estimate — the underlying engine (vLLM-style) keeps the
// prefix's K/V tensors stored once engine-wide, deduplicated across
// sessions, rather than re-storing them per session. The
// cmd/probe-prefix-cache probe is what verifies which world we're in;
// the runner's wiring sets this from the probe result.
type SlidingWindowEstimator struct {
	// BytesPerToken is the per-token KV-cache cost. Defaults to 80,000
	// (a 4B fp16 model). Override for other model sizes/quantizations.
	BytesPerToken uint64
	// ModelResidentBytes is a fixed per-session overhead the model
	// imposes outside the KV cache (small allocations, attention masks,
	// etc.). Conservative default is 64 MB; can be overridden.
	ModelResidentBytes uint64
	// PrefixCacheGlobal subtracts the prefix from each session's
	// estimate when set, on the theory that the engine deduplicates it
	// across sessions. Defaults to false (conservative — assume worst
	// case until proven otherwise via cmd/probe-prefix-cache).
	PrefixCacheGlobal bool
}

// DefaultSlidingWindowEstimator returns an estimator tuned for a 4B
// fp16 model under the conservative assumption that prefix caching is
// per-session (until proven otherwise).
func DefaultSlidingWindowEstimator() SlidingWindowEstimator {
	return SlidingWindowEstimator{
		BytesPerToken:      80_000,
		ModelResidentBytes: 64 * 1024 * 1024,
		PrefixCacheGlobal:  false,
	}
}

// Estimate implements Estimator.
func (e SlidingWindowEstimator) Estimate(s SessionEstimate) uint64 {
	bpt := e.BytesPerToken
	if bpt == 0 {
		bpt = 80_000
	}
	prefix := s.PrefixTokens
	if prefix < 0 {
		prefix = 0
	}
	observed := s.ObservedTokens
	if observed < 0 {
		observed = 0
	}
	active := prefix + observed
	if s.ContextSize > 0 && active > s.ContextSize {
		active = s.ContextSize
	}
	if e.PrefixCacheGlobal && active > prefix {
		// Prefix is stored once engine-wide; this session pays only for
		// the active conversation portion above the prefix.
		active = active - prefix
	} else if e.PrefixCacheGlobal {
		// Active == prefix means the session has only the prefix loaded
		// so far; with global prefix caching, that's zero per-session
		// cost on top of the model resident overhead.
		active = 0
	}
	return e.ModelResidentBytes + uint64(active)*bpt
}
