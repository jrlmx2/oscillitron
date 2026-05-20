// CLAUDE GENERATED
// Package adapter defines the contract between the runner and the
// substrate that actually runs the specialist work — a Hermes
// instance (production), a stub (tests/demo), or the frontier
// baseline (comparison harness only).
//
// Under the uniform-node + evaluate/execute model: every AP runs the
// same two steps.
//
//  1. Evaluate — picks the playbook for this AP. Cheap-local-first;
//     the frontier is *not* freely selectable by evaluate.
//  2. Execute — runs the chosen playbook; produces an Execute payload
//     in one of the three Categories (emit_subtree, return_result,
//     verifier_signal).
//
// Per-invocation session lifecycle is the adapter's responsibility.
// A Hermes adapter, for example, spawns or reuses a per-playbook
// session keyed on envelope.ID inside the long-lived Hermes that
// holds the specialist's persistent store.
//
// Both steps return the *envelope* (with Evaluate or Execute filled
// in) so the runner can keep stitching call-tree state onto a single
// record. On error, the runner wraps the failure into an
// ExitInhibited envelope.
package adapter

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Adapter wraps a specialist substrate. Implementations: stub (this
// package's stub subpackage), hermes (lands in Stage 5 of the
// uniform-node refactor), claude (frontier baseline, comparison
// harness only).
type Adapter interface {
	// Name identifies the adapter in logs.
	Name() string
	// Evaluate runs the playbook-pick step. The returned envelope has
	// Evaluate populated. The runner reads env.Evaluate.Playbook and
	// dispatches Execute next.
	Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error)
	// Execute runs the chosen playbook. The returned envelope has
	// Execute populated. Category drives the runner's next move.
	Execute(ctx context.Context, env session.Envelope) (session.Envelope, error)
}
