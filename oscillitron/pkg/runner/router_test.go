package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/router"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// fakeRouter returns a canned hint (or error) and counts calls.
type fakeRouter struct {
	hint  router.Hint
	err   error
	calls int
}

func (f *fakeRouter) Hint(_ context.Context, _ session.Payload) (router.Hint, error) {
	f.calls++
	return f.hint, f.err
}

func TestRouter_SeedsEvaluateNeverSkips(t *testing.T) {
	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "answer"}, 0.85)
	fr := &fakeRouter{hint: router.Hint{Playbook: session.PlaybookPlan, Confidence: 0.8, K: 8}}

	root := session.NewRoot("ap-root", "do the thing", "{answer}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{Adapter: a, Router: fr, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The hint was consulted and seeded as steering text...
	if fr.calls != 1 {
		t.Errorf("router consulted %d times, want 1", fr.calls)
	}
	if !strings.Contains(res.Root.Input.Content, "[playbook-hint: plan") {
		t.Errorf("steering text not seeded into Evaluate input: %q", res.Root.Input.Content)
	}
	// ...AND Evaluate still ran (the hint NEVER skips it).
	if res.State.EvaluateCount != 1 {
		t.Errorf("EvaluateCount = %d, want 1 (hint must not skip Evaluate)", res.State.EvaluateCount)
	}
	if res.State.RouterHintsProduced != 1 {
		t.Errorf("RouterHintsProduced = %d, want 1", res.State.RouterHintsProduced)
	}
}

func TestRouter_OverrideCounterAndTrace(t *testing.T) {
	root := session.NewRoot("ap-root", "do the thing", "{answer}", "", session.Budget{DepthRemaining: 3})
	mk := func() *stub.Adapter {
		return stub.New("worker").
			WithDefaultPlaybook(session.PlaybookProcess).
			WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "answer"}, 0.85)
	}

	// Hint=plan but Evaluate picks process → one override.
	fr := &fakeRouter{hint: router.Hint{Playbook: session.PlaybookPlan, Confidence: 0.8, K: 8}}
	res, err := Run(context.Background(), Config{Adapter: mk(), Router: fr, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.RouterHintOverrides != 1 || res.State.RouterHintsProduced != 1 {
		t.Errorf("override case: overrides=%d produced=%d, want 1/1", res.State.RouterHintOverrides, res.State.RouterHintsProduced)
	}

	// Hint=process and Evaluate picks process → no override.
	fr2 := &fakeRouter{hint: router.Hint{Playbook: session.PlaybookProcess, Confidence: 0.8, K: 8}}
	res2, err := Run(context.Background(), Config{Adapter: mk(), Router: fr2, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res2.State.RouterHintOverrides != 0 || res2.State.RouterHintsProduced != 1 {
		t.Errorf("agree case: overrides=%d produced=%d, want 0/1", res2.State.RouterHintOverrides, res2.State.RouterHintsProduced)
	}
}

func TestRouter_NilRouterUnchanged(t *testing.T) {
	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "answer"}, 0.85)
	root := session.NewRoot("ap-root", "do the thing", "{answer}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{Adapter: a /* nil Router */, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(res.Root.Input.Content, "playbook-hint") {
		t.Errorf("nil Router seeded steering text: %q", res.Root.Input.Content)
	}
	if res.State.RouterHintsProduced != 0 || res.State.RouterHintOverrides != 0 {
		t.Errorf("nil Router counters nonzero: produced=%d overrides=%d", res.State.RouterHintsProduced, res.State.RouterHintOverrides)
	}
}

func TestRouter_HintErrorIsBestEffort(t *testing.T) {
	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "answer"}, 0.85)
	fr := &fakeRouter{err: errors.New("store unreachable")}
	root := session.NewRoot("ap-root", "do the thing", "{answer}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{Adapter: a, Router: fr, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run should not fail on router error: %v", err)
	}
	if res.State.EvaluateCount != 1 {
		t.Errorf("EvaluateCount = %d, want 1 (router error must not block Evaluate)", res.State.EvaluateCount)
	}
	if res.State.RouterHintsProduced != 0 {
		t.Errorf("RouterHintsProduced = %d, want 0 (errored hint is not 'produced')", res.State.RouterHintsProduced)
	}
	if strings.Contains(res.Root.Input.Content, "playbook-hint") {
		t.Errorf("errored hint still seeded steering text: %q", res.Root.Input.Content)
	}
}
