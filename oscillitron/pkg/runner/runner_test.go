// CLAUDE GENERATED
package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor/hardcap"
	"github.com/jrlmx2/oscillitron/pkg/oscillator"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/registry"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// staticInhibitor always returns the given verdict — for tests that
// want deterministic abort/continue without depending on signal shape.
type staticInhibitor struct{ v inhibitor.Verdict }

func (s staticInhibitor) Check(inhibitor.Edge) inhibitor.Verdict { return s.v }

func newRegistry(bindings map[session.BrainFunction]*oscillator.Oscillator) *registry.Registry {
	r := registry.New()
	for bf, osc := range bindings {
		r.Register(bf, osc)
	}
	return r
}

func TestLeafRootDispatches(t *testing.T) {
	osc := oscillator.New("r-1", session.BrainReasoning,
		stub.New("reasoner", stub.ModeDone).WithConfidence(0.9), nil)
	cfg := Config{
		Registry:   newRegistry(map[session.BrainFunction]*oscillator.Oscillator{session.BrainReasoning: osc}),
		Recomposer: recomposer.Concat{},
		Inhibitor:  hardcap.New(10),
		Root:       session.NewRoot("root", session.BrainReasoning, "hi", "ok", session.Budget{DepthRemaining: 5}),
	}
	res, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Reason != ReasonSuccess {
		t.Fatalf("Reason = %q, want success", res.Reason)
	}
	if res.Root.Output == nil || res.Root.Output.Confidence != 0.9 {
		t.Fatalf("root output not populated correctly: %+v", res.Root.Output)
	}
}

func TestParentWithSubAPsRecomposesChildren(t *testing.T) {
	// Parent (planner) emits two SubAPs (one critic, one composer).
	// Tree-walker dispatches them and recomposer joins their content.
	subSeeds := []session.SubAPSeed{
		{BrainFunction: session.BrainCritic, Input: session.Input{Content: "check this"}, OutputSchema: "ok"},
		{BrainFunction: session.BrainComposition, Input: session.Input{Content: "write that"}, OutputSchema: "prose"},
	}
	planner := oscillator.New("p", session.BrainPlanning,
		stub.New("planner", stub.ModeDone).WithConfidence(0.8).WithSubAPs(subSeeds...), nil)
	critic := oscillator.New("c", session.BrainCritic,
		stub.New("critic", stub.ModeDone).WithConfidence(0.7), nil)
	composer := oscillator.New("co", session.BrainComposition,
		stub.New("composer", stub.ModeDone).WithConfidence(0.6), nil)

	cfg := Config{
		Registry: newRegistry(map[session.BrainFunction]*oscillator.Oscillator{
			session.BrainPlanning:    planner,
			session.BrainCritic:      critic,
			session.BrainComposition: composer,
		}),
		Recomposer: recomposer.Concat{Separator: " | "},
		Inhibitor:  hardcap.New(10),
		Root:       session.NewRoot("root", session.BrainPlanning, "plan it", "plan", session.Budget{DepthRemaining: 5}),
	}
	res, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Reason != ReasonSuccess {
		t.Fatalf("Reason = %q, want success", res.Reason)
	}
	if res.Root.Output == nil {
		t.Fatal("composed output missing")
	}
	// Composed content should include both children's content.
	content := res.Root.Output.Content
	if !strings.Contains(content, "critic") || !strings.Contains(content, "composer") {
		t.Errorf("composed content missing child contributions: %q", content)
	}
	// Confidence = min across parent+children = 0.6 (composer).
	if res.Root.Output.Confidence != 0.6 {
		t.Errorf("Confidence = %v, want 0.6 (weakest link)", res.Root.Output.Confidence)
	}
}

func TestInhibitorAbortPropagates(t *testing.T) {
	// Inhibition is an edge property — root has no incoming edge, so
	// for the inhibitor to fire at all the root must emit a SubAP. The
	// edge-level abort on the child then propagates back to the root.
	subSeeds := []session.SubAPSeed{
		{BrainFunction: session.BrainCritic, Input: session.Input{Content: "check"}, OutputSchema: "ok"},
	}
	parent := oscillator.New("p", session.BrainReasoning,
		stub.New("reasoner", stub.ModeDone).WithSubAPs(subSeeds...), nil)
	critic := oscillator.New("c", session.BrainCritic,
		stub.New("critic", stub.ModeDone), nil)
	cfg := Config{
		Registry: newRegistry(map[session.BrainFunction]*oscillator.Oscillator{
			session.BrainReasoning: parent,
			session.BrainCritic:    critic,
		}),
		Recomposer: recomposer.Concat{},
		Inhibitor:  staticInhibitor{v: inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "test abort"}},
		Root:       session.NewRoot("root", session.BrainReasoning, "x", "y", session.Budget{DepthRemaining: 5}),
	}
	res, _ := Run(context.Background(), cfg)
	if res.Reason != ReasonInhibitorAbort {
		t.Fatalf("Reason = %q, want inhibitor_abort", res.Reason)
	}
	if !res.Root.IsInhibited() {
		t.Error("root should be inhibited")
	}
}

