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
	"time"

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

// MinTimeoutAdvisory is an OPTIONAL interface adapters may implement
// to declare a minimum per-Call ctx deadline they need to operate.
// The runner type-asserts at startup; if a chain-level timeout is set
// shorter than what any adapter advertises, the runner emits a tracer
// warning so the user knows their chain timeout is too aggressive
// for the configured backend. This is advisory only — adapters that
// care enforce hard refusal at Call entry independently (see
// hermes.Adapter.MinCallTimeout / prompt_timeout_too_short signal).
//
// Adapters that don't care don't implement this; the runner skips
// the check by type assertion.
type MinTimeoutAdvisory interface {
	MinCallTimeout() time.Duration
}
