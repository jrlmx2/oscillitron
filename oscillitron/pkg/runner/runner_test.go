// CLAUDE GENERATED
package runner

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// fakeRecomposer joins all children's Result.Content with " | " and
// takes the min confidence. Used in tests so the runner can exercise
// the emit_subtree path without depending on Stage 4's real Concat.
type fakeRecomposer struct {
	lastSpec     session.RecomposeSpec
	lastChildren []session.ReturnResultPayload
	calls        int
	err          error
}

func (f *fakeRecomposer) Recompose(_ context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error) {
	f.calls++
	f.lastSpec = spec
	f.lastChildren = append([]session.ReturnResultPayload(nil), children...)
	if f.err != nil {
		return session.ReturnResultPayload{}, f.err
	}
	parts := make([]string, 0, len(children))
	minConf := -1.0
	for _, c := range children {
		parts = append(parts, c.Result.Content)
		if minConf < 0 || c.Confidence < minConf {
			minConf = c.Confidence
		}
	}
	if minConf < 0 {
		minConf = 0
	}
	return session.ReturnResultPayload{
		Result:     session.Payload{Kind: "result", Content: strings.Join(parts, " | ")},
		Confidence: minConf,
	}, nil
}

func seededRand() *rand.Rand {
	return rand.New(rand.NewPCG(42, 1024))
}

func TestRun_RootReturnResult(t *testing.T) {
	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "answer"}, 0.85)

	root := session.NewRoot("ap-root", "do the thing", "{answer}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{Adapter: a, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Root.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want done", res.Root.ExitReason)
	}
	if res.Root.Evaluate == nil || res.Root.Evaluate.Playbook != session.PlaybookProcess {
		t.Errorf("Evaluate not populated: %+v", res.Root.Evaluate)
	}
	if res.Root.Execute == nil || res.Root.Execute.Category != session.CategoryReturnResult {
		t.Fatalf("Execute not return_result: %+v", res.Root.Execute)
	}
	if res.ResolvedPayload.Result.Content != "answer" {
		t.Errorf("ResolvedPayload = %q, want %q", res.ResolvedPayload.Result.Content, "answer")
	}
	if res.State.EvaluateCount != 1 || res.State.ExecuteCount != 1 {
		t.Errorf("counts: eval=%d exec=%d", res.State.EvaluateCount, res.State.ExecuteCount)
	}
}

func TestRun_RootVerifierSignal(t *testing.T) {
	a := stub.New("critic").
		WithDefaultPlaybook(session.PlaybookCritique).
		WithVerifierSignal(session.PlaybookCritique, session.VerdictIssues,
			session.Issue{Severity: session.SeverityWarning, What: "looks off"})

	root := session.NewRoot("ap-root", "check this", "pass|fail", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{Adapter: a, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Root.Execute.Category != session.CategoryVerifierSignal {
		t.Fatalf("Category = %q, want verifier_signal", res.Root.Execute.Category)
	}
	// Verifier signal records in RunState, NOT on the next AP.
	if len(res.State.VerifierSignals) != 1 {
		t.Fatalf("VerifierSignals: got %d, want 1", len(res.State.VerifierSignals))
	}
	if res.State.VerifierSignals[0].Verdict != session.VerdictIssues {
		t.Errorf("verdict not recorded: %+v", res.State.VerifierSignals[0])
	}
	// ResolvedPayload is zero for verifier_signal roots.
	if !isZeroPayload(res.ResolvedPayload) {
		t.Errorf("ResolvedPayload should be zero for verifier_signal root; got %+v", res.ResolvedPayload)
	}
}

func TestRun_EmitSubtreeWithRecompose(t *testing.T) {
	// Plan emits 3 process children. Each process returns a distinct
	// content. The fake recomposer joins them.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "step a"}, OutputSchema: "{r}"},
		{Input: session.Payload{Kind: "task", Content: "step b"}, OutputSchema: "{r}"},
		{Input: session.Payload{Kind: "task", Content: "step c"}, OutputSchema: "{r}"},
	}
	// Adapter: plan picks PlaybookPlan and emits seeds; sub-APs pick
	// PlaybookProcess and return content derived from input.
	a := stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "x"}, 0.8)

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{combined}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Root.Execute.Category != session.CategoryEmitSubtree {
		t.Fatalf("root Category = %q, want emit_subtree", res.Root.Execute.Category)
	}
	if rec.calls != 1 {
		t.Errorf("recomposer.calls = %d, want 1", rec.calls)
	}
	if rec.lastSpec != session.RecomposeSequential {
		t.Errorf("spec = %q, want sequential", rec.lastSpec)
	}
	if len(rec.lastChildren) != 3 {
		t.Fatalf("recomposer received %d children, want 3", len(rec.lastChildren))
	}
	// Composed payload is the fake's join.
	if res.ResolvedPayload.Result.Content != "x | x | x" {
		t.Errorf("composed = %q, want %q", res.ResolvedPayload.Result.Content, "x | x | x")
	}
	// 4 evals, 4 executes (1 plan + 3 process).
	if res.State.EvaluateCount != 4 || res.State.ExecuteCount != 4 {
		t.Errorf("counts: eval=%d exec=%d, want 4/4", res.State.EvaluateCount, res.State.ExecuteCount)
	}
	// Subtree has 3 resolved children under the root.
	if got := len(res.Subtree[root.ID]); got != 3 {
		t.Errorf("subtree[root]: got %d, want 3", got)
	}
}

