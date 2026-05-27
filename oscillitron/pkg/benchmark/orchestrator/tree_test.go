package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func letterExtractor() Extractor {
	return ExtractorFunc(func(_ context.Context, _, raw string) string {
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
	_, err := Tree{Extractor: letterExtractor()}.
		Answer(context.Background(), benchmark.Case{ID: "x", Goal: "a single letter A-D"})
	if err == nil {
		t.Fatal("expected error with nil Adapter")
	}
}

func TestTree_RequiresExtractor(t *testing.T) {
	a := stub.New("a")
	_, err := Tree{Adapter: a}.
		Answer(context.Background(), benchmark.Case{ID: "x", Goal: "a single letter A-D"})
	if err == nil {
		t.Fatal("expected error with nil Extractor")
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
		NameStr:   "test-tree",
		Adapter:   a,
		Extractor: letterExtractor(),
	}

	ans, err := tree.Answer(context.Background(), benchmark.Case{ID: "x", Prompt: "What letter?", Goal: "a single letter A-D"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A", ans.Extracted)
	}
	// Collect recomposer produces [Analysis 1]...[Analysis 2]... blocks.
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
		Adapter:   a,
		Extractor: letterExtractor(),
	}
	ans, err := tree.Answer(context.Background(), benchmark.Case{ID: "y", Prompt: "what?", Goal: "a single letter A-D"})
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ans.Extracted != "A" {
		t.Errorf("Extracted = %q, want A", ans.Extracted)
	}
	if ans.Calls != 2 {
		t.Errorf("Calls = %d, want 2 (one evaluate + one execute)", ans.Calls)
	}
}
