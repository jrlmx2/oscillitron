// CLAUDE GENERATED
package confidence

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func env(c float64) session.Envelope {
	return session.Envelope{Output: &session.Output{Confidence: c}}
}

func TestFloorAborts(t *testing.T) {
	c := New(0.4, 0, 0)
	got := c.Check([]session.Envelope{env(0.8), env(0.6), env(0.3)})
	if got.Decision != inhibitor.Abort {
		t.Fatalf("Decision = %v, want Abort", got.Decision)
	}
}

func TestFloorTolerates(t *testing.T) {
	c := New(0.4, 0, 0)
	got := c.Check([]session.Envelope{env(0.8), env(0.6), env(0.5)})
	if got.Decision != inhibitor.Continue {
		t.Fatalf("Decision = %v, want Continue", got.Decision)
	}
}

func TestDropTriggersRestart(t *testing.T) {
	c := New(0, 0.3, 3)
	got := c.Check([]session.Envelope{env(0.9), env(0.8), env(0.5)})
	if got.Decision != inhibitor.Restart {
		t.Fatalf("Decision = %v, want Restart", got.Decision)
	}
	if got.Checkpoint != 0 {
		t.Errorf("Checkpoint = %d, want 0 (window covers full chain)", got.Checkpoint)
	}
}

func TestDropOnlyCountsPeakThenValley(t *testing.T) {
	c := New(0, 0.3, 3)
	// Rising trend — must not trigger.
	got := c.Check([]session.Envelope{env(0.4), env(0.6), env(0.9)})
	if got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue for rising trend", got.Decision)
	}
}

func TestDropWindowLimitsHistory(t *testing.T) {
	c := New(0, 0.3, 2)
	// 0.9 → 0.4 outside window; 0.7 → 0.6 inside window (drop=0.1).
	got := c.Check([]session.Envelope{env(0.9), env(0.4), env(0.7), env(0.6)})
	if got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue (drop outside window)", got.Decision)
	}
}

func TestSkipsEnvelopesWithoutOutcome(t *testing.T) {
	c := New(0.4, 0, 0)
	chain := []session.Envelope{{}, {}, env(0.5)}
	if got := c.Check(chain); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}

func TestEmptyChainContinues(t *testing.T) {
	c := New(0.4, 0.3, 3)
	if got := c.Check(nil); got.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", got.Decision)
	}
}