func TestRun_VerifierSignalChildDoesNotBubble(t *testing.T) {
	// Plan emits 2 children: one process returns a payload, the other
	// is a critique that produces a verifier_signal. Only the process
	// child should feed the recomposer.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "work"}},
		{Input: session.Payload{Kind: "task", Content: "check"}},
	}
	a := stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			switch env.Input.Content {
			case "go":
				return session.PlaybookPlan, 0.9
			case "work":
				return session.PlaybookProcess, 0.9
			case "check":
				return session.PlaybookCritique, 0.9
			}
			return session.PlaybookProcess, 0.5
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "did work"}, 0.8).
		WithVerifierSignal(session.PlaybookCritique, session.VerdictPass)

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.lastChildren) != 1 {
		t.Errorf("recomposer should see 1 child (process only); got %d", len(rec.lastChildren))
	}
	if rec.lastChildren[0].Result.Content != "did work" {
		t.Errorf("composed input: got %q", rec.lastChildren[0].Result.Content)
	}
	if len(res.State.VerifierSignals) != 1 || res.State.VerifierSignals[0].Verdict != session.VerdictPass {
		t.Errorf("verifier signal not captured: %+v", res.State.VerifierSignals)
	}
}

// recordingInhibitor counts edges and can be configured to abort one.
type recordingInhibitor struct {
	edges     []inhibitor.Edge
	abortOnID session.ID
}

func (r *recordingInhibitor) Check(e inhibitor.Edge) inhibitor.Verdict {
	r.edges = append(r.edges, e)
	if e.Child.ID == r.abortOnID {
		return inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "test abort"}
	}
	return inhibitor.Verdict{Decision: inhibitor.Continue}
}

func TestRun_InhibitorChecksParentChildEdgesOnly(t *testing.T) {
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
		{Input: session.Payload{Kind: "task", Content: "b"}},
	}
	a := stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "ok"}, 0.8)

	inh := &recordingInhibitor{}
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Inhibitor: inh, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Root.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want done", res.Root.ExitReason)
	}
	// Root has no parent edge — only the two children should be checked.
	if len(inh.edges) != 2 {
		t.Errorf("inhibitor edges = %d, want 2 (root not checked)", len(inh.edges))
	}
	for _, e := range inh.edges {
		if e.Parent == nil || e.Parent.ID != root.ID {
			t.Errorf("edge parent should be root; got %v", e.Parent)
		}
		if len(e.Path) < 2 {
			t.Errorf("edge.Path should include root → child; got %d", len(e.Path))
		}
	}
}

func TestRun_InhibitorAbortPropagatesToParent(t *testing.T) {
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
		{Input: session.Payload{Kind: "task", Content: "b"}},
	}
	a := stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "ok"}, 0.8)

	// Abort the first emitted child. (Default ChildIDFn produces
	// "ap-root-c0" and "ap-root-c1".)
	inh := &recordingInhibitor{abortOnID: "ap-root-c0"}
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Inhibitor: inh, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Strict semantics: one inhibited child inhibits the parent.
	if !res.Root.IsInhibited() {
		t.Errorf("expected root inhibited; got ExitReason=%q", res.Root.ExitReason)
	}
	if res.State.InhibitCount == 0 {
		t.Errorf("InhibitCount should be > 0")
	}
	// Recomposer should not have been called.
	if rec.calls != 0 {
		t.Errorf("recomposer should not be called on inhibited subtree; got %d calls", rec.calls)
	}
}

