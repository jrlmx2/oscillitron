// CLAUDE GENERATED
// Package adapter defines the contract between an oscillator and the
// substrate that actually runs the specialist work — a Hermes
// instance (production), a stub (tests/demo), or the frontier baseline
// (comparison harness only).
//
// Per library-plan §5.3, with the wrap-Hermes lock applied (§3.2):
// the production-facing Adapter takes an envelope IN and returns an
// Outcome OUT, because the AP IS a summary handoff. The §5.3 v0.1
// draft used a lower-level Request{Prompt}/Response{Text} shape; that
// belongs to a future RawAdapter for direct model calls below the
// Hermes layer, not to the orchestration boundary.
package adapter

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Adapter wraps a specialist substrate. Implementations: stub (this
// package's stub subpackage), hermes (TBD — library-plan §9 step 4),
// claude (frontier baseline, comparison harness only).
type Adapter interface {
	// Name identifies the adapter in logs and routing decisions.
	Name() string
	// Call dispatches the envelope to the substrate and returns the
	// substrate's Outcome. The caller stitches the Outcome back into
	// the envelope and decides what to do next based on its ExitReason.
	Call(ctx context.Context, env session.Envelope) (session.Outcome, error)
}
