// CLAUDE GENERATED
package cope

import (
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/stakes"
)

func TestDecide_HighConfidence_Ships(t *testing.T) {
	rt := DefaultRuleTable()
	for _, level := range []stakes.Level{stakes.Low, stakes.Medium, stakes.High, ""} {
		if got := rt.Decide(0.92, level); got != Ship {
			t.Errorf("high conf + stakes=%q → got %q, want Ship", level, got)
		}
	}
}

func TestDecide_MediumConfidence_ShipsWithCaveat(t *testing.T) {
	rt := DefaultRuleTable()
	for _, level := range []stakes.Level{stakes.Low, stakes.Medium, stakes.High} {
		if got := rt.Decide(0.7, level); got != ShipWithCaveat {
			t.Errorf("mid conf + stakes=%q → got %q, want ShipWithCaveat", level, got)
		}
	}
}

func TestDecide_LowConfidence_LowStakes_ShipsWithCaveat(t *testing.T) {
	rt := DefaultRuleTable()
	for _, level := range []stakes.Level{stakes.Low, stakes.Medium} {
		if got := rt.Decide(0.3, level); got != ShipWithCaveat {
			t.Errorf("low conf + stakes=%q → got %q, want ShipWithCaveat (no escalation worth the cost)", level, got)
		}
	}
}

func TestDecide_LowConfidence_HighStakes_Escalates(t *testing.T) {
	rt := DefaultRuleTable()
	if got := rt.Decide(0.3, stakes.High); got != Escalate {
		t.Errorf("low conf + high stakes → got %q, want Escalate", got)
	}
}

func TestDecide_LowConfidence_HighStakes_NoFrontier_Refuses(t *testing.T) {
	rt := DefaultRuleTable()
	rt.EscalateAllowed = false
	if got := rt.Decide(0.3, stakes.High); got != Refuse {
		t.Errorf("low conf + high stakes + no frontier → got %q, want Refuse", got)
	}
}

func TestDecide_ZeroConfidence_TreatedAsCaveat(t *testing.T) {
	rt := DefaultRuleTable()
	// Zero == "not reported" — fall back to safe default.
	if got := rt.Decide(0, stakes.Medium); got != ShipWithCaveat {
		t.Errorf("zero conf (unreported) → got %q, want ShipWithCaveat", got)
	}
	// Even on high stakes, zero confidence shouldn't escalate
	// blindly — we don't know if it's actually low or just
	// missing. Caveat is the conservative read.
	if got := rt.Decide(0, stakes.High); got != ShipWithCaveat {
		t.Errorf("zero conf + high stakes → got %q, want ShipWithCaveat (don't escalate on missing data)", got)
	}
}

func TestDecide_StakesZeroValue_DefaultsToMedium(t *testing.T) {
	rt := DefaultRuleTable()
	// Stakes unset (zero value) reads as Medium per stakes package.
	// Low confidence + unset → ShipWithCaveat (medium-stakes path).
	if got := rt.Decide(0.3, ""); got != ShipWithCaveat {
		t.Errorf("low conf + zero stakes → got %q, want ShipWithCaveat", got)
	}
}

func TestDecide_BoundaryAtHighFloor(t *testing.T) {
	rt := DefaultRuleTable()
	// Exactly at the floor is Ship.
	if got := rt.Decide(0.85, stakes.Medium); got != Ship {
		t.Errorf("conf=0.85 (exactly at floor) → got %q, want Ship", got)
	}
	// Just below is ShipWithCaveat.
	if got := rt.Decide(0.849, stakes.Medium); got != ShipWithCaveat {
		t.Errorf("conf=0.849 (just below floor) → got %q, want ShipWithCaveat", got)
	}
}

func TestDecide_BoundaryAtLowFloor(t *testing.T) {
	rt := DefaultRuleTable()
	// Exactly at LowConfidence is ShipWithCaveat (>=, not >).
	if got := rt.Decide(0.5, stakes.High); got != ShipWithCaveat {
		t.Errorf("conf=0.5 (exactly at low floor) + high → got %q, want ShipWithCaveat", got)
	}
	// Just below is Escalate (high stakes).
	if got := rt.Decide(0.499, stakes.High); got != Escalate {
		t.Errorf("conf=0.499 (just below low floor) + high → got %q, want Escalate", got)
	}
}

func TestDecide_CustomThresholds(t *testing.T) {
	// Operator-tuned: shift both floors down.
	rt := RuleTable{
		HighConfidence:  0.6,
		LowConfidence:   0.3,
		EscalateAllowed: true,
	}
	if got := rt.Decide(0.7, stakes.High); got != Ship {
		t.Errorf("with lowered floors, 0.7 should ship; got %q", got)
	}
	if got := rt.Decide(0.4, stakes.High); got != ShipWithCaveat {
		t.Errorf("with lowered floors, 0.4 should caveat; got %q", got)
	}
	if got := rt.Decide(0.2, stakes.High); got != Escalate {
		t.Errorf("with lowered floors, 0.2 + high should escalate; got %q", got)
	}
}

func TestExplain_PopulatesRationale(t *testing.T) {
	rt := DefaultRuleTable()
	cases := []struct {
		conf         float64
		level        stakes.Level
		wantAction   Action
		wantContains string
	}{
		{0.9, stakes.Low, Ship, "ship floor"},
		{0.7, stakes.Medium, ShipWithCaveat, "medium-band"},
		{0.3, stakes.Low, ShipWithCaveat, "low/medium"},
		{0.3, stakes.High, Escalate, "escalate"},
		{0, stakes.Low, ShipWithCaveat, "no confidence reported"},
	}
	for _, tc := range cases {
		d := rt.Explain(tc.conf, tc.level)
		if d.Action != tc.wantAction {
			t.Errorf("Explain(%v, %q).Action = %q, want %q", tc.conf, tc.level, d.Action, tc.wantAction)
		}
		if d.Rationale == "" {
			t.Errorf("Explain(%v, %q) returned empty Rationale", tc.conf, tc.level)
		}
		// Check the rationale mentions a key concept.
		if !strings.Contains(d.Rationale, tc.wantContains) {
			t.Errorf("Explain(%v, %q).Rationale = %q, want to contain %q",
				tc.conf, tc.level, d.Rationale, tc.wantContains)
		}
	}
}

func TestExplain_PassesThroughEffectiveStakes(t *testing.T) {
	rt := DefaultRuleTable()
	// Zero stakes → Effective returns Medium → Decision.Stakes
	// should be Medium, not "".
	d := rt.Explain(0.7, "")
	if d.Stakes != stakes.Medium {
		t.Errorf("Decision.Stakes = %q, want medium (effective of zero value)", d.Stakes)
	}
}