func TestRun_MaxDepthCap(t *testing.T) {
	// Adapter that always plans, always emitting one further sub-AP.
	// Without a depth cap this recurses forever. MaxDepth=2 caps it.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "more"}},
	}
	a := stub.New("agent").
		WithDefaultPlaybook(session.PlaybookPlan).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 100})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, MaxDepth: 2, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Root.IsInhibited() && res.State.InhibitCount == 0 {
		t.Errorf("expected depth cap to inhibit somewhere; State=%+v", res.State)
	}
}

func TestRun_DepthBudgetExhausted(t *testing.T) {
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
	}
	a := stub.New("agent").
		WithDefaultPlaybook(session.PlaybookPlan).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	rec := &fakeRecomposer{}
	// Budget allows no descent: DepthRemaining=0 on root.
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 0})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Root.ExitReason != session.ExitBudgetExhausted {
		t.Errorf("ExitReason = %q, want budget_exhausted", res.Root.ExitReason)
	}
}

func TestRun_AdapterRequired(t *testing.T) {
	_, err := Run(context.Background(), Config{}, session.NewRoot("r", "", "", "", session.Budget{}))
	if !errors.Is(err, ErrAdapterRequired) {
		t.Errorf("got %v, want ErrAdapterRequired", err)
	}
}

func TestRun_RecomposerRequiredForEmitSubtree(t *testing.T) {
	seeds := []session.SubAPSeed{{Input: session.Payload{Kind: "task", Content: "a"}}}
	a := stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
		WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "x"}, 0.8)

	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	_, err := Run(context.Background(), Config{Adapter: a, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if !errors.Is(err, ErrRecomposerRequired) {
		t.Errorf("got %v, want ErrRecomposerRequired", err)
	}
}

func TestRun_RecomposerError(t *testing.T) {
	seeds := []session.SubAPSeed{{Input: session.Payload{Kind: "task", Content: "a"}}}
	a := stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
		WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "x"}, 0.8)

	want := errors.New("recompose blew up")
	rec := &fakeRecomposer{err: want}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if !errors.Is(err, want) {
		t.Errorf("got %v, want recompose error", err)
	}
	if !res.Root.IsInhibited() {
		t.Errorf("plan should be inhibited on recompose error")
	}
}

func TestRun_RandomizedDispatchVariesAcrossSeeds(t *testing.T) {
	// 6 children; record adapter call order via the input content.
	// Two different seeded runs should produce different visit orders.
	var seeds []session.SubAPSeed
	for i := 0; i < 6; i++ {
		seeds = append(seeds, session.SubAPSeed{
			Input: session.Payload{Kind: "task", Content: fmt.Sprintf("step%d", i)},
		})
	}

	runOrder := func(seed1, seed2 uint64) []string {
		var order []string
		// recordingEvaluator records the content of every AP it sees.
		recordingEval := func(env session.Envelope) (session.Playbook, float64) {
			order = append(order, env.Input.Content)
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}
		a := stub.New("agent").
			WithEvaluator(recordingEval).
			WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...).
			WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "x"}, 0.8)
		rec := &fakeRecomposer{}
		root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
		_, err := Run(context.Background(), Config{
			Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: rand.New(rand.NewPCG(seed1, seed2)),
		}, root)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		return order
	}

	a := runOrder(1, 1)
	b := runOrder(99, 7)
	// Both visit all 7 APs (1 root + 6 children).
	if len(a) != 7 || len(b) != 7 {
		t.Fatalf("expected 7 visits each; got %d and %d", len(a), len(b))
	}
	// First visit is always the root ("go"); after that, children
	// should appear in different orders across the two seeds.
	if a[0] != "go" || b[0] != "go" {
		t.Fatalf("root should be visited first; got %q / %q", a[0], b[0])
	}
	sameTail := true
	for i := 1; i < 7; i++ {
		if a[i] != b[i] {
			sameTail = false
			break
		}
	}
	if sameTail {
		t.Errorf("two different seeds produced identical child order: %v", a)
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	a := stub.New("agent").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess, session.Payload{}, 0.5)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	root := session.NewRoot("ap-root", "x", "y", "", session.Budget{DepthRemaining: 2})
	_, err := Run(ctx, Config{Adapter: a, Tracer: trace.Discard{}, Rand: seededRand()}, root)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}
