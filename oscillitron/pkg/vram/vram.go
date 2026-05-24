// Package vram measures available GPU/accelerator memory across
// platforms and estimates per-Hermes-session VRAM footprint for the
// runner's dynamic concurrency cap.
//
// Two surfaces:
//
//   - Probe: reads how much memory is currently free on the
//     accelerator (or unified memory, or system RAM as a last-resort
//     fallback). Multi-platform — auto-detects what's available and
//     uses the most explicit signal it can find.
//
//   - Estimator: predicts how much VRAM one Hermes session will hold
//     at steady state. Bounded by the model's context window
//     (sliding-window semantics — KV cache can't grow past context).
//
// The runner divides Probe.Available() by Estimator.Estimate() to
// derive a concurrency cap each dispatch wave.
//
// Coverage limitations are documented in references/vram-platform-coverage.md
// — read that before extending platform support.
package vram

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Source identifies which platform-specific probe produced a reading.
// Useful for trace records, debugging, and operator visibility.
type Source string

const (
	SourceOverride    Source = "override"
	SourceNvidiaSMI   Source = "nvidia-smi"
	SourceROCmSMI     Source = "rocm-smi"
	SourceDarwin      Source = "darwin-unified"
	SourceLinuxDRM    Source = "linux-drm-sysfs"
	SourceProcMeminfo Source = "proc-meminfo"
	SourceNone        Source = "none"
)

// Report is one VRAM probe reading.
type Report struct {
	// Source identifies which probe produced this reading.
	Source Source
	// AvailableBytes is the total free memory across all detected GPUs
	// (or unified memory). When multiple GPUs are detected, this is the
	// sum; per-GPU detail is in GPUs.
	AvailableBytes uint64
	// TotalBytes is the total memory across all detected GPUs.
	TotalBytes uint64
	// GPUs is the per-device breakdown when the source supports it.
	// Empty for sources that report only aggregates (proc-meminfo,
	// override).
	GPUs []GPUReading
}

// GPUReading is one device's contribution to a Report.
type GPUReading struct {
	Index          int
	Name           string
	AvailableBytes uint64
	TotalBytes     uint64
}

// Probe is the read-only VRAM-detection interface. Implementations are
// expected to be cheap-to-call (read a file, run a subprocess, parse
// output); the runner re-reads periodically rather than per dispatch.
type Probe interface {
	// Name identifies the probe in trace records.
	Name() string
	// Probe returns a Report. Returns an error if the underlying source
	// is unavailable (e.g., nvidia-smi not installed). Callers should
	// treat an error as "this probe couldn't read" — not as a fatal
	// runtime condition.
	Probe(ctx context.Context) (Report, error)
}

// ErrNoSource is returned by AutoChain.Probe when no registered probe
// produced a successful reading. The runner falls back to its static
// concurrency cap when this happens.
var ErrNoSource = errors.New("vram: no source available")

// AutoChain tries a sequence of probes and returns the first
// successful reading. Probes are ordered by priority (most explicit
// signal first); the operator override always wins when set.
type AutoChain struct {
	Probes []Probe
}

// Name implements Probe.
func (c AutoChain) Name() string { return "auto" }

// Probe tries each registered probe in order and returns the first
// successful reading.
func (c AutoChain) Probe(ctx context.Context) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if len(c.Probes) == 0 {
		return Report{Source: SourceNone}, ErrNoSource
	}
	var firstErr error
	for _, p := range c.Probes {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		report, err := p.Probe(ctx)
		if err == nil {
			return report, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return Report{Source: SourceNone}, fmt.Errorf("%w: tried %d probes, first error: %v", ErrNoSource, len(c.Probes), firstErr)
}

// --- registry: each platform-specific file registers its probes
// in init() so AutoChain picks them up automatically. The override
// probe is registered when SetOverride is called.

type registryEntry struct {
	priority int
	make     func() Probe
}

var (
	registryMu sync.Mutex
	registry   []registryEntry
	override   *overrideProbe
)

// Register adds a Probe constructor to the auto-detection chain. The
// priority is the sort key — lower numbers run first. The override
// probe always runs at priority 0 (lowest) when set; standard probes
// use 100 (nvidia-smi), 200 (rocm-smi), 300 (darwin-unified), 400
// (linux-drm-sysfs), 500 (proc-meminfo). Custom probes can interleave
// by picking their own number.
func Register(priority int, make func() Probe) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = append(registry, registryEntry{priority: priority, make: make})
}

// SetOverride pins the auto-detector to return availableBytes /
// totalBytes regardless of what the real probes find. Use for operator
// overrides (--vram-budget), CI, or any setup where auto-detect is
// unreliable. Passing zero clears the override.
//
// The same totalBytes is reported as both available and total when
// only an available value is supplied — callers using totalBytes
// for percent-free calculations should treat it as the budget ceiling.
func SetOverride(availableBytes uint64) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if availableBytes == 0 {
		override = nil
		return
	}
	override = &overrideProbe{available: availableBytes}
}

// Auto returns an AutoChain containing every registered Probe in
// priority order, with the operator override (if set) at the front.
func Auto() Probe {
	registryMu.Lock()
	defer registryMu.Unlock()
	probes := make([]Probe, 0, len(registry)+1)
	if override != nil {
		probes = append(probes, override)
	}
	sorted := append([]registryEntry(nil), registry...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].priority < sorted[j].priority
	})
	for _, e := range sorted {
		probes = append(probes, e.make())
	}
	return AutoChain{Probes: probes}
}

// --- override probe (always available, no platform requirements) ---

type overrideProbe struct {
	available uint64
}

func (p *overrideProbe) Name() string { return string(SourceOverride) }

func (p *overrideProbe) Probe(_ context.Context) (Report, error) {
	return Report{
		Source:         SourceOverride,
		AvailableBytes: p.available,
		TotalBytes:     p.available,
	}, nil
}
