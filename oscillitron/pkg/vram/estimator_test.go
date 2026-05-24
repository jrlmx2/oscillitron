package vram

import "testing"

func TestSlidingWindowEstimator_ReturnsZeroOnUnsetBytesPerToken(t *testing.T) {
	est := SlidingWindowEstimator{} // unset → zero
	got := est.Estimate(SessionEstimate{PrefixTokens: 100, ContextSize: 4096})
	if got != 0 {
		t.Errorf("Estimate with zero BytesPerToken = %d, want 0 (signals misconfiguration)", got)
	}
}

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

func TestEstimatorForModel_KnownArchitectures(t *testing.T) {
	cases := []struct {
		name         string
		layers       int
		kvHiddenDim  int
		dtypeBytes   int
		wantBytesPer uint64
	}{
		// Gemma 2B: 18 layers, num_kv_heads=1, head_dim=256
		{"gemma-2b-fp16", 18, 256, 2, 2 * 18 * 256 * 2},
		// Llama 3 8B: 32 layers, num_kv_heads=8, head_dim=128 → KVHidden=1024
		{"llama3-8b-fp16", 32, 1024, 2, 2 * 32 * 1024 * 2},
		// fp8 KV cache halves the per-token cost
		{"llama3-8b-fp8", 32, 1024, 1, 2 * 32 * 1024 * 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			est := EstimatorForModel(c.layers, c.kvHiddenDim, c.dtypeBytes)
			if est.BytesPerToken != c.wantBytesPer {
				t.Errorf("BytesPerToken = %d, want %d", est.BytesPerToken, c.wantBytesPer)
			}
		})
	}
}

func TestEstimatorForModel_ZeroesOnBadInput(t *testing.T) {
	// No magic-number fallback. Zero or negative inputs produce a
	// zero-BytesPerToken estimator, which Estimate then returns 0
	// for — the Governor catches that at NewGovernor and refuses
	// to start, surfacing misconfiguration as a config-time error
	// rather than fabricated runtime numbers.
	for _, tc := range []struct {
		name        string
		layers      int
		kvHiddenDim int
		dtypeBytes  int
	}{
		{"zero layers", 0, 256, 2},
		{"zero kv hidden", 32, 0, 2},
		{"zero dtype", 32, 1024, 0},
		{"negative dtype", 32, 1024, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			est := EstimatorForModel(tc.layers, tc.kvHiddenDim, tc.dtypeBytes)
			if est.BytesPerToken != 0 {
				t.Errorf("BytesPerToken = %d, want 0 (no magic fallback)", est.BytesPerToken)
			}
		})
	}
}

func TestEstimatorForModel_PluggableIntoEstimate(t *testing.T) {
	est := EstimatorForModel(32, 1024, 2)
	bytes := est.Estimate(SessionEstimate{
		PrefixTokens:   2000,
		ObservedTokens: 500,
		ContextSize:    8192,
	})
	if bytes == 0 {
		t.Fatal("Estimate returned 0 bytes")
	}
	// 2500 active tokens × (2 × 32 × 1024 × 2) bytes = 327.7 MiB
	want := uint64(2500 * 2 * 32 * 1024 * 2)
	if bytes != want {
		t.Errorf("Estimate = %d, want %d", bytes, want)
	}
}
