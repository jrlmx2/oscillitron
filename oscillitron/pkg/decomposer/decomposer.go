// CLAUDE GENERATED
// Package decomposer turns a single prompt into the ordered set of
// sessions the orchestrator will dispatch. Per library-plan §5.5:
// the manual implementation reads a workflow file and returns
// pre-defined sessions; an LLM-driven implementation (Phase 3) calls
// a model with a decomposition scaffold.
//
// This package ships the interface and a passthrough implementation
// — useful as a baseline and as the eval-harness control arm (single
// session, no decomposition). Real decomposer impls live in
// subpackages.
package decomposer

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Decomposer produces the session sequence for a prompt.
type Decomposer interface {
	Decompose(ctx context.Context, prompt string) ([]session.Envelope, error)
}

// Passthrough returns a single envelope wrapping the prompt
// unchanged. Acts as the no-decomposition control arm.
type Passthrough struct {
	// Classification stamped on the produced envelope. Defaults to
	// classification.Internal when zero.
	Classification classification.Level
	// Objective is the human-readable task label. Defaults to
	// "passthrough" when empty.
	Objective string
}

// Decompose implements Decomposer.
func (p Passthrough) Decompose(_ context.Context, prompt string) ([]session.Envelope, error) {
	level := p.Classification
	if level == "" {
		level = classification.Internal
	}
	obj := p.Objective
	if obj == "" {
		obj = "passthrough"
	}
	return []session.Envelope{{
		Type:           session.TypeAnalyze,
		Objective:      obj,
		Classification: level,
		Input: session.Input{
			Type:    "prompt",
			Content: prompt,
		},
	}}, nil
}

var _ Decomposer = Passthrough{}
