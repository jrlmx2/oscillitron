// CLAUDE GENERATED
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/judge"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/verifier"
	"github.com/jrlmx2/oscillitron/pkg/vram"
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
// Mutex-protected because concurrent dispatch (Config.MaxConcurrency > 1)
// invokes Check from multiple goroutines.
type recordingInhibitor struct {
	mu        sync.Mutex
	edges     []inhibitor.Edge
	abortOnID session.ID
}

func (r *recordingInhibitor) Check(e inhibitor.Edge) inhibitor.Verdict {
	r.mu.Lock()
	r.edges = append(r.edges, e)
	r.mu.Unlock()
	if e.Child.ID == r.abortOnID {
		return inhibitor.Verdict{Decision: inhibitor.Abort, Reason: "test abort"}
	}
	return inhibitor.Verdict{Decision: inhibitor.Continue}
}

func (r *recordingInhibitor) edgeCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.edges)
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
			// Strict serial: this test asserts that the randomized
			// *dispatch order* differs across seeds, which is a
			// property of serial execution. Under auto-managed
			// concurrency, completion order is non-deterministic for
			// reasons unrelated to the seed.
			MaxConcurrency: 1,
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

// --- Verifier policy integration tests ---

// verifierPolicyAdapter wires the stub adapter for a plan + 2 process
// children, plus a critique playbook that fires whenever the runner
// injects a critique AP (input.kind == "critique_target").
func verifierPolicyAdapter(t *testing.T) *stub.Adapter {
	t.Helper()
	return stub.New("agent").
		WithEvaluator(func(env session.Envelope) (session.Playbook, float64) {
			switch env.Input.Kind {
			case "critique_target":
				return session.PlaybookCritique, 0.95
			}
			if env.Input.Content == "go" {
				return session.PlaybookPlan, 0.9
			}
			return session.PlaybookProcess, 0.9
		}).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "result"}, 0.8).
		WithVerifierSignal(session.PlaybookCritique, session.VerdictPass)
}

func TestRun_VerifierPolicy_BootstrapEmitsCritiqueForEveryChild(t *testing.T) {
	// Plan emits 3 process children. Bootstrap threshold of 1000 means
	// the policy is in bootstrap for the entire run → critique fires
	// on every return_result.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
		{Input: session.Payload{Kind: "task", Content: "b"}},
		{Input: session.Payload{Kind: "task", Content: "c"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 1000,
		SlidingWindow:      100,
		ConfidenceLevel:    0.95,
		Floor:              0.15,
		HappinessScope:     verifier.ScopeGlobal,
	})
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.PolicyCritiquesEmitted != 3 {
		t.Errorf("PolicyCritiquesEmitted = %d, want 3 (bootstrap forces critique on all 3)",
			res.State.PolicyCritiquesEmitted)
	}
	if len(res.State.VerifierSignals) != 3 {
		t.Errorf("VerifierSignals = %d, want 3", len(res.State.VerifierSignals))
	}
	// Critiques must NOT feed the recomposer.
	if len(rec.lastChildren) != 3 {
		t.Errorf("recomposer received %d children, want 3 (critiques excluded)",
			len(rec.lastChildren))
	}
}

func TestRun_VerifierPolicy_ParentOverrideForcesCritiquePostBootstrap(t *testing.T) {
	// Steady-state policy at the floor (0.15). With seeded rand and 200
	// children, none of the rolls would (probabilistically) hit. But one
	// seed has NeedsVerification: true — parent override forces critique
	// for that child specifically.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
		{Input: session.Payload{Kind: "task", Content: "b"}, NeedsVerification: true},
		{Input: session.Payload{Kind: "task", Content: "c"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 0,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		Floor:              0.0001, // effectively 0 so the override path is the only critique source
		HappinessScope:     verifier.ScopeGlobal,
	})
	// Saturate happiness: judge always agrees → steady-state rate near 0.
	for i := 0; i < 500; i++ {
		pol.RecordJudgeAgreement(session.PlaybookProcess, true)
	}

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// At least 1 critique — the parent-override seed must fire one.
	if res.State.PolicyCritiquesEmitted < 1 {
		t.Errorf("PolicyCritiquesEmitted = %d, want >=1 from parent override",
			res.State.PolicyCritiquesEmitted)
	}
	// At least one verifier signal recorded; corresponds to the override.
	if len(res.State.VerifierSignals) < 1 {
		t.Errorf("VerifierSignals = %d, want >=1", len(res.State.VerifierSignals))
	}
}

