// CLAUDE GENERATED
// Package registry binds brain functions to specialist instances. It
// replaces the old pkg/topology graph: under the call-tree model there
// are no edges and no static routing weights, just a lookup from
// BrainFunction to the specialist that handles it.
//
// v0: one specialist per brain function. Multi-specialist selection
// (different model sizes, specialized variants) arrives later.
package registry

import (
	"fmt"

	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Registry is the brain-function → specialist binding table.
type Registry struct {
	bindings map[session.BrainFunction]*oscillator.Oscillator
}

// New constructs an empty registry.
func New() *Registry {
	return &Registry{bindings: map[session.BrainFunction]*oscillator.Oscillator{}}
}

// Register binds an oscillator to a brain function. Overwrites any
// existing binding.
func (r *Registry) Register(bf session.BrainFunction, osc *oscillator.Oscillator) {
	r.bindings[bf] = osc
}

// Lookup returns the oscillator bound to bf. Returns an error when no
// binding exists so the tree-walker catches typos at dispatch time
// rather than silently dropping the AP.
func (r *Registry) Lookup(bf session.BrainFunction) (*oscillator.Oscillator, error) {
	osc, ok := r.bindings[bf]
	if !ok {
		return nil, fmt.Errorf("registry: no specialist registered for brain function %q", bf)
	}
	return osc, nil
}

// Functions returns the set of brain functions with bound specialists.
// Order is not stable.
func (r *Registry) Functions() []session.BrainFunction {
	fns := make([]session.BrainFunction, 0, len(r.bindings))
	for bf := range r.bindings {
		fns = append(fns, bf)
	}
	return fns
}
