// CLAUDE GENERATED
package vram

// SessionEstimate is the input to an Estimator. Fields all relate to
// one inference session — the persona+pool+instructions prefix it
// holds, the conversation tokens accumulated so far in that session,
// and the model's hard context-window limit.
type SessionEstimate struct {
	// Model identifies the model family for trace/log purposes.
	Model string
	// PrefixTokens is the persona + (optional) semantic pool +
	// per-playbook instructions prefix.
	PrefixTokens int
	// ObservedTokens is the conversation tokens accumulated in this
	// session so far. Pass 0 for a brand-new session.
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
// The sliding-window cap reflects engine behavior — the KV cache
// cannot grow past the model's context window; old turns get evicted
// when full.
//
// When PrefixCacheGlobal is true, the prefix is subtracted from the
// per-session estimate — the engine keeps the prefix's K/V tensors
// stored once engine-wide, deduplicated across sessions.
// cmd/probe-prefix-cache verifies which world we're in.
//
// No magic-number defaults. Construct via EstimatorForModel (or
// supply BytesPerToken yourself); Estimate returns 0 if BytesPerToken
// is zero, signaling a misconfigured estimator rather than producing
// fabricated numbers.
type SlidingWindowEstimator struct {
	// BytesPerToken is the per-token KV-cache cost. Required.
	BytesPerToken uint64
	// ModelResidentBytes is a fixed per-session overhead the model
	// imposes outside the KV cache. Optional (default 0). On most
	// substrates the model weights are loaded once, not per session,
	// so per-session overhead is small and often safely ignored.
	ModelResidentBytes uint64
	// PrefixCacheGlobal subtracts the prefix from each session's
	// estimate when set. False (the conservative default) assumes
	// the engine pays for the prefix per session.
	PrefixCacheGlobal bool
}

// EstimatorForModel returns a SlidingWindowEstimator with
// BytesPerToken computed from real transformer architecture. Works
// for both GPU and CPU substrates — the underlying KV-cache math is
// the same; only the available-memory pool differs (which is the
// probe's concern, not the estimator's).
//
// Math:
//
//	BytesPerToken = 2 (K and V) × NumLayers × KVHiddenDim × KVDtypeBytes
//
// KVHiddenDim is num_kv_heads × head_dim. For models without
// grouped-query attention this equals the hidden dimension; for
// Llama-3 / Mistral / Gemma-style GQA it's smaller (commonly 1/4 to
// 1/8 of hidden dim).
//
// Examples (fp16 KV cache, KVDtypeBytes=2):
//
//   - Gemma 2B: 18 layers, num_kv_heads=1, head_dim=256
//     → KVHiddenDim=256, BytesPerToken = 2·18·256·2 = 18,432
//   - Llama 3 8B: 32 layers, num_kv_heads=8, head_dim=128
//     → KVHiddenDim=1024, BytesPerToken = 2·32·1024·2 = 131,072
//   - Mistral 7B: same as Llama 3 8B
//
// KVDtypeBytes:
//   - fp16 / bf16: 2
//   - fp8: 1
//   - q4/q8: typically still 2 effective (storage quantized, compute
//     expands)
//
// Returns a SlidingWindowEstimator with BytesPerToken=0 on
// zero/negative input — no silent fallback to a made-up number.
// Governor.NewGovernor catches that and refuses to start.
func EstimatorForModel(numLayers, kvHiddenDim, kvDtypeBytes int) SlidingWindowEstimator {
	if numLayers <= 0 || kvHiddenDim <= 0 || kvDtypeBytes <= 0 {
		return SlidingWindowEstimator{} // zero BytesPerToken signals misconfiguration
	}
	return SlidingWindowEstimator{
		BytesPerToken: uint64(2 * numLayers * kvHiddenDim * kvDtypeBytes),
	}
}

// Estimate implements Estimator. Returns 0 if BytesPerToken is unset
// — callers (Governor) must validate before consuming.
func (e SlidingWindowEstimator) Estimate(s SessionEstimate) uint64 {
	if e.BytesPerToken == 0 {
		return 0
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
		active = active - prefix
	} else if e.PrefixCacheGlobal {
		active = 0
	}
	return e.ModelResidentBytes + uint64(active)*e.BytesPerToken
}
