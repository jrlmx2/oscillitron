// Package stub provides a no-network Adapter for tests and the demo
// under the uniform-node + evaluate/execute model.
//
// The stub is configurable per-playbook: Evaluate picks a playbook
// according to a caller-provided rule (or a default), and Execute
// returns the response pre-configured for that playbook. Useful for
// stitching together specific call-tree shapes in tests without
// running real LLM calls.
package stub

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Evaluator is the rule the stub uses to pick a playbook on Evaluate.
// Tests can override via WithEvaluator. Default is DefaultEvaluator,
// which picks based on Input.Kind.
type Evaluator func(env session.Envelope) (session.Playbook, float64)

// DefaultEvaluator picks PlaybookProcess unconditionally. Plenty of
// tests don't care which playbook is picked; the ones that do supply
// their own Evaluator.
func DefaultEvaluator(env session.Envelope) (session.Playbook, float64) {
	return session.PlaybookProcess, 0.8
}

// ExecuteResponse is the Execute-step payload the stub returns for a
// given playbook. Exactly one of EmitSubtree / ReturnResult /
// VerifierSignal must be populated; Category should match.
type ExecuteResponse struct {
	Category       session.Category
	EmitSubtree    *session.EmitSubtreePayload
	ReturnResult   *session.ReturnResultPayload
	VerifierSignal *session.VerifierSignalPayload
	ExitReason     session.ExitReason // defaults to ExitDone if zero
	TokensUsed     int
}

// Adapter is a deterministic, configurable stub.
type Adapter struct {
	name      string
	evaluator Evaluator
	responses map[session.Playbook]ExecuteResponse

	evalErr    error
	executeErr error

	calls            atomic.Int64
	evalCalls        atomic.Int64
	executeCalls     atomic.Int64
	playbookCounts   map[session.Playbook]*atomic.Int64
	playbookCountsMu chan struct{} // simple mutex-as-channel for the lazy-init
}

// New constructs a stub. Without further configuration it picks
// PlaybookProcess and returns an empty return_result on Execute.
func New(name string) *Adapter {
	return &Adapter{
		name:             name,
		evaluator:        DefaultEvaluator,
		responses:        map[session.Playbook]ExecuteResponse{},
		playbookCounts:   map[session.Playbook]*atomic.Int64{},
		playbookCountsMu: make(chan struct{}, 1),
	}
}

// WithEvaluator overrides the playbook-pick rule.
func (a *Adapter) WithEvaluator(e Evaluator) *Adapter { a.evaluator = e; return a }

// WithDefaultPlaybook is a convenience for tests that always want the
// same playbook picked regardless of envelope content.
func (a *Adapter) WithDefaultPlaybook(p session.Playbook) *Adapter {
	a.evaluator = func(session.Envelope) (session.Playbook, float64) { return p, 0.9 }
	return a
}

// WithEmitSubtree configures the Execute response for the given
// playbook to emit the supplied sub-APs with the supplied recompose
// spec.
func (a *Adapter) WithEmitSubtree(p session.Playbook, recompose session.RecomposeSpec, seeds ...session.SubAPSeed) *Adapter {
	a.responses[p] = ExecuteResponse{
		Category: session.CategoryEmitSubtree,
		EmitSubtree: &session.EmitSubtreePayload{
			SubAPs:    append([]session.SubAPSeed(nil), seeds...),
			Recompose: recompose,
		},
	}
	return a
}

// WithReturnResult configures the Execute response for the given
// playbook to return a value up the tree.
func (a *Adapter) WithReturnResult(p session.Playbook, result session.Payload, confidence float64) *Adapter {
	a.responses[p] = ExecuteResponse{
		Category: session.CategoryReturnResult,
		ReturnResult: &session.ReturnResultPayload{
			Result:     result,
			Confidence: confidence,
		},
	}
	return a
}