func TestRun_VerifierPolicy_NoPolicyAndNoOverride_NoCritique(t *testing.T) {
	// No VerifierPolicy on Config, no NeedsVerification on seeds → no
	// auto-critique is injected. The runner falls back to the prior
	// behavior (only adapter-emitted critiques exist).
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
		{Input: session.Payload{Kind: "task", Content: "b"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.PolicyCritiquesEmitted != 0 {
		t.Errorf("PolicyCritiquesEmitted = %d, want 0 (no policy)",
			res.State.PolicyCritiquesEmitted)
	}
	if len(res.State.VerifierSignals) != 0 {
		t.Errorf("VerifierSignals = %d, want 0 (adapter only emits process)",
			len(res.State.VerifierSignals))
	}
}

func TestRun_VerifierPolicy_OverrideWithoutPolicyStillFires(t *testing.T) {
	// No VerifierPolicy but seed has NeedsVerification: true. The
	// override is honored — a critique is injected.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}, NeedsVerification: true},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.PolicyCritiquesEmitted != 1 {
		t.Errorf("PolicyCritiquesEmitted = %d, want 1 (override forces)",
			res.State.PolicyCritiquesEmitted)
	}
}

func TestRun_VerifierPolicy_PerActionScope_TelemetryTracksBoth(t *testing.T) {
	// Use per_action happiness scope. Run a few children; check that
	// telemetry has populated both global and per-action streams for
	// the process action.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
		{Input: session.Payload{Kind: "task", Content: "b"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 1000,
		SlidingWindow:      500,
		ConfidenceLevel:    0.95,
		Floor:              0.15,
		HappinessScope:     verifier.ScopePerAction,
	})
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	_, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	tel := pol.Telemetry()
	if tel.Scope != verifier.ScopePerAction {
		t.Errorf("Scope = %q, want per_action", tel.Scope)
	}
	if tel.Global.Invocations != 2 {
		t.Errorf("Global.Invocations = %d, want 2 (two policy consultations)",
			tel.Global.Invocations)
	}
	if pa, ok := tel.PerAction[session.PlaybookProcess]; !ok || pa.Invocations != 2 {
		t.Errorf("per-action process telemetry missing or wrong: %+v", pa)
	}
}

func TestRun_VerifierPolicy_CritiqueRecordedInSubtree(t *testing.T) {
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{BootstrapThreshold: 1000, SlidingWindow: 100, Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal})
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Plan's subtree should include the process child AND the critique.
	got := res.Subtree[root.ID]
	if len(got) != 2 {
		t.Fatalf("subtree[plan] children = %d, want 2 (process + critique)", len(got))
	}
	// One of the two should be a critique with verifier_signal category.
	foundCritique := false
	for _, child := range got {
		if child.Execute != nil && child.Execute.Category == session.CategoryVerifierSignal {
			foundCritique = true
		}
	}
	if !foundCritique {
		t.Errorf("expected a verifier_signal child in plan's subtree; got %+v", got)
	}
}

// --- Cost tracker integration ---

