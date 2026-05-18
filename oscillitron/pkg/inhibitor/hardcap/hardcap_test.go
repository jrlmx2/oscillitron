// CLAUDE GENERATED
package hardcap

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestHardcapAllowsShortChains(t *testing.T) {
	h := New(5)
	chain := []session.Envelope{{ID: "a"}, {ID: "b"}}
	v := h.Check(chain)
	if v.Decision != inhibitor.Continue {
		t.Errorf("Decision = %v, want Continue", v.Decision)
	}
}

func TestHardcapAbortsAtThreshold(t *testing.T) {
	h := New(3)
	chain := []session.Envelope{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	v := h.Check(chain)
	if v.Decision != inhibitor.Abort {
		t.Errorf("Decision = %v, want Abort", v.Decision)
	}
	if v.Reason == "" {
		t.Error("Abort should carry a reason")
	}
}