// WithVerifierSignal configures the Execute response for the given
// playbook to emit a verifier signal.
func (a *Adapter) WithVerifierSignal(p session.Playbook, verdict session.Verdict, issues ...session.Issue) *Adapter {
	a.responses[p] = ExecuteResponse{
		Category: session.CategoryVerifierSignal,
		VerifierSignal: &session.VerifierSignalPayload{
			Verdict: verdict,
			Issues:  append([]session.Issue(nil), issues...),
		},
	}
	return a
}

// WithExitReason overrides the ExitReason for a specific playbook's
// Execute response (default ExitDone). Use to simulate budget-exhausted
// runs in tests.
func (a *Adapter) WithExitReason(p session.Playbook, reason session.ExitReason) *Adapter {
	r := a.responses[p]
	r.ExitReason = reason
	a.responses[p] = r
	return a
}

// WithEvalError causes Evaluate to fail with the supplied error.
func (a *Adapter) WithEvalError(err error) *Adapter { a.evalErr = err; return a }

// WithExecuteError causes Execute to fail with the supplied error.
func (a *Adapter) WithExecuteError(err error) *Adapter { a.executeErr = err; return a }

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return a.name }

// Calls reports total Evaluate+Execute call count.
func (a *Adapter) Calls() int64 { return a.calls.Load() }

// EvalCalls reports how many times Evaluate was invoked.
func (a *Adapter) EvalCalls() int64 { return a.evalCalls.Load() }

// ExecuteCalls reports how many times Execute was invoked.
func (a *Adapter) ExecuteCalls() int64 { return a.executeCalls.Load() }

// CallsForPlaybook reports how many Execute calls produced the given
// playbook's response.
func (a *Adapter) CallsForPlaybook(p session.Playbook) int64 {
	if c, ok := a.playbookCounts[p]; ok {
		return c.Load()
	}
	return 0
}

// Evaluate implements adapter.Adapter.
func (a *Adapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	a.calls.Add(1)
	a.evalCalls.Add(1)
	if err := ctx.Err(); err != nil {
		return env, err
	}
	if a.evalErr != nil {
		return env, a.evalErr
	}
	pb, conf := a.evaluator(env)
	env.Evaluate = &session.Evaluate{
		Playbook:   pb,
		Rationale:  fmt.Sprintf("stub picked %s for input.kind=%q", pb, env.Input.Kind),
		Confidence: conf,
		TokensUsed: 40,
	}
	return env, nil
}

// Execute implements adapter.Adapter.
func (a *Adapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	a.calls.Add(1)
	a.executeCalls.Add(1)
	if err := ctx.Err(); err != nil {
		return env, err
	}
	if a.executeErr != nil {
		return env, a.executeErr
	}
	if env.Evaluate == nil {
		return env, errors.New("stub: Execute called before Evaluate")
	}
	pb := env.Evaluate.Playbook
	a.incrPlaybookCount(pb)

	resp, ok := a.responses[pb]
	if !ok {
		// Default response: empty return_result. Keeps the runner happy
		// without forcing every test to configure every playbook.
		resp = ExecuteResponse{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result:     session.Payload{Kind: "result", Content: fmt.Sprintf("[%s] handled %q", a.name, env.Input.Content)},
				Confidence: 0.5,
			},
		}
	}
	exitReason := resp.ExitReason
	if exitReason == "" {
		exitReason = session.ExitDone
	}
	env.Execute = &session.Execute{
		Category:       resp.Category,
		EmitSubtree:    resp.EmitSubtree,
		ReturnResult:   resp.ReturnResult,
		VerifierSignal: resp.VerifierSignal,
		TokensUsed:     resp.TokensUsed,
	}
	env.ExitReason = exitReason
	return env, nil
}

func (a *Adapter) incrPlaybookCount(p session.Playbook) {
	// Lazy init under a coarse lock; this isn't on the hot path.
	a.playbookCountsMu <- struct{}{}
	c, ok := a.playbookCounts[p]
	if !ok {
		c = new(atomic.Int64)
		a.playbookCounts[p] = c
	}
	<-a.playbookCountsMu
	c.Add(1)
}

// Compile-time check.
var _ adapter.Adapter = (*Adapter)(nil)
