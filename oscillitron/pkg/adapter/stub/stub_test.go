// CLAUDE GENERATED
package stub

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestStubModeDone(t *testing.T) {
	a := New("code", ModeDone).WithConfidence(0.8).WithSignals("looks-ok")
	env := session.Envelope{ID: "s1", Objective: "review f"}
	out, err := a.Call(context.Background(), env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want %q", out.ExitReason, session.ExitDone)
	}
	if out.Confidence != 0.8 {
		t.Errorf("Confidence = %v, want 0.8", out.Confidence)
	}
	if len(out.Signals) != 1 || out.Signals[0] != "looks-ok" {
		t.Errorf("Signals = %v, want [looks-ok]", out.Signals)
	}
	if a.Calls() != 1 {
		t.Errorf("Calls() = %d, want 1", a.Calls())
	}
}

func TestStubModeBudgetExhausted(t *testing.T) {
	a := New("writer", ModeBudgetExhausted)
	out, err := a.Call(context.Background(), session.Envelope{Objective: "long doc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != session.ExitBudgetExhausted {
		t.Errorf("ExitReason = %q, want %q", out.ExitReason, session.ExitBudgetExhausted)
	}
	if len(out.OpenQuestions) == 0 {
		t.Error("expected open questions on budget exhaustion")
	}
}

func TestStubModeError(t *testing.T) {
	a := New("flaky", ModeError)
	_, err := a.Call(context.Background(), session.Envelope{})
	if err == nil {
		t.Fatal("expected error from ModeError stub")
	}
}

func TestStubRespectsContextCancellation(t *testing.T) {
	a := New("x", ModeDone)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Call(ctx, session.Envelope{})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
