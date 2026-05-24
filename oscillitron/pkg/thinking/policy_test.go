package thinking

import (
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/stakes"
)

func envWith(s stakes.Level, pb session.Playbook) session.Envelope {
	env := session.Envelope{Stakes: s}
	if pb != "" {
		env.Evaluate = &session.Evaluate{Playbook: pb}
	}
	return env
}

func TestAlwaysOn(t *testing.T) {
	if !(AlwaysOn{}.ShouldThink(envWith(stakes.Low, ""))) {
		t.Errorf("AlwaysOn should think on low stakes empty env")
	}
	if !(AlwaysOn{}.ShouldThink(envWith(stakes.High, session.PlaybookPlan))) {
		t.Errorf("AlwaysOn should think on high stakes plan")
	}
}

func TestAlwaysOff(t *testing.T) {
	if (AlwaysOff{}).ShouldThink(envWith(stakes.High, session.PlaybookPlan)) {
		t.Errorf("AlwaysOff should never think")
	}
}

func TestByStakes_DefaultMapping(t *testing.T) {
	p := ByStakes{}
	cases := []struct {
		s    stakes.Level
		want bool
	}{
		{stakes.Low, false},
		{stakes.Medium, false},
		{stakes.High, true},
		{"", false},
	}
	for _, tc := range cases {
		if got := p.ShouldThink(envWith(tc.s, "")); got != tc.want {
			t.Errorf("ByStakes(%q) = %v, want %v", tc.s, got, tc.want)
		}
	}
}

func TestByStakes_CustomMapping(t *testing.T) {
	p := ByStakes{On: map[string]bool{"medium": true, "high": true}}
	if p.ShouldThink(envWith(stakes.Low, "")) {
		t.Errorf("custom: low should be off")
	}
	if !p.ShouldThink(envWith(stakes.Medium, "")) {
		t.Errorf("custom: medium should be on")
	}
	if !p.ShouldThink(envWith(stakes.High, "")) {
		t.Errorf("custom: high should be on")
	}
}

func TestByPlaybook_DefaultMappingIsAllOff(t *testing.T) {
	p := ByPlaybook{}
	for _, pb := range []session.Playbook{session.PlaybookPlan, session.PlaybookProcess, session.PlaybookCompose, session.PlaybookCritique} {
		if p.ShouldThink(envWith(stakes.High, pb)) {
			t.Errorf("zero-value ByPlaybook should be off for %q", pb)
		}
	}
}

func TestByPlaybook_CustomMapping(t *testing.T) {
	p := ByPlaybook{On: map[session.Playbook]bool{
		session.PlaybookPlan:    true,
		session.PlaybookCompose: true,
	}}
	if !p.ShouldThink(envWith(stakes.Low, session.PlaybookPlan)) {
		t.Errorf("plan should be on")
	}
	if !p.ShouldThink(envWith(stakes.Low, session.PlaybookCompose)) {
		t.Errorf("compose should be on")
	}
	if p.ShouldThink(envWith(stakes.Low, session.PlaybookProcess)) {
		t.Errorf("process should be off")
	}
	if p.ShouldThink(envWith(stakes.Low, session.PlaybookCritique)) {
		t.Errorf("critique should be off")
	}
}

func TestByPlaybook_NoEvaluateMeansOff(t *testing.T) {
	p := ByPlaybook{On: map[session.Playbook]bool{session.PlaybookPlan: true}}
	if p.ShouldThink(session.Envelope{}) {
		t.Errorf("ByPlaybook should not think when env.Evaluate is nil")
	}
}

func TestComposite_OrsWrappedPolicies(t *testing.T) {
	p := Composite{Policies: []Policy{
		ByStakes{}, // only high triggers
		ByPlaybook{On: map[session.Playbook]bool{session.PlaybookPlan: true}},
	}}

	// High stakes + non-plan: high triggers (via ByStakes)
	if !p.ShouldThink(envWith(stakes.High, session.PlaybookProcess)) {
		t.Errorf("composite: should think on high stakes regardless of playbook")
	}
	// Low stakes + plan: plan triggers (via ByPlaybook)
	if !p.ShouldThink(envWith(stakes.Low, session.PlaybookPlan)) {
		t.Errorf("composite: should think on plan playbook regardless of stakes")
	}
	// Low stakes + non-plan: neither triggers
	if p.ShouldThink(envWith(stakes.Low, session.PlaybookProcess)) {
		t.Errorf("composite: should NOT think on low stakes + process")
	}
}

func TestComposite_NilPolicyIsSkipped(t *testing.T) {
	// Defensive: a nil entry in Policies shouldn't panic.
	p := Composite{Policies: []Policy{nil, AlwaysOff{}, nil}}
	if p.ShouldThink(envWith(stakes.High, session.PlaybookPlan)) {
		t.Errorf("composite with all-off (modulo nils) should be off")
	}
	p2 := Composite{Policies: []Policy{nil, AlwaysOn{}, nil}}
	if !p2.ShouldThink(envWith(stakes.Low, session.PlaybookProcess)) {
		t.Errorf("composite with AlwaysOn should be on")
	}
}

func TestComposite_EmptyAllowsNothing(t *testing.T) {
	p := Composite{}
	if p.ShouldThink(envWith(stakes.High, session.PlaybookPlan)) {
		t.Errorf("empty composite should never think")
	}
}
