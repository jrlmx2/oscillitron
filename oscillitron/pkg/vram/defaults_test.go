// CLAUDE GENERATED
package vram

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/trace"
)

func TestDefaultVRAMModel_Validates(t *testing.T) {
	// The library-managed path constructs a governor from
	// DefaultVRAMModel — if that ModelSpec fails Validate, the entire
	// auto-wire path collapses with a panic in AutoGovernor. Pin the
	// invariant.
	spec := DefaultVRAMModel()
	if err := spec.Validate(); err != nil {
		t.Fatalf("DefaultVRAMModel must validate, got: %v", err)
	}
	if spec.ModelResidentBytes == 0 {
		t.Errorf("DefaultVRAMModel should set ModelResidentBytes (catches the small-model OOM scenario); got 0")
	}
	g, err := NewGovernor(GovernorConfig{Model: spec})
	if err != nil {
		t.Fatalf("NewGovernor(DefaultVRAMModel): %v", err)
	}
	if g == nil {
		t.Fatal("NewGovernor returned nil governor without error")
	}
}

func TestAutoGovernor_Constructs(t *testing.T) {
	// Basic sanity: AutoGovernor returns a non-nil governor whose
	// ceiling matches MaxConcurrencyCeiling and whose ModelSpec is
	// DefaultVRAMModel.
	g := AutoGovernor(trace.Discard{})
	if g == nil {
		t.Fatal("AutoGovernor returned nil")
	}
	if got, want := g.Ceiling(), MaxConcurrencyCeiling; got != want {
		t.Errorf("AutoGovernor.Ceiling = %d, want %d", got, want)
	}
	if got := g.Model().Name; got != DefaultVRAMModel().Name {
		t.Errorf("AutoGovernor.Model.Name = %q, want %q", got, DefaultVRAMModel().Name)
	}
	if got := g.Model().ModelResidentBytes; got != DefaultVRAMModel().ModelResidentBytes {
		t.Errorf("AutoGovernor.Model.ModelResidentBytes = %d, want %d",
			got, DefaultVRAMModel().ModelResidentBytes)
	}
}

func TestAutoGovernor_NilTracer(t *testing.T) {
	// nil tracer must not panic — NewGovernor swaps in trace.Discard{}.
	g := AutoGovernor(nil)
	if g == nil {
		t.Fatal("AutoGovernor(nil tracer) returned nil")
	}
}

func TestMaxConcurrencyCeiling_IsEight(t *testing.T) {
	// The lock specifies a hard safety ceiling of 8. Pin it so changes
	// here are deliberate and surface in code review.
	if MaxConcurrencyCeiling != 8 {
		t.Errorf("MaxConcurrencyCeiling = %d, want 8 (per 2026-05-21 lock)",
			MaxConcurrencyCeiling)
	}
}
