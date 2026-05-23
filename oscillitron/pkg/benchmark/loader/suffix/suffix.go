// CLAUDE GENERATED
// Package suffix is a benchmark.Loader decorator that appends a
// fixed Suffix to every Case.Prompt as cases are loaded.
//
// Use case: format recipes. Many benchmarks need the model to end
// its response in a parseable shape (e.g., "Answer: X" for MCQ).
// Without a hint, smaller substrates often reason correctly but
// don't commit in the right format, so the extractor whiffs and
// the case is recorded as fail. A one-line suffix instruction
// directly addresses this leakage without touching the underlying
// dataset.
//
// Decorator (not modification) so the underlying loader stays pure
// and reusable. Pass-through when Suffix is empty so wiring it
// always is safe.
package suffix

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// Loader wraps an inner benchmark.Loader and appends Suffix to every
// Case.Prompt as cases load. Inner errors propagate verbatim.
//
// Loaded Cases are returned in the inner loader's order; only the
// Prompt field is modified. The Name passes through (so reports
// still show "gpqa-diamond" rather than "gpqa-diamond+suffix" — the
// trace events log the suffix separately when operators care).
type Loader struct {
	// Inner is the loader whose cases will be augmented. Required.
	Inner benchmark.Loader
	// Suffix is the text appended to every Case.Prompt. When empty,
	// Load is a pure pass-through (no mutation, no allocation).
	Suffix string
}

// Name implements benchmark.Loader.
func (l Loader) Name() string {
	if l.Inner == nil {
		return "suffix(<nil>)"
	}
	return l.Inner.Name()
}

// Load implements benchmark.Loader. Loads the inner cases and
// appends Suffix to each Case.Prompt. When Suffix is empty,
// returns the inner result unchanged.
func (l Loader) Load(ctx context.Context) ([]benchmark.Case, error) {
	if l.Inner == nil {
		return nil, errInner
	}
	cases, err := l.Inner.Load(ctx)
	if err != nil {
		return cases, err
	}
	if l.Suffix == "" {
		return cases, nil
	}
	for i := range cases {
		cases[i].Prompt += l.Suffix
	}
	return cases, nil
}

// errInner is returned when Load is called with no Inner set.
// Defined as a sentinel so callers can errors.Is if they want.
var errInner = errorInner("suffix: Inner is required")

type errorInner string

func (e errorInner) Error() string { return string(e) }

// Compile-time check.
var _ benchmark.Loader = Loader{}
