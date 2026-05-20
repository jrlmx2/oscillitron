// CLAUDE GENERATED
package repetition

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func env(v string) session.Envelope {
	return session.Envelope{
		Execute: &session.Execute{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result: session.Payload{Kind: "result", Content: v},
			},
		},
	}
}

func TestRepetitionAbortsOnDuplicateVerdicts(t *testing.T) {
	r := New(5, 3)
	chain := []session.Envelope{
		env("looped on x"),
		env("looped on x"),
		env("different"),
		env("looped on x"),
	}
	got := r.Check(inhibitor.Edge{Path: chain})
	if got.Decision != inhibitor.Abort {
		t.Fatalf("Decision = %v, want Abort", got.Decision)
	}
}

func TestRepetitionTolerantOfDistinctVerdicts(t *testing.T) {
	r := New(5, 3)
	chain := []session.Envelope{env("a"), env("b"), env("c"), env("d"), env("e")}
	if got := r.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}

func TestRepetitionWindowLimitsHistory(t *testing.T) {
	r := New(3, 3)
	// Three "loop" early, then three distinct — the trailing 3 should
	// not trigger.
	chain := []session.Envelope{env("loop"), env("loop"), env("loop"), env("a"), env("b"), env("c")}
	if got := r.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue (repeats outside window)", got.Decision)
	}
}

func TestRepetitionSkipsEmptyVerdicts(t *testing.T) {
	r := New(5, 2)
	chain := []session.Envelope{{}, {}, {}, {}}
	if got := r.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}

func TestRepetitionShortChainContinues(t *testing.T) {
	r := New(5, 3)
	chain := []session.Envelope{env("x"), env("x")}
	if got := r.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue (chain shorter than minRepeats)", got.Decision)
	}
}

func TestRepetitionDefaultsApply(t *testing.T) {
	r := New(0, 0) // window=5, minRepeats=3
	chain := []session.Envelope{env("x"), env("x"), env("x")}
	if got := r.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Abort {
		t.Errorf("Decision = %v, want Abort with default thresholds", got.Decision)
	}
}
