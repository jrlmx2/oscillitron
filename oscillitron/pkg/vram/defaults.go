// Package-level defaults for the library-managed VRAM path. The
// 2026-05-21 lock specifies that MaxConcurrency=0 means "let the
// library figure it out" — these are the building blocks.
package vram

import "github.com/jrlmx2/oscillitron/pkg/trace"

const (
	// MaxConcurrencyCeiling is the hard safety ceiling on per-wave
	// goroutines under library-managed mode. Prevents pathological
	// goroutine counts even when probed VRAM is abundant.
	MaxConcurrencyCeiling = 8
)

// DefaultVRAMModel returns a conservative ModelSpec usable when the
// operator hasn't supplied real model architecture. Sized for a
// Llama-7B class model — over-counts for smaller models (good: tighter
// throttle) and under-counts for Llama-70B (operator should override
// for those). ModelResidentBytes = 3 GB (catches the phi4-mini OOM
// scenario where 5×3GB = 15GB blew through unified memory).
func DefaultVRAMModel() ModelSpec {
	return ModelSpec{
		Name:               "unknown-model-conservative",
		Layers:             32,
		KVHiddenDim:        1024,
		KVDtypeBytes:       2, // fp16
		ContextSize:        8192,
		PrefixTokens:       512,
		ModelResidentBytes: 3 << 30, // 3 GB
	}
}

// AutoGovernor returns a Governor built with DefaultVRAMModel and the
// MaxConcurrencyCeiling. Probe is vram.Auto() (auto-detects platform).
// On probe failure the returned governor degrades to ceiling-only;
// the runner additionally detects this and downshifts the wave cap
// to 1 (serial) — see runner.probeHealthyOnce.
func AutoGovernor(tracer trace.Tracer) *Governor {
	g, err := NewGovernor(GovernorConfig{
		Model:   DefaultVRAMModel(),
		Ceiling: MaxConcurrencyCeiling,
		Tracer:  tracer,
	})
	if err != nil {
		// Should be impossible given DefaultVRAMModel validates,
		// but panic with context rather than silently returning nil.
		panic("vram: AutoGovernor construction failed: " + err.Error())
	}
	return g
}
