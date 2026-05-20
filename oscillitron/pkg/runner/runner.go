// CLAUDE GENERATED
// Package runner walks the AP call tree under the uniform-node +
// evaluate/execute model.
//
// This file is a placeholder. Stage 3 of the uniform-node refactor
// (parent CLAUDE.md "Architecture", scratch/design-notes.md "JSON
// envelope sketch") rewrites the runner to:
//
//   - Call adapter.Evaluate then adapter.Execute on every AP.
//   - Branch on Execute.Category:
//   - emit_subtree → construct child envelopes, publish into the
//     parent's scope channel, dispatch in randomized order.
//   - return_result → bubble up.
//   - verifier_signal → feed to runtime policy (verifier-policy
//     happiness signal), not to the next AP.
//   - Invoke the inhibitor on every parent→child edge after the child
//     resolves. Root has no incoming edge and is never checked.
//   - Enforce MaxDepth and per-AP Budget.
//
// Until Stage 3 lands, Run panics with a clear stage-pending message.
package runner

import (
	"context"
	"errors"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// ErrStagePending is returned by Run while Stage 3 of the
// uniform-node refactor is in flight.
var ErrStagePending = errors.New("runner: rewrite pending (Stage 3 of uniform-node refactor)")

// Config bundles the tree-walker's dependencies. Shape is intentionally
// preserved across the refactor so callers (cmd/oscillitron, tests)
// don't churn twice.
type Config struct {
	Adapter    adapter.Adapter
	Inhibitor  inhibitor.Inhibitor
	Recomposer recomposer.Recomposer
	Tracer     trace.Tracer
	MaxDepth   int
}

// Run walks the call tree from root. Returns the resolved envelope.
// Stage 3 of the uniform-node refactor will implement this; for now
// it returns ErrStagePending so downstream callers see a clear
// failure rather than silently doing nothing.
func Run(ctx context.Context, cfg Config, root session.Envelope) (session.Envelope, error) {
	return root, ErrStagePending
}