func TestRun_CostSummary_SnapshotsTrackerWhenWired(t *testing.T) {
	// Pre-record entries directly on the tracker (simulates what an
	// adapter does on each phase). Run a stub-only tree, confirm
	// RunState.CostSummary mirrors the tracker's Summary.
	tracker := cost.New(cost.Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 30})
	tracker.Register("local", cost.Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 3})
	tracker.Record("local", 1000, 500)
	tracker.Record("local", 2000, 1000)

	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "ok"}, 0.8)
	root := session.NewRoot("ap-root", "x", "y", "", session.Budget{DepthRemaining: 1})
	res, err := Run(context.Background(), Config{
		Adapter: a, Cost: tracker, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := res.State.CostSummary
	if len(got.Entries) != 2 {
		t.Errorf("CostSummary.Entries = %d, want 2 (mirrors tracker)", len(got.Entries))
	}
	want := tracker.Summary()
	if got.TotalActualUSD != want.TotalActualUSD ||
		got.TotalFrontierUSD != want.TotalFrontierUSD ||
		got.TotalSavingsUSD != want.TotalSavingsUSD {
		t.Errorf("totals mismatch: got %+v want %+v", got, want)
	}
}

func TestRun_CostSummary_ZeroWhenNoTracker(t *testing.T) {
	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "ok"}, 0.8)
	root := session.NewRoot("ap-root", "x", "y", "", session.Budget{DepthRemaining: 1})
	res, err := Run(context.Background(), Config{
		Adapter: a, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.State.CostSummary; len(got.Entries) != 0 ||
		got.TotalActualUSD != 0 || got.TotalFrontierUSD != 0 {
		t.Errorf("CostSummary should be zero without a tracker; got %+v", got)
	}
}

func TestRun_CostSummary_PopulatedEvenOnError(t *testing.T) {
	// If the adapter fails mid-run, the caller still wants the cost so
	// far. We pre-record into the tracker and force an adapter error.
	tracker := cost.New(cost.Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 30})
	tracker.Register("local", cost.Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 3})
	tracker.Record("local", 500, 200)

	boom := errors.New("evaluate exploded")
	a := stub.New("worker").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithEvalError(boom)
	root := session.NewRoot("ap-root", "x", "y", "", session.Budget{DepthRemaining: 1})
	res, err := Run(context.Background(), Config{
		Adapter: a, Cost: tracker, Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want eval error", err)
	}
	if len(res.State.CostSummary.Entries) != 1 {
		t.Errorf("CostSummary on error path: entries = %d, want 1",
			len(res.State.CostSummary.Entries))
	}
}

// --- Judge sampling integration ---

func TestRun_Judge_SamplesUngroundedAndFeedsPolicy(t *testing.T) {
	// Plan emits one process child. Verifier policy is in bootstrap so
	// critique always fires. Judge agrees with the critique → policy
	// should record an agreement.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 1000, SlidingWindow: 100,
		Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal,
	})
	jstub := judge.NewStub("frontier") // agrees by default
	sampler := judge.NewSampler(judge.DefaultSamplePolicy())

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol,
		Judge: jstub, JudgeSampler: sampler,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.JudgeSamplesTaken != 1 {
		t.Errorf("JudgeSamplesTaken = %d, want 1 (un-grounded → 100%%)",
			res.State.JudgeSamplesTaken)
	}
	if res.State.JudgeAgreements != 1 {
		t.Errorf("JudgeAgreements = %d, want 1", res.State.JudgeAgreements)
	}
	if res.State.JudgeDisagreements != 0 || res.State.JudgeErrors != 0 {
		t.Errorf("unexpected disagreement/error counters: %+v", res.State)
	}
	// Policy's window should have one agreement recorded.
	tel := pol.Telemetry()
	if tel.Global.WindowCount != 1 {
		t.Errorf("policy global window count = %d, want 1", tel.Global.WindowCount)
	}
	if jstub.Calls() != 1 {
		t.Errorf("judge calls = %d, want 1", jstub.Calls())
	}
}

func TestRun_Judge_DisagreementRecorded(t *testing.T) {
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 1000, SlidingWindow: 100,
		Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal,
	})
	// Force disagreement: judge flips whatever the local verdict was.
	jstub := judge.NewStub("frontier").
		WithDisagreeWhen(func(judge.Request) bool { return true })

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol, Judge: jstub,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.JudgeDisagreements != 1 {
		t.Errorf("JudgeDisagreements = %d, want 1", res.State.JudgeDisagreements)
	}
	if res.State.JudgeAgreements != 0 {
		t.Errorf("JudgeAgreements = %d, want 0", res.State.JudgeAgreements)
	}
}

