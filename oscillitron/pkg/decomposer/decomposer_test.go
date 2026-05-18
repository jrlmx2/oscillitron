// CLAUDE GENERATED
package decomposer

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestPassthroughDefaults(t *testing.T) {
	got, err := Passthrough{}.Decompose(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Decompose err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	env := got[0]
	if env.Input.Content != "hello" {
		t.Errorf("Content = %q, want hello", env.Input.Content)
	}
	if env.Classification != classification.Internal {
		t.Errorf("Classification = %v, want Internal default", env.Classification)
	}
	if env.Objective != "passthrough" {
		t.Errorf("Objective = %q, want default", env.Objective)
	}
	if env.Type != session.TypeAnalyze {
		t.Errorf("Type = %v, want analyze", env.Type)
	}
}

func TestPassthroughOverrides(t *testing.T) {
	p := Passthrough{Classification: classification.Confidential, Objective: "audit"}
	got, _ := p.Decompose(context.Background(), "x")
	if got[0].Classification != classification.Confidential {
		t.Errorf("Classification = %v, want Confidential", got[0].Classification)
	}
	if got[0].Objective != "audit" {
		t.Errorf("Objective = %q, want audit", got[0].Objective)
	}
}
