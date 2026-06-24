package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/router"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// recordingRouter counts Hint calls and always abstains.
type recordingRouter struct{ calls int }

func (r *recordingRouter) Hint(_ context.Context, _ session.Payload) (router.Hint, error) {
	r.calls++
	return router.Hint{}, nil
}

func TestTree_ForwardsRouterToRunner(t *testing.T) {
	a := stub.New("tree-router")
	a.WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
		return session.PlaybookProcess, 0.9
	})
	a.WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "A"}, 0.8)

	rr := &recordingRouter{}
	tree := Tree{Adapter: a, Router: rr}
	if _, err := tree.Answer(context.Background(), benchmark.Case{ID: "z", Prompt: "what?", Goal: "a single letter A-D"}); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if rr.calls == 0 {
		t.Error("Tree did not forward Router to the runner (Hint never called)")
	}
}

func TestTree_RequiresAdapter(t *testing.T) {
	_, err := Tree{}.
		Answer(context.Background(), benchmark.Case{ID: "x", Goal: "a single letter A-D"})
	if err == nil {
		t.Fatal("expected error with nil Adapter")
	}
}

func TestTree_PlanDecomposeRecompose(t *testing.T) {
	a := stub.New("tree-stub")
	a.WithEmitSubtree(
		session.PlaybookPlan,
		session.RecomposePairwise,
		session.SubAPSeed{
			Input: session.Payload{Kind: "task", Content: "sub-task 1"},
		},
		session.SubAPSeed{
			Input: session.Payload{Kind: "task", Content: "sub-task 2"},
		},
	)
	longResult := "The answer is A because of detailed analysis."
	a.WithReturnResult(
		session.PlaybookProcess,
		session.Payload{Kind: "result", Content: longResult},
		0.9,
	)
	a.WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
		if env.ParentID == nil {
			return session.PlaybookPlan, 1.0
		}
		return session.PlaybookProcess, 0.9
	})

	tree := Tree{
		NameStr: "test-tree",
		Adapter: a,
	}

	ans, err := tree.Answer(context.Background(), benchmark.Case{ID: "x", Prompt: "What letter?", Goal: "a single letter A-D"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	// Extracted is empty — grader handles extraction from Raw.
	if ans.Extracted != "" {
		t.Errorf("Extracted = %q, want empty (grader extracts)", ans.Extracted)
	}
	// Raw contains the collected reasoning trail.
	if !strings.Contains(ans.Raw, "[Analysis 1]") {
		t.Errorf("Raw should contain collected analysis blocks; got %q", ans.Raw)
	}
	if ans.Calls < 3 {
		t.Errorf("Calls = %d, want at least 3 (plan + 2 children)", ans.Calls)
	}
}

func TestTree_NameDefault(t *testing.T) {
	if got := (Tree{}).Name(); got != "tree" {
		t.Errorf("default Name = %q, want tree", got)
	}
	if got := (Tree{NameStr: "tree-v0"}).Name(); got != "tree-v0" {
		t.Errorf("override Name = %q, want tree-v0", got)
	}
}

func TestTree_RootEvaluatesFreelyCanPickProcess(t *testing.T) {
	a := stub.New("tree-free")
	a.WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
		return session.PlaybookProcess, 0.9
	})
	a.WithReturnResult(
		session.PlaybookProcess,
		session.Payload{Kind: "result", Content: "A"},
		0.8,
	)

	tree := Tree{
		Adapter: a,
	}
	ans, err := tree.Answer(context.Background(), benchmark.Case{ID: "y", Prompt: "what?", Goal: "a single letter A-D"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	// Raw carries the answer; Extracted is empty for grader.
	if ans.Raw != "A" {
		t.Errorf("Raw = %q, want A", ans.Raw)
	}
	if ans.Calls != 2 {
		t.Errorf("Calls = %d, want 2 (one evaluate + one execute)", ans.Calls)
	}
}
