// CLAUDE GENERATED
// Package recomposer combines a parent plan's resolved children into
// a single composed payload under the uniform-node + evaluate/execute
// model.
//
// This file is a placeholder. Stage 4 of the uniform-node refactor
// rewrites the recomposer to read the RecomposeSpec carried on the
// parent plan's emit_subtree payload and dispatch accordingly:
//
//   - RecomposeSequential — Concat-style sequential reduction.
//   - RecomposePairwise   — driven by the compose playbook self-chaining
//     off the parent's scope channel; the recomposer in this package
//     supplies the binary reducer step.
//   - RecomposeNone       — no recomposition; parent simply collects
//     children without merging.
//
// Until Stage 4 lands, Concat is preserved in skeletal form but no
// longer reads the old session.Output flat record (deleted).
package recomposer

import (
	"context"
	"errors"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Recomposer collapses N resolved children into a single composed
// return_result payload. Inputs are the children's return_result
// payloads only; emit_subtree and verifier_signal children are not
// directly composable (the runner handles those separately).
type Recomposer interface {
	Recompose(ctx context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error)
}

// ErrStagePending is returned by all Recomposer implementations while
// Stage 4 of the uniform-node refactor is in flight.
var ErrStagePending = errors.New("recomposer: rewrite pending (Stage 4 of uniform-node refactor)")

// Concat is the v0 sequential recomposer. Skeletal until Stage 4.
type Concat struct {
	Separator string
}

// Recompose implements Recomposer.
func (c Concat) Recompose(_ context.Context, _ session.RecomposeSpec, _ []session.ReturnResultPayload) (session.ReturnResultPayload, error) {
	return session.ReturnResultPayload{}, ErrStagePending
}

var _ Recomposer = Concat{}