func TestRun_Judge_GroundedTargetSampledAtLowerRate(t *testing.T) {
	// Adapter produces a *grounded* process result (Signals.GroundedPass
	// set). Critique fires (bootstrap). Sample rate for grounded is 10%
	// per the lock — across many trials, judge calls should be ~10%.
	groundedPass := true
	var seeds []session.SubAPSeed
	for i := 0; i < 200; i++ {
		seeds = append(seeds, session.SubAPSeed{
			Input: session.Payload{Kind: "task", Content: fmt.Sprintf("step%d", i)},
		})
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)
	groundedAdapter := &groundedProcessAdapter{
		inner:        a,
		groundedPass: &groundedPass,
	}

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 100000, SlidingWindow: 1000,
		Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal,
	})
	jstub := judge.NewStub("frontier")
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: groundedAdapter, Recomposer: rec, VerifierPolicy: pol, Judge: jstub,
		Tracer: trace.Discard{}, Rand: rand.New(rand.NewPCG(7, 7)),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Expect ~10% sampling × 200 children = ~20.
	got := res.State.JudgeSamplesTaken
	if got < 10 || got > 35 {
		t.Errorf("JudgeSamplesTaken = %d, want roughly 10-35 (10%% of 200, jitter)", got)
	}
}

func TestRun_Judge_ErrorIsNonFatal(t *testing.T) {
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 1000, SlidingWindow: 100,
		Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal,
	})
	jstub := judge.NewStub("frontier").
		WithError(errors.New("frontier down"))

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol, Judge: jstub,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run should not fail on judge error: %v", err)
	}
	if res.State.JudgeErrors != 1 {
		t.Errorf("JudgeErrors = %d, want 1", res.State.JudgeErrors)
	}
	if res.State.JudgeAgreements != 0 || res.State.JudgeDisagreements != 0 {
		t.Errorf("error should not be counted as agreement/disagreement: %+v", res.State)
	}
	// Policy should NOT have recorded any agreement.
	if pol.Telemetry().Global.WindowCount != 0 {
		t.Errorf("policy window should be empty after judge error")
	}
}

func TestRun_Judge_NoJudgeSkipsSampling(t *testing.T) {
	// Verifier policy on, but no Judge wired → no samples taken.
	seeds := []session.SubAPSeed{
		{Input: session.Payload{Kind: "task", Content: "a"}},
	}
	a := verifierPolicyAdapter(t).
		WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 1000, SlidingWindow: 100,
		Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal,
	})
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, VerifierPolicy: pol,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.JudgeSamplesTaken != 0 {
		t.Errorf("JudgeSamplesTaken = %d, want 0 (no Judge wired)", res.State.JudgeSamplesTaken)
	}
}

// groundedProcessAdapter wraps a stub.Adapter so process executions
// emit a grounded return_result (Signals.GroundedPass set). Used to
// drive the sampler's grounded-rate path without changing pkg/adapter/stub.
type groundedProcessAdapter struct {
	inner        *stub.Adapter
	groundedPass *bool
}

func (g *groundedProcessAdapter) Name() string { return g.inner.Name() }
func (g *groundedProcessAdapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	return g.inner.Evaluate(ctx, env)
}
// Auto-managed-VRAM tests deleted with the runner refactor (2026-05-22)
// — the runner no longer owns probe/estimator construction. VRAM
// management lives in vram.Governor; see TestRun_Governor_* below and
// pkg/vram/governor_test.go for the unit-level coverage.

