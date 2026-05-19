// CLAUDE GENERATED
// Package adapter defines the contract between an oscillator and the
// substrate that actually runs the specialist work — a Hermes
// instance (production), a stub (tests/demo), or the frontier baseline
// (comparison harness only).
//
// Under the call-tree model: the adapter takes an envelope IN (the
// invocation) and returns an Output OUT. The oscillator stitches the
// Output back into the envelope and returns it to the tree-walker,
// which decides whether to descend into Output.SubAPs.
//
// Per-invocation session lifecycle is the adapter's responsibility.
// A Hermes adapter, for example, may spin up a fresh Hermes process
// per Call, seeded from the brain function's persistent memory store.
package adapter

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Adapter wraps a specialist substrate. Implementations: stub (this
// package's stub subpackage), hermes (TBD — library-plan §9 step 4),
// claude (frontier baseline, comparison harness only).
type Adapter interface {
	// Name identifies the adapter in logs.
	Name() string
	// Call runs the invocation and returns its Output. On non-nil
	// error, the caller wraps the failure in an ExitInhibited Output.
	Call(ctx context.Context, env session.Envelope) (session.Output, error)
}
