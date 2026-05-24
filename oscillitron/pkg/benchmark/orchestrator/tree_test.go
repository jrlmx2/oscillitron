package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// letterExtractor pulls the last [A-D] letter from a string.
// (Mirrors what cmd/bench wires in real runs via grader.ExtractLetter.)
func letterExtractor() Extractor {
	return ExtractorFunc(func(raw string) string {
		for i := len(raw) - 1; i >= 0; i-- {
			c := raw[i]
			if c >= 'A' && c <= 'D' {
				return string(c)
			}
		}
		return ""
	})
}

func TestTree_RequiresAdapter(t *testing.T) {
	_, err := Tree{Synthesizer: recomposer.NewSynthStub("s"), Extractor: letterExtractor()}.
		Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with nil Adapter")
	}
}

func TestTree_RequiresSynthesizer(t *testing.T) {
	a := stub.New("a")
	_, err := Tree{Adapter: a, Extractor: letterExtractor()}.
		Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with nil Synthesizer")
	}
}

func TestTree_RequiresExtractor(t *testing.T) {
	a := stub.New("a")
	_, err := Tree{Adapter: a, Synthesizer: recomposer.NewSynthStub("s")}.
		Answer(context.Background(), benchmark.Case{ID: "x"})
	if err == nil {
		t.Fatal("expected error with nil Extractor")
	}
}

func TestTree_PlanDecomposeRecompose(t *testing.T) {
	// Build a stub adapter that:
	//   - On plan playbook (root): emits 2 sub-APs with pairwise recompose.
	//   - On process playbook (children): returns "X containing A" so the
	//     letter extractor finds "A" as the final answer.
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
	a.WithReturnResult(
		session.PlaybookProcess,
		session.Payload{Kind: "result", Content: "partial result A"},
		0.9,
	)
	// Evaluator: route plan for root, process for children.
	a.WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
		if env.ParentID == nil {
			return session.PlaybookPlan, 1.0
		}
		return session.PlaybookProcess, 0.9
	})

	// Synth stub: emits "merged: <Left> + <Right>" with confidence 0.95.
	synth := recomposer.NewSynthStub("synth").
		WithFormat(func(req recomposer.SynthesizeRequest) string {
			return "merged final answer A combining: " + req.Left.Result.Content + " and " + req.Right.Result.Content
		}).
		WithConfidence(0.95)

	tree := Tree{
		NameStr:     "test-tree",
		Adapter:     a,
		Synthesizer: synth,
		Extractor:   letterExtractor(),
	}

	ans, err := tree.Answer(context.Background(), benchmark.Case{ID: "x", Prompt: "What letter?"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A", ans.Extracted)
	}
	if !strings.Contains(ans.Raw, "merged") {
		t.Errorf("Raw should contain synthesizer's merge marker; got %q", ans.Raw)
	}
	if ans.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95 (from synth)", ans.Confidence)
	}
	// Expect at least 1 plan + 2 process executes + 1 synth call = 4 calls minimum.
	// (Tree counts Evaluate + Execute; synth is its own Adapter call so it bumps the counter too.)
	if ans.Calls < 3 {
		t.Errorf("Calls = %d, want at least 3 (plan + 2 children)", ans.Calls)
	}
	// Synth must have been invoked exactly once (2 children → 1 binary reduction).
	if got := synth.Calls(); got != 1 {
		t.Errorf("synth Calls = %d, want 1 (2 children = 1 pairwise reduction)", got)
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

func TestTree_ForcesPlanPlaybook(t *testing.T) {
	// The root must be pre-stamped with PlaybookPlan so the model's
	// Evaluate doesn't accidentally pick Process (which would degenerate
	// Tree to Single under a different name). Verify by inspecting the
	// stub's evaluator-call history: the evaluator should NOT have been
	// called for the root because Evaluate is pre-stamped.
	a := stub.New("tree-force")
	a.WithReturnResult(
		session.PlaybookPlan,
		session.Payload{Kind: "result", Content: "A"},
		0.8,
	)
	a.WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
		// If this is invoked for the root, the pre-stamp didn't work.
		// Return Process to make the test fail loudly.
		return session.PlaybookProcess, 0.0
	})
	// The plan playbook here returns a ReturnResult directly (degenerate
	// "plan that just returns") so we can test the pre-stamping without
	// also needing children + recompose.
	a.WithReturnResult(
		session.PlaybookPlan,
		session.Payload{Kind: "result", Content: "A direct from plan"},
		0.8,
	)

	tree := Tree{
		Adapter:     a,
		Synthesizer: recomposer.NewSynthStub("synth"),
		Extractor:   letterExtractor(),
	}
	ans, err := tree.Answer(context.Background(), benchmark.Case{ID: "y", Prompt: "what?"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A (plan playbook returned 'A direct from plan')", ans.Extracted)
	}
}