func (g *groundedProcessAdapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	env, err := g.inner.Execute(ctx, env)
	if err != nil {
		return env, err
	}
	if env.Execute != nil && env.Execute.Category == session.CategoryReturnResult &&
		env.Execute.ReturnResult != nil &&
		env.Evaluate != nil && env.Evaluate.Playbook == session.PlaybookProcess {
		// Clone before mutating — the stub adapter returns a pointer
		// to a shared ReturnResult, so writing to it from concurrent
		// goroutines races. The clone is local to this Execute call.
		clone := *env.Execute.ReturnResult
		clone.Signals.GroundedPass = g.groundedPass
		env.Execute.ReturnResult = &clone
	}
	return env, nil
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

// --- Concurrency, VRAM, telemetry, MaxInputBytes ---

func planAdapterWithChildren(n int, returnContent string, returnConf float64) (*stub.Adapter, []session.SubAPSeed) {
	seeds := make([]session.SubAPSeed, n)
	for i := 0; i < n; i++ {
		seeds[i] = session.SubAPSeed{Input: session.Payload{Kind: "task", Content: fmt.Sprintf("step%d", i)}}
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
			session.Payload{Kind: "result", Content: returnContent}, returnConf)
	return a, seeds
}

func TestRun_Concurrency_AllChildrenResolve(t *testing.T) {
	a, _ := planAdapterWithChildren(8, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, MaxConcurrency: 4,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.lastChildren) != 8 {
		t.Errorf("recomposer saw %d children, want 8", len(rec.lastChildren))
	}
	if res.State.ConcurrentDispatches != 1 {
		t.Errorf("ConcurrentDispatches = %d, want 1", res.State.ConcurrentDispatches)
	}
}

func TestRun_Concurrency_StrictCancellationOnInhibit(t *testing.T) {
	// 6 children. The inhibitor aborts one of them. Under strict
	// cancellation, in-flight siblings get cancelled via context; the
	// parent ends up inhibited.
	seeds := make([]session.SubAPSeed, 6)
	for i := range seeds {
		seeds[i] = session.SubAPSeed{Input: session.Payload{Kind: "task", Content: fmt.Sprintf("step%d", i)}}
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
			session.Payload{Kind: "result", Content: "x"}, 0.8)

	inh := &recordingInhibitor{abortOnID: "ap-root-c1"}
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Inhibitor: inh,
		MaxConcurrency: 4,
		Tracer:         trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Root.IsInhibited() {
		t.Errorf("root should be inhibited; got ExitReason=%q", res.Root.ExitReason)
	}
	if rec.calls != 0 {
		t.Errorf("recomposer must NOT run on inhibited subtree; got %d", rec.calls)
	}
}

func TestRun_Concurrency_RaceDetectorCleanWithVerifierAndJudge(t *testing.T) {
	// Concurrent dispatch with verifier policy + judge + cost tracker
	// all wired on. The assertions confirm telemetry aggregates across
	// goroutines without races (run with `go test -race`).
	a := verifierPolicyAdapter(t)
	seeds := make([]session.SubAPSeed, 6)
	for i := range seeds {
		seeds[i] = session.SubAPSeed{Input: session.Payload{Kind: "task", Content: fmt.Sprintf("s%d", i)}}
	}
	a = a.WithEmitSubtree(session.PlaybookPlan, session.RecomposeSequential, seeds...)

	pol := verifier.New(verifier.Config{
		BootstrapThreshold: 100, SlidingWindow: 100,
		Floor: 0.15, ConfidenceLevel: 0.95, HappinessScope: verifier.ScopeGlobal,
	})
	jstub := judge.NewStub("frontier")
	tracker := cost.New(cost.Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 30})
	tracker.Register("hermes", cost.Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 3})

	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec,
		VerifierPolicy: pol, Judge: jstub, Cost: tracker,
		MaxConcurrency: 3,
		Tracer:         trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := res.State.PolicyCritiquesEmitted; got != 6 {
		t.Errorf("PolicyCritiquesEmitted = %d, want 6", got)
	}
	if got := len(res.State.VerifierSignals); got != 6 {
		t.Errorf("VerifierSignals = %d, want 6", got)
	}
	if got := res.State.JudgeSamplesTaken; got != 6 {
		t.Errorf("JudgeSamplesTaken = %d, want 6", got)
	}
	if got := res.State.JudgeAgreements + res.State.JudgeDisagreements; got != 6 {
		t.Errorf("agreements+disagreements = %d, want 6", got)
	}
}