func TestInhibitorSkippedAtRoot(t *testing.T) {
	// A leaf root must dispatch successfully even when the inhibitor
	// would always abort — the root has no incoming edge to check.
	osc := oscillator.New("r", session.BrainReasoning,
		stub.New("reasoner", stub.ModeDone).WithConfidence(0.5), nil)
	cfg := Config{
		Registry:   newRegistry(map[session.BrainFunction]*oscillator.Oscillator{session.BrainReasoning: osc}),
		Recomposer: recomposer.Concat{},
		Inhibitor:  staticInhibitor{v: inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "should not fire"}},
		Root:       session.NewRoot("root", session.BrainReasoning, "x", "y", session.Budget{DepthRemaining: 5}),
	}
	res, _ := Run(context.Background(), cfg)
	if res.Reason != ReasonSuccess {
		t.Fatalf("Reason = %q, want success (inhibitor should not fire at root)", res.Reason)
	}
}

func TestUnregisteredBrainFunctionFailsValidation(t *testing.T) {
	cfg := Config{
		Registry:   registry.New(),
		Recomposer: recomposer.Concat{},
		Inhibitor:  hardcap.New(10),
		Root:       session.NewRoot("root", session.BrainReasoning, "x", "y", session.Budget{}),
	}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Fatal("expected validation error for unregistered brain function")
	}
}

func TestInhibitedChildPropagatesToParent(t *testing.T) {
	// Parent emits one SubAP to a brain function whose adapter errors;
	// child returns inhibited; parent inhibits too.
	subSeeds := []session.SubAPSeed{
		{BrainFunction: session.BrainCritic, Input: session.Input{Content: "check"}, OutputSchema: "ok"},
	}
	planner := oscillator.New("p", session.BrainPlanning,
		stub.New("planner", stub.ModeDone).WithSubAPs(subSeeds...), nil)
	brokenCritic := oscillator.New("c", session.BrainCritic,
		stub.New("critic", stub.ModeError), nil)

	cfg := Config{
		Registry: newRegistry(map[session.BrainFunction]*oscillator.Oscillator{
			session.BrainPlanning: planner,
			session.BrainCritic:   brokenCritic,
		}),
		Recomposer: recomposer.Concat{},
		Inhibitor:  hardcap.New(10),
		Root:       session.NewRoot("root", session.BrainPlanning, "plan", "ok", session.Budget{DepthRemaining: 5}),
	}
	res, _ := Run(context.Background(), cfg)
	if res.Reason != ReasonInhibitorAbort {
		t.Fatalf("Reason = %q, want inhibitor_abort", res.Reason)
	}
	if !res.Root.IsInhibited() {
		t.Error("root should be inhibited when child is")
	}
}

func TestDepthCapPreventsRunaway(t *testing.T) {
	// A reasoner that keeps emitting a sub-AP to itself — depth cap
	// must catch it before stack overflow.
	selfSeed := []session.SubAPSeed{
		{BrainFunction: session.BrainReasoning, Input: session.Input{Content: "again"}, OutputSchema: "ok"},
	}
	osc := oscillator.New("r", session.BrainReasoning,
		stub.New("reasoner", stub.ModeDone).WithSubAPs(selfSeed...), nil)
	cfg := Config{
		Registry:   newRegistry(map[session.BrainFunction]*oscillator.Oscillator{session.BrainReasoning: osc}),
		Recomposer: recomposer.Concat{},
		Inhibitor:  hardcap.New(1000), // disable for this test
		Root:       session.NewRoot("root", session.BrainReasoning, "loop", "ok", session.Budget{DepthRemaining: 100}),
		MaxDepth:   4,
	}
	res, _ := Run(context.Background(), cfg)
	// Depth-exhausted leaf becomes inhibited and propagates up.
	if !res.Root.IsInhibited() {
		t.Fatalf("expected root inhibited due to depth cap; got %+v", res.Root.Output)
	}
}
