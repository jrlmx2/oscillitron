// CLAUDE GENERATED
package stub

import (
	"context"
	"errors"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func envelopeWithEvaluate(p session.Playbook) session.Envelope {
	return session.Envelope{
		Input:    session.Payload{Kind: "task", Content: "do thing"},
		Evaluate: &session.Evaluate{Playbook: p, Confidence: 0.7},
	}
}

func TestStubEvaluateDefault(t *testing.T) {
	a := New("stub")
	env, err := a.Evaluate(context.Background(), session.Envelope{
		Input: session.Payload{Kind: "task", Content: "do thing"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Evaluate == nil || env.Evaluate.Playbook != session.PlaybookProcess {
		t.Fatalf("default playbook should be process; got %+v", env.Evaluate)
	}
	if a.EvalCalls() != 1 {
		t.Errorf("EvalCalls = %d, want 1", a.EvalCalls())
	}
}

func TestStubWithDefaultPlaybook(t *testing.T) {
	a := New("planner").WithDefaultPlaybook(session.PlaybookPlan)
	env, err := a.Evaluate(context.Background(), session.Envelope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Evaluate.Playbook != session.PlaybookPlan {
		t.Errorf("got %q, want plan", env.Evaluate.Playbook)
	}
}

func TestStubExecuteReturnResult(t *testing.T) {
	a := New("worker").WithReturnResult(session.PlaybookProcess,
		session.Payload{Kind: "result", Content: "42"}, 0.9)
	env, err := a.Execute(context.Background(), envelopeWithEvaluate(session.PlaybookProcess))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Execute == nil || env.Execute.Category != session.CategoryReturnResult {
		t.Fatalf("expected return_result; got %+v", env.Execute)
	}
	if env.Execute.ReturnResult.Confidence != 0.9 {
		t.Errorf("confidence not stamped: %v", env.Execute.ReturnResult.Confidence)
	}
	if env.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want done", env.ExitReason)
	}
}

func TestStubExecuteEmitSubtree(t *testing.T) {
	seed := session.SubAPSeed{
		Input:        session.Payload{Kind: "task", Content: "subtask 1"},
		OutputSchema: "{step_result}",
	}
	a := New("planner").WithEmitSubtree(session.PlaybookPlan, session.RecomposePairwise, seed, seed)
	env, err := a.Execute(context.Background(), envelopeWithEvaluate(session.PlaybookPlan))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Execute.Category != session.CategoryEmitSubtree {
		t.Fatalf("category = %q, want emit_subtree", env.Execute.Category)
	}
	if len(env.Execute.EmitSubtree.SubAPs) != 2 {
		t.Errorf("expected 2 sub-APs; got %d", len(env.Execute.EmitSubtree.SubAPs))
	}
	if env.Execute.EmitSubtree.Recompose != session.RecomposePairwise {
		t.Errorf("recompose = %q, want pairwise", env.Execute.EmitSubtree.Recompose)
	}
}

func TestStubExecuteVerifierSignal(t *testing.T) {
	a := New("critic").WithVerifierSignal(session.PlaybookCritique, session.VerdictIssues,
		session.Issue{Severity: session.SeverityWarning, Where: "line 12", What: "loop bound suspicious"})
	env, err := a.Execute(context.Background(), envelopeWithEvaluate(session.PlaybookCritique))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Execute.Category != session.CategoryVerifierSignal {
		t.Fatalf("category = %q, want verifier_signal", env.Execute.Category)
	}
	if env.Execute.VerifierSignal.Verdict != session.VerdictIssues {
		t.Errorf("verdict = %q, want issues", env.Execute.VerifierSignal.Verdict)
	}
	if len(env.Execute.VerifierSignal.Issues) != 1 {
		t.Errorf("issues lost: %+v", env.Execute.VerifierSignal.Issues)
	}
}

func TestStubExecuteExitReasonOverride(t *testing.T) {
	a := New("starved").
		WithReturnResult(session.PlaybookProcess, session.Payload{}, 0.5).
		WithExitReason(session.PlaybookProcess, session.ExitBudgetExhausted)
	env, _ := a.Execute(context.Background(), envelopeWithEvaluate(session.PlaybookProcess))
	if env.ExitReason != session.ExitBudgetExhausted {
		t.Errorf("ExitReason = %q, want budget_exhausted", env.ExitReason)
	}
}

func TestStubExecuteBeforeEvaluate(t *testing.T) {
	a := New("buggy")
	_, err := a.Execute(context.Background(), session.Envelope{})
	if err == nil {
		t.Fatal("expected error when Execute called before Evaluate")
	}
}

func TestStubEvalError(t *testing.T) {
	want := errors.New("eval failed")
	a := New("broken").WithEvalError(want)
	if _, err := a.Evaluate(context.Background(), session.Envelope{}); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestStubExecuteError(t *testing.T) {
	want := errors.New("execute failed")
	a := New("broken").WithExecuteError(want)
	if _, err := a.Execute(context.Background(), envelopeWithEvaluate(session.PlaybookProcess)); !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestStubCounters(t *testing.T) {
	a := New("counter").WithReturnResult(session.PlaybookProcess, session.Payload{}, 0.5)
	for i := 0; i < 3; i++ {
		env, _ := a.Evaluate(context.Background(), session.Envelope{})
		_, _ = a.Execute(context.Background(), env)
	}
	if got := a.EvalCalls(); got != 3 {
		t.Errorf("EvalCalls = %d, want 3", got)
	}
	if got := a.ExecuteCalls(); got != 3 {
		t.Errorf("ExecuteCalls = %d, want 3", got)
	}
	if got := a.Calls(); got != 6 {
		t.Errorf("Calls = %d, want 6", got)
	}
	if got := a.CallsForPlaybook(session.PlaybookProcess); got != 3 {
		t.Errorf("CallsForPlaybook(process) = %d, want 3", got)
	}
}

func TestStubDefaultExecuteResponse(t *testing.T) {
	// Stub returns a default return_result when no response is configured.
	a := New("lazy")
	env, err := a.Execute(context.Background(), envelopeWithEvaluate(session.PlaybookProcess))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Execute.Category != session.CategoryReturnResult {
		t.Errorf("default category should be return_result; got %q", env.Execute.Category)
	}
}