// TestRun_Concurrency_DynamicCap*, ProbeFailure*: deleted with the
// runner refactor (2026-05-22). The runner no longer owns VRAM probe
// or estimator — those moved entirely into vram.Governor. See
// TestRun_Governor_* and pkg/vram/governor_test.go.

func TestRun_MaxInputBytes_InhibitsOversizedInput(t *testing.T) {
	a := stub.New("agent").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess, session.Payload{Kind: "result", Content: "ok"}, 0.8)
	big := strings.Repeat("x", 5000)
	root := session.NewRoot("ap-root", big, "{r}", "", session.Budget{DepthRemaining: 1})
	res, err := Run(context.Background(), Config{
		Adapter: a, MaxInputBytes: 1000,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Root.IsInhibited() {
		t.Errorf("oversized input should inhibit; got ExitReason=%q", res.Root.ExitReason)
	}
	if res.State.InhibitCount == 0 {
		t.Errorf("InhibitCount should be >0")
	}
}

func TestRun_Telemetry_PerPlaybookTokens(t *testing.T) {
	a, _ := planAdapterWithChildren(3, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec,
		Tracer: trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.PerPlaybookTokens == nil {
		t.Fatal("PerPlaybookTokens is nil; expected populated map")
	}
	// Evaluate tokens (empty key) come from the stub adapter's default
	// of 40 per call. Execute tokens are zero by default in the stub —
	// the assertion here is "evaluate telemetry plumbed through under
	// concurrency-safe aggregation," not that every playbook has data.
	if got := res.State.PerPlaybookTokens[""]; got == 0 {
		t.Errorf("evaluate tokens (empty key) = 0, expected >0")
	}
}

// fakeVRAMProbe implements vram.Probe for tests. Safe for concurrent
// use — the governor and runner both call Probe from goroutines.
type fakeVRAMProbe struct {
	mu     sync.Mutex
	report vram.Report
	err    error
	calls  int
}

func (p *fakeVRAMProbe) Name() string { return "fake" }
func (p *fakeVRAMProbe) Probe(_ context.Context) (vram.Report, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return vram.Report{}, p.err
	}
	return p.report, nil
}

// --- Governor wiring ---

// testRunnerModelSpec builds a small valid ModelSpec for runner
// integration tests. Per-call estimate is tiny so an abundant probe
// reading easily fits many concurrent leases.
func testRunnerModelSpec() vram.ModelSpec {
	return vram.ModelSpec{
		Name: "runner-test", Layers: 1, KVHiddenDim: 1, KVDtypeBytes: 2, ContextSize: 1, PrefixTokens: 1,
	}
}

func TestRun_Governor_HappyPath(t *testing.T) {
	// Governor wired with ceiling=4. A plan with 8 children should
	// resolve all 8 with concurrent dispatch capped at the ceiling.
	probe := &fakeVRAMProbe{report: vram.Report{Source: "fake", AvailableBytes: 64 * 1024 * 1024 * 1024}}
	g, err := vram.NewGovernor(vram.GovernorConfig{
		Model:         testRunnerModelSpec(),
		Ceiling:       4,
		ProbeOverride: probe,
	})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}

	a, _ := planAdapterWithChildren(8, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec,
		Governor: g,
		Tracer:   trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.lastChildren) != 8 {
		t.Errorf("recomposer saw %d children, want 8", len(rec.lastChildren))
	}
	if res.State.ConcurrentDispatches == 0 {
		t.Errorf("expected concurrent dispatch with ceiling=4 and 8 siblings")
	}
	snap := g.Snapshot(context.Background())
	if snap.ActiveLeases != 0 {
		t.Errorf("ActiveLeases after Run = %d, want 0", snap.ActiveLeases)
	}
	if snap.QueuedWaiters != 0 {
		t.Errorf("QueuedWaiters after Run = %d, want 0", snap.QueuedWaiters)
	}
}

