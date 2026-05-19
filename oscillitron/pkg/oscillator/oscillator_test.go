// CLAUDE GENERATED
package oscillator

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestInvokePopulatesOutput(t *testing.T) {
	a := stub.New("reasoner", stub.ModeDone).WithConfidence(0.7)
	o := New("reasoner-1", session.BrainReasoning, a, nil)
	env := session.Envelope{
		ID:            "s1",
		BrainFunction: session.BrainReasoning,
		Input:         session.Input{Content: "review"},
	}
	got := o.Invoke(context.Background(), env)
	if got.Output == nil {
		t.Fatal("Invoke did not populate Output")
	}
	if got.Output.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want %q", got.Output.ExitReason, session.ExitDone)
	}
	if got.Output.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7", got.Output.Confidence)
	}
}

func TestInvokeSurfacesAdapterErrorAsInhibited(t *testing.T) {
	a := stub.New("broken", stub.ModeError)
	o := New("broken-1", session.BrainReasoning, a, nil)
	got := o.Invoke(context.Background(), session.Envelope{ID: "s2"})
	if got.Output == nil {
		t.Fatal("Invoke did not populate Output on adapter error")
	}
	if got.Output.ExitReason != session.ExitInhibited {
		t.Errorf("ExitReason = %q, want %q", got.Output.ExitReason, session.ExitInhibited)
	}
	if len(got.Output.Contradictions) == 0 {
		t.Error("expected contradictions to flag adapter failure")
	}
}

func TestInvokeRespectsContextCancellation(t *testing.T) {
	a := stub.New("done", stub.ModeDone)
	o := New("done-1", session.BrainReasoning, a, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := o.Invoke(ctx, session.Envelope{})
	if got.Output == nil {
		t.Fatal("Invoke should populate Output even on ctx error")
	}
	if got.Output.ExitReason != session.ExitInhibited {
		t.Errorf("cancelled ctx should surface as Inhibited; got %q", got.Output.ExitReason)
	}
}
