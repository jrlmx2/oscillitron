// CLAUDE GENERATED
package contradictions

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func env(contras ...string) session.Envelope {
	return session.Envelope{Output: &session.Output{Contradictions: contras}}
}

func TestSpikeAborts(t *testing.T) {
	c := New(3, 0)
	chain := []session.Envelope{env("a"), env("b", "c", "d")}
	if got := c.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Abort {
		t.Errorf("Decision = %v, want Abort on spike", got.Decision)
	}
}

func TestMaxTotalAborts(t *testing.T) {
	c := New(0, 5)
	chain := []session.Envelope{env("a", "b"), env("c"), env("d", "e")}
	if got := c.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Abort {
		t.Errorf("Decision = %v, want Abort on cumulative", got.Decision)
	}
}

func TestBelowThresholdsContinues(t *testing.T) {
	c := New(3, 5)
	chain := []session.Envelope{env("a"), env("b"), env("c")}
	if got := c.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}

func TestZeroThresholdsDisable(t *testing.T) {
	c := New(0, 0)
	chain := []session.Envelope{env("a", "b", "c", "d", "e", "f", "g")}
	if got := c.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue with both thresholds disabled", got.Decision)
	}
}

func TestSkipsNilOutcome(t *testing.T) {
	c := New(2, 3)
	chain := []session.Envelope{{}, {}, env("only")}
	if got := c.Check(inhibitor.Edge{Path: chain}); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}
