// CLAUDE GENERATED
package stub

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestStubDone(t *testing.T) {
	a := New("reasoner", ModeDone).WithConfidence(0.8).WithSignals("looks-ok").
		WithClassification("ok")
	out, err := a.Call(context.Background(), session.Envelope{
		Input: session.Input{Content: "do thing"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want %q", out.ExitReason, session.ExitDone)
	}
	if out.Confidence != 0.8 {
		t.Errorf("Confidence = %v, want 0.8", out.Confidence)
	}
	if out.Classification != "ok" {
		t.Errorf("Classification = %q, want ok", out.Classification)
	}
	if len(out.Signals) != 1 || out.Signals[0] != "looks-ok" {
		t.Errorf("Signals = %v", out.Signals)
	}
}

func TestStubBudgetExhausted(t *testing.T) {
	a := New("composer", ModeBudgetExhausted)
	out, err := a.Call(context.Background(), session.Envelope{
		Input: session.Input{Content: "long task"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != session.ExitBudgetExhausted {
		t.Errorf("ExitReason = %q, want %q", out.ExitReason, session.ExitBudgetExhausted)
	}
}

func TestStubEmitsSubAPs(t *testing.T) {
	seed := session.SubAPSeed{
		BrainFunction: session.BrainCritic,
		Input:         session.Input{Type: "subap", Content: "verify"},
		OutputSchema:  "approve | reject",
	}
	a := New("planner", ModeDone).WithSubAPs(seed)
	out, err := a.Call(context.Background(), session.Envelope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.SubAPs) != 1 || out.SubAPs[0].BrainFunction != session.BrainCritic {
		t.Fatalf("SubAPs not emitted: %+v", out.SubAPs)
	}
}

func TestStubError(t *testing.T) {
	a := New("broken", ModeError)
	if _, err := a.Call(context.Background(), session.Envelope{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestStubCallCounter(t *testing.T) {
	a := New("counter", ModeDone)
	for i := 0; i < 3; i++ {
		_, _ = a.Call(context.Background(), session.Envelope{})
	}
	if got := a.Calls(); got != 3 {
		t.Errorf("Calls() = %d, want 3", got)
	}
}
