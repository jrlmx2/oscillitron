// CLAUDE GENERATED
package vram

import "testing"

func TestSlidingWindowEstimator_GrowsThenCapsAtContext(t *testing.T) {
	est := SlidingWindowEstimator{BytesPerToken: 80_000, ModelResidentBytes: 0}
	// Below context: linear growth.
	got := est.Estimate(SessionEstimate{PrefixTokens: 1000, ObservedTokens: 500, ContextSize: 4096})
	if got != 1500*80_000 {
		t.Errorf("below-context: got %d, want %d", got, 1500*80_000)
	}
	// At context: capped.
	got = est.Estimate(SessionEstimate{PrefixTokens: 2000, ObservedTokens: 2096, ContextSize: 4096})
	if got != 4096*80_000 {
		t.Errorf("at-context: got %d, want %d", got, 4096*80_000)
	}
	// Above context: still capped (sliding-window eviction kicks in).
	got = est.Estimate(SessionEstimate{PrefixTokens: 2000, ObservedTokens: 99999, ContextSize: 4096})
	if got != 4096*80_000 {
		t.Errorf("above-context: got %d, want %d (sliding-window cap)", got, 4096*80_000)
	}
}

func TestSlidingWindowEstimator_PrefixCacheGlobalSubtractsPrefix(t *testing.T) {
	est := SlidingWindowEstimator{
		BytesPerToken:      80_000,
		ModelResidentBytes: 0,
		PrefixCacheGlobal:  true,
	}
	// Active = prefix + observed = 1500; with global prefix cache,
	// session pays only for observed = 500.
	got := est.Estimate(SessionEstimate{PrefixTokens: 1000, ObservedTokens: 500, ContextSize: 4096})
	if got != 500*80_000 {
		t.Errorf("prefix-cache-global: got %d, want %d (only observed)", got, 500*80_000)
	}
	// Brand-new session (observed=0): nothing on top of model resident.
	got = est.Estimate(SessionEstimate{PrefixTokens: 1000, ObservedTokens: 0, ContextSize: 4096})
	if got != 0 {
		t.Errorf("prefix-cache-global new-session: got %d, want 0", got)
	}
}

func TestSlidingWindowEstimator_AddsModelResident(t *testing.T) {
	est := SlidingWindowEstimator{
		BytesPerToken:      80_000,
		ModelResidentBytes: 64 * 1024 * 1024,
	}
	got := est.Estimate(SessionEstimate{PrefixTokens: 100, ObservedTokens: 0, ContextSize: 4096})
	want := uint64(64*1024*1024 + 100*80_000)
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestSlidingWindowEstimator_NoContextSizeMeansUnbounded(t *testing.T) {
	// ContextSize == 0 means "don't cap" — useful for callers that
	// don't know the model's context window.
	est := SlidingWindowEstimator{BytesPerToken: 80_000}
	got := est.Estimate(SessionEstimate{PrefixTokens: 100_000, ObservedTokens: 0, ContextSize: 0})
	if got != 100_000*80_000 {
		t.Errorf("got %d, want %d (no cap)", got, 100_000*80_000)
	}
}

func TestSlidingWindowEstimator_NegativeTokensClamped(t *testing.T) {
	est := SlidingWindowEstimator{BytesPerToken: 80_000}
	got := est.Estimate(SessionEstimate{PrefixTokens: -100, ObservedTokens: -50, ContextSize: 4096})
	if got != 0 {
		t.Errorf("got %d, want 0 (clamped)", got)
	}
}

func TestDefaultSlidingWindowEstimator_Values(t *testing.T) {
	e := DefaultSlidingWindowEstimator()
	if e.BytesPerToken != 80_000 {
		t.Errorf("BytesPerToken = %d, want 80000 (4B fp16)", e.BytesPerToken)
	}
	if e.ModelResidentBytes != 64*1024*1024 {
		t.Errorf("ModelResidentBytes = %d, want 64 MiB", e.ModelResidentBytes)
	}
	if e.PrefixCacheGlobal {
		t.Errorf("default should be conservative: PrefixCacheGlobal = true")
	}
}