func TestRun_Governor_DepthDeeperThanCeiling_NoDeadlock(t *testing.T) {
	// Governor with ceiling=1, recursive plan tree. The per-resolve
	// Acquire/Release-before-descend contract is what prevents
	// deadlock — holding a lease across descend would have a depth-2
	// tree wait forever on itself.
	probe := &fakeVRAMProbe{report: vram.Report{Source: "fake", AvailableBytes: 1024 * 1024 * 1024}}
	g, err := vram.NewGovernor(vram.GovernorConfig{
		Model:         testRunnerModelSpec(),
		Ceiling:       1, // strict — exposes any lease-stacking bug
		ProbeOverride: probe,
	})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}

	a, _ := planAdapterWithChildren(2, "leaf", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = Run(ctx, Config{
		Adapter: a, Recomposer: rec,
		Governor: g,
		Tracer:   trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v (deadlock would surface as DeadlineExceeded)", err)
	}
	if len(rec.lastChildren) != 2 {
		t.Errorf("got %d children, want 2", len(rec.lastChildren))
	}
}

func TestRun_Governor_RespectsExplicitMaxConcurrency(t *testing.T) {
	// Operator wires both a Governor (ceiling=8) and an explicit
	// MaxConcurrency=2. The tighter cap wins for the wave dispatch.
	probe := &fakeVRAMProbe{report: vram.Report{Source: "fake", AvailableBytes: 64 * 1024 * 1024 * 1024}}
	g, err := vram.NewGovernor(vram.GovernorConfig{
		Model:         testRunnerModelSpec(),
		Ceiling:       8,
		ProbeOverride: probe,
	})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}

	a, _ := planAdapterWithChildren(6, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter:        a, Recomposer: rec,
		Governor:       g,
		MaxConcurrency: 2,
		Tracer:         trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.ConcurrentDispatches == 0 {
		t.Errorf("expected concurrent dispatch")
	}
	if len(rec.lastChildren) != 6 {
		t.Errorf("got %d children, want 6", len(rec.lastChildren))
	}
}

func TestRun_Governor_StrictSerialHonored(t *testing.T) {
	// Even with a Governor allowing concurrency, MaxConcurrency=1 must
	// still mean strict serial.
	probe := &fakeVRAMProbe{report: vram.Report{Source: "fake", AvailableBytes: 64 * 1024 * 1024 * 1024}}
	g, err := vram.NewGovernor(vram.GovernorConfig{
		Model:         testRunnerModelSpec(),
		Ceiling:       8,
		ProbeOverride: probe,
	})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}

	a, _ := planAdapterWithChildren(4, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	res, err := Run(context.Background(), Config{
		Adapter:        a, Recomposer: rec,
		Governor:       g,
		MaxConcurrency: 1,
		Tracer:         trace.Discard{}, Rand: seededRand(),
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.State.ConcurrentDispatches != 0 {
		t.Errorf("MaxConcurrency=1 must never dispatch concurrently; got %d waves",
			res.State.ConcurrentDispatches)
	}
}

// recordingTracerEvents captures trace event names so tests can assert
// what was emitted without inspecting stderr. Implements trace.Tracer.
type recordingTracerEvents struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	level slog.Level
	name  string
}

func (t *recordingTracerEvents) Event(_ context.Context, level slog.Level, name string, _ ...slog.Attr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, recordedEvent{level, name})
}

func (t *recordingTracerEvents) has(name string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.events {
		if e.name == name {
			return true
		}
	}
	return false
}

func TestRunner_AutoWiresGovernorOnZeroMaxConcurrency(t *testing.T) {
	// Library auto-manages concurrency (LOCKED 2026-05-21): when
	// MaxConcurrency=0 and Governor=nil, the runner constructs an
	// auto-governor at Run start. We detect that via the
	// runner.auto_governor_wired trace event.
	a, _ := planAdapterWithChildren(3, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	tracer := &recordingTracerEvents{}
	_, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: tracer, Rand: seededRand(),
		// No Governor, no MaxConcurrency — library-managed path.
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !tracer.has("runner.auto_governor_wired") {
		t.Errorf("expected runner.auto_governor_wired trace event under library-managed mode")
	}
	if len(rec.lastChildren) != 3 {
		t.Errorf("got %d children, want 3", len(rec.lastChildren))
	}
}

func TestRunner_MaxConcurrencyOneIsStrictSerial_NoAutoGovernor(t *testing.T) {
	// Operator explicitly opts out of concurrency via MaxConcurrency=1.
	// The runner must NOT auto-construct a governor in that mode — the
	// auto-governor is only for the zero-value path.
	a, _ := planAdapterWithChildren(3, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	tracer := &recordingTracerEvents{}
	_, err := Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: tracer, Rand: seededRand(),
		MaxConcurrency: 1,
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if tracer.has("runner.auto_governor_wired") {
		t.Errorf("MaxConcurrency=1 must NOT auto-construct a governor")
	}
}

func TestRunner_ExplicitGovernorNotOverridden(t *testing.T) {
	// Operator-supplied Governor wins even under MaxConcurrency=0. The
	// auto-governor must not replace it.
	probe := &fakeVRAMProbe{report: vram.Report{Source: "fake", AvailableBytes: 64 * 1024 * 1024 * 1024}}
	g, err := vram.NewGovernor(vram.GovernorConfig{
		Model:         testRunnerModelSpec(),
		Ceiling:       4,
		ProbeOverride: probe,
	})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}
	a, _ := planAdapterWithChildren(3, "x", 0.8)
	rec := &fakeRecomposer{}
	root := session.NewRoot("ap-root", "go", "{r}", "", session.Budget{DepthRemaining: 3})
	tracer := &recordingTracerEvents{}
	_, err = Run(context.Background(), Config{
		Adapter: a, Recomposer: rec, Tracer: tracer, Rand: seededRand(),
		Governor: g, // explicit governor, MaxConcurrency=0
	}, root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if tracer.has("runner.auto_governor_wired") {
		t.Errorf("explicit Governor must not be replaced by AutoGovernor")
	}
	// And we should not have probed for fallback either (operator
	// owns probe-failure semantics through their own config).
	if tracer.has("runner.probe_failed_serial_fallback") {
		t.Errorf("operator-supplied Governor must not trigger autoSerialFallback")
	}
}

func TestRunner_ProbeFailure_FallsBackToSerial(t *testing.T) {
	// Auto-managed governor whose probe returns an error → the runner
	// downshifts the wave cap to 1 (serial). Build the governor with a
	// ProbeOverride that fails so we don't depend on the platform's
	// auto-detected probes.
	failingProbe := &fakeVRAMProbe{err: errors.New("probe nope")}
	g, err := vram.NewGovernor(vram.GovernorConfig{
		Model:         vram.DefaultVRAMModel(),
		Ceiling:       vram.MaxConcurrencyCeiling,
		ProbeOverride: failingProbe,
	})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}
	// MaxConcurrency=0 with the operator-supplied governor would NOT
	// normally trigger probeHealthyOnce (that path is reserved for
	// auto-wired governors). Construct the runner directly to test the
	// fallback in isolation.
	r := &runner{
		cfg: Config{
			Adapter: nil, Tracer: trace.Discard{}, Governor: g, MaxConcurrency: 0,
		},
		state:   &RunState{},
		subtree: map[session.ID][]session.Envelope{},
	}
	r.probeHealthyOnce(context.Background())
	if !r.autoSerialFallback {
		t.Fatal("expected probe failure to set autoSerialFallback")
	}
	if got := r.computeConcurrency(context.Background(), 8); got != 1 {
		t.Errorf("autoSerialFallback should collapse wave cap to 1, got %d", got)
	}
}
