// CLAUDE GENERATED
package composite

import (
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
)

type fake struct{ v inhibitor.Verdict }

func (f fake) Check(inhibitor.Edge) inhibitor.Verdict { return f.v }

func TestCompositeContinuesWhenAllContinue(t *testing.T) {
	c := New(fake{inhibitor.Verdict{Decision: inhibitor.Continue}}, fake{inhibitor.Verdict{Decision: inhibitor.Continue}})
	if got := c.Check(inhibitor.Edge{}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}

func TestCompositeAbortBeatsRestart(t *testing.T) {
	c := New(
		fake{inhibitor.Verdict{Decision: inhibitor.Restart, Reason: "drift", Checkpoint: 2}},
		fake{inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "cap"}},
		fake{inhibitor.Verdict{Decision: inhibitor.Continue}},
	)
	got := c.Check(inhibitor.Edge{})
	if got.Decision != inhibitor.Abort {
		t.Fatalf("Decision = %v, want Abort", got.Decision)
	}
	if !strings.Contains(got.Reason, "cap") {
		t.Errorf("reason missing abort source: %q", got.Reason)
	}
}

func TestCompositeRestartPicksEarliestCheckpoint(t *testing.T) {
	c := New(
		fake{inhibitor.Verdict{Decision: inhibitor.Restart, Reason: "a", Checkpoint: 5}},
		fake{inhibitor.Verdict{Decision: inhibitor.Restart, Reason: "b", Checkpoint: 2}},
		fake{inhibitor.Verdict{Decision: inhibitor.Restart, Reason: "c", Checkpoint: 7}},
	)
	got := c.Check(inhibitor.Edge{})
	if got.Decision != inhibitor.Restart {
		t.Fatalf("Decision = %v, want Restart", got.Decision)
	}
	if got.Checkpoint != 2 {
		t.Errorf("Checkpoint = %d, want 2 (earliest)", got.Checkpoint)
	}
	if !strings.Contains(got.Reason, "a") || !strings.Contains(got.Reason, "b") || !strings.Contains(got.Reason, "c") {
		t.Errorf("reason should concatenate all restart reasons: %q", got.Reason)
	}
}

func TestCompositeAggregatesMultipleAbortReasons(t *testing.T) {
	c := New(
		fake{inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "floor"}},
		fake{inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "cap"}},
	)
	got := c.Check(inhibitor.Edge{})
	if got.Decision != inhibitor.Abort {
		t.Fatalf("Decision = %v, want Abort", got.Decision)
	}
	if !strings.Contains(got.Reason, "floor") || !strings.Contains(got.Reason, "cap") {
		t.Errorf("reason should include both: %q", got.Reason)
	}
}

func TestCompositeEmptyMembersContinues(t *testing.T) {
	c := New()
	if got := c.Check(inhibitor.Edge{}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}
