// CLAUDE GENERATED
// Package runner walks the AP call tree under the uniform-node +
// evaluate/execute model.
//
// Given a root envelope, Run:
//
//  1. Calls adapter.Evaluate to pick a Playbook, then adapter.Execute
//     to run it.
//  2. Branches on the Execute.Category:
//     - return_result   → payload bubbles up to the caller.
//     - verifier_signal → recorded in RunState, NOT propagated as a
//     child payload to a recompose step (per the locked
//     "verifier signals go to the runtime, not the next AP" rule).
//     - emit_subtree    → constructs child envelopes from the
//     SubAPSeeds, dispatches them in *randomized* sibling order,
//     recurses synchronously into each, collects the children's
//     resolved return_result payloads, and calls recomposer with
//     the plan's RecomposeSpec to produce a synthesized result for
//     this subtree.
//  3. On every parent→child traversal (root has no incoming edge and
//     is never checked), invokes the configured Inhibitor with a full
//     Edge (Parent, Child, Path). On Abort or Restart, marks the child
//     subtree ExitInhibited and short-circuits the parent (no partial
//     recomposition; strict semantics). Restart→Abort downgrade is
//     still in effect — v0 has no checkpointing.
//
// Sync, no goroutines. v0 honours the randomized-sibling-dispatch lock
// without sibling parallelism; the random pop order keeps the v0
// baseline honest about not relying on emission order.
//
// Budget: parent's DepthRemaining is decremented by 1 on each child;
// TokensRemaining is unchanged (split policy is an open subquestion
// per the JSON envelope sketch). MaxDepth on Config is a belt-and-
// suspenders absolute path-length cap, independent of per-AP
// DepthRemaining.
//
// Recomposer: the runner delegates recomposition of children's
// return_result payloads to pkg/recomposer. While Stage 4 is in
// flight, callers that want a runnable end-to-end path should pass a
// recomposer that does *not* return ErrStagePending — the runner
// surfaces ErrStagePending as a clean failure (it does not silently
// fall back).
//
// Compose-as-a-dispatched-AP (with scope channels and deferred
// dispatch order) is a v1 concern. Stage 3 leaves the compose
// playbook category supported at the adapter level (an adapter may
// emit a return_result on a compose Execute), but the call-tree
// orchestration of pairwise reduction is owned by the recomposer.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/verifier"
)

// Errors surfaced by Run.
var (
	// ErrAdapterRequired is returned when Config.Adapter is nil.
	ErrAdapterRequired = errors.New("runner: Adapter is required")
	// ErrRecomposerRequired is returned when Config.Recomposer is nil
	// and the tree contains at least one emit_subtree AP.
	ErrRecomposerRequired = errors.New("runner: Recomposer is required for emit_subtree APs")
	// ErrInvalidExecuteCategory is returned when an adapter produces an
	// Execute payload whose Category doesn't match its filled fields.
	ErrInvalidExecuteCategory = errors.New("runner: invalid Execute payload (category/fields mismatch)")
)

// Config bundles the tree-walker's dependencies.
type Config struct {
	// Adapter runs Evaluate and Execute for every AP. Required.
	Adapter adapter.Adapter
	// Inhibitor is consulted on every parent→child edge. If nil, no
	// inhibition is applied (every edge is treated as Continue).
	Inhibitor inhibitor.Inhibitor
	// Recomposer collapses N children's return_result payloads into
	// the parent's synthesized payload. Required iff the tree
	// contains any emit_subtree APs.
	Recomposer recomposer.Recomposer
	// Tracer emits structured events. Defaults to trace.Discard{}.
	Tracer trace.Tracer
	// MaxDepth is an absolute cap on Path length (belt-and-suspenders
	// alongside per-AP Budget.DepthRemaining). 0 means "no cap from
	// the runner; per-AP budget is the only ceiling."
	MaxDepth int
	// Rand provides the randomized sibling-dispatch order. If nil, a
	// fresh non-deterministic *rand.Rand is created per Run call.
	// Tests pass a seeded *rand.Rand for reproducibility.
	Rand *rand.Rand
	// ChildIDFn assigns an ID to a fresh child envelope. Required for
	// trees that emit sub-APs. Defaults to a sequential helper if nil.
	ChildIDFn func(parent session.Envelope, seedIndex int) session.ID
	// VerifierPolicy decides whether to inject a critique AP on each
	// return_result child of an emit_subtree plan. If nil, no critiques
	// are auto-emitted (only adapter-emitted critique APs run). The
	// parent override (child seed's NeedsVerification == true) is honored
	// only when a policy is configured; without a policy, the override is
	// a no-op since there is no runtime to react to it. The policy is
	// locked in design 2026-05-20; see pkg/verifier.
	VerifierPolicy *verifier.Policy
}

// Result is the return value of Run.
type Result struct {
	// Root is the resolved root envelope with all Evaluate/Execute
	// fields populated. For emit_subtree roots, the SubAPSeeds inside
	// Execute.EmitSubtree are reflected as-emitted; the resolved
	// children are reachable via Subtree (a parallel structure that
	// avoids bloating the lean envelope with child copies).
	Root session.Envelope
	// ResolvedPayload is what a hypothetical parent of the root would
	// see if the root were itself a child: the return_result payload
	// for a return_result root, the recomposed payload for an
	// emit_subtree root, or a zero value for a verifier_signal root.
	ResolvedPayload session.ReturnResultPayload
	// Subtree mirrors the tree shape with resolved child envelopes,
	// keyed by AP ID. Useful for tests and traces.
	Subtree map[session.ID][]session.Envelope
	// State accumulates runtime-level signals (verifier verdicts, call
	// counts) per the "verifier signals go to the runtime, not the
	// next AP" lock.
	State RunState
}

// RunState is the runtime's per-Run scratchpad. Verifier signals
// accumulate here (rather than on the next AP's envelope), per the
// locked "verifier_signal flows to the runtime" rule.
type RunState struct {
	VerifierSignals []VerifierSignalRecord
	EvaluateCount   int
	ExecuteCount    int
	InhibitCount    int
	// PolicyCritiquesEmitted counts critique APs the runner injected
	// because the verifier policy (or parent override) said so. Distinct
	// from VerifierSignals (which also counts adapter-emitted critique
	// APs) and useful for asserting the phase ramp drove the rate.
	PolicyCritiquesEmitted int
}

// VerifierSignalRecord is one critique / verify_grounded outcome
// captured by the runtime.
type VerifierSignalRecord struct {
	APID    session.ID
	Verdict session.Verdict
	Issues  []session.Issue
}

// Run walks the call tree from root and returns the Result.
func Run(ctx context.Context, cfg Config, root session.Envelope) (Result, error) {
	if cfg.Adapter == nil {
		return Result{}, ErrAdapterRequired
	}
	if cfg.Tracer == nil {
		cfg.Tracer = trace.Discard{}
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	if cfg.ChildIDFn == nil {
		cfg.ChildIDFn = defaultChildIDFn()
	}

	r := &runner{
		cfg:     cfg,
		state:   &RunState{},
		subtree: map[session.ID][]session.Envelope{},
	}

	resolved, payload, err := r.resolve(ctx, root, nil, nil)
	if err != nil {
		return Result{Root: resolved, State: *r.state, Subtree: r.subtree}, err
	}
	return Result{
		Root:            resolved,
		ResolvedPayload: payload,
		Subtree:         r.subtree,
		State:           *r.state,
	}, nil
}

type runner struct {
	cfg     Config
	state   *RunState
	subtree map[session.ID][]session.Envelope
}

// resolve recurses on a single AP. parentEnv and parentChain are the
// caller's context: nil at the root, non-nil for any descendant.
// parentChain is the full root→parent slice the inhibitor reads.
func (r *runner) resolve(
	ctx context.Context,
	env session.Envelope,
	parentEnv *session.Envelope,
	parentChain []session.Envelope,
) (session.Envelope, session.ReturnResultPayload, error) {
	if err := ctx.Err(); err != nil {
		return env, session.ReturnResultPayload{}, err
	}

	// Path-length cap. Belt-and-suspenders alongside per-AP budget.
	if cfg := r.cfg.MaxDepth; cfg > 0 && env.Depth() > cfg {
		env.ExitReason = session.ExitInhibited
		trace.Error(r.cfg.Tracer, ctx, "runner.max_depth_exceeded",
			slog.String("ap_id", string(env.ID)),
			slog.Int("depth", env.Depth()),
			slog.Int("max", cfg),
		)
		r.state.InhibitCount++
		return env, session.ReturnResultPayload{}, nil
	}

	// Evaluate step.
	evalEnv, err := r.cfg.Adapter.Evaluate(ctx, env)
	if err != nil {
		evalEnv.ExitReason = session.ExitInhibited
		trace.Error(r.cfg.Tracer, ctx, "runner.evaluate_error",
			slog.String("ap_id", string(env.ID)),
			slog.String("err", err.Error()),
		)
		return evalEnv, session.ReturnResultPayload{}, err
	}
	r.state.EvaluateCount++
	env = evalEnv
	trace.Info(r.cfg.Tracer, ctx, "runner.evaluated",
		slog.String("ap_id", string(env.ID)),
		slog.String("playbook", string(env.Evaluate.Playbook)),
	)

	// Execute step.
	execEnv, err := r.cfg.Adapter.Execute(ctx, env)
	if err != nil {
		execEnv.ExitReason = session.ExitInhibited
		trace.Error(r.cfg.Tracer, ctx, "runner.execute_error",
			slog.String("ap_id", string(env.ID)),
			slog.String("playbook", string(env.Evaluate.Playbook)),
			slog.String("err", err.Error()),
		)
		return execEnv, session.ReturnResultPayload{}, err
	}
	r.state.ExecuteCount++
	env = execEnv

	if env.Execute == nil {
		return env, session.ReturnResultPayload{}, fmt.Errorf("%w: adapter returned nil Execute", ErrInvalidExecuteCategory)
	}
	trace.Info(r.cfg.Tracer, ctx, "runner.executed",
		slog.String("ap_id", string(env.ID)),
		slog.String("category", string(env.Execute.Category)),
	)

	// Inhibitor (parent→child edge). Root has no parent and is never
	// checked. If parent passed, we have the full chain incl. this AP.
	if parentEnv != nil && r.cfg.Inhibitor != nil {
		chain := append([]session.Envelope(nil), parentChain...)
		chain = append(chain, env)
		edge := inhibitor.Edge{
			Parent: parentEnv,
			Child:  env,
			Path:   chain,
		}
		verdict := r.cfg.Inhibitor.Check(edge)
		switch verdict.Decision {
		case inhibitor.Abort, inhibitor.Restart:
			// Restart→Abort downgrade: v0 has no checkpointing.
			env.ExitReason = session.ExitInhibited
			r.state.InhibitCount++
			trace.Error(r.cfg.Tracer, ctx, "runner.inhibited",
				slog.String("ap_id", string(env.ID)),
				slog.String("decision", decisionString(verdict.Decision)),
				slog.String("reason", verdict.Reason),
			)
			return env, session.ReturnResultPayload{}, nil
		}
	}

	// Branch on Execute.Category.
	switch env.Execute.Category {
	case session.CategoryReturnResult:
		if env.Execute.ReturnResult == nil {
			return env, session.ReturnResultPayload{}, fmt.Errorf("%w: return_result with nil payload", ErrInvalidExecuteCategory)
		}
		return env, *env.Execute.ReturnResult, nil

	case session.CategoryVerifierSignal:
		if env.Execute.VerifierSignal == nil {
			return env, session.ReturnResultPayload{}, fmt.Errorf("%w: verifier_signal with nil payload", ErrInvalidExecuteCategory)
		}
		// Verifier signals go to the runtime, not the next AP.
		r.state.VerifierSignals = append(r.state.VerifierSignals, VerifierSignalRecord{
			APID:    env.ID,
			Verdict: env.Execute.VerifierSignal.Verdict,
			Issues:  env.Execute.VerifierSignal.Issues,
		})
		trace.Info(r.cfg.Tracer, ctx, "runner.verifier_signal",
			slog.String("ap_id", string(env.ID)),
			slog.String("verdict", string(env.Execute.VerifierSignal.Verdict)),
		)
		// No payload to bubble up.
		return env, session.ReturnResultPayload{}, nil

	case session.CategoryEmitSubtree:
		if env.Execute.EmitSubtree == nil {
			return env, session.ReturnResultPayload{}, fmt.Errorf("%w: emit_subtree with nil payload", ErrInvalidExecuteCategory)
		}
		return r.descend(ctx, env, parentChain)

	default:
		return env, session.ReturnResultPayload{}, fmt.Errorf("%w: unknown category %q", ErrInvalidExecuteCategory, env.Execute.Category)
	}
}

// descend dispatches an emit_subtree AP's children, recurses into
// each, collects return_result payloads, and recomposes per the
// plan's RecomposeSpec.
func (r *runner) descend(
	ctx context.Context,
	plan session.Envelope,
	parentChain []session.Envelope,
) (session.Envelope, session.ReturnResultPayload, error) {
	seeds := plan.Execute.EmitSubtree.SubAPs
	if len(seeds) == 0 {
		// Empty subtree — recomposer has nothing to do. Treat as
		// no-bubble (caller's recomposer skips this branch).
		return plan, session.ReturnResultPayload{}, nil
	}

	// Budget: child's DepthRemaining = parent's - 1.
	if plan.Budget.DepthRemaining <= 0 {
		plan.ExitReason = session.ExitBudgetExhausted
		trace.Error(r.cfg.Tracer, ctx, "runner.depth_budget_exhausted",
			slog.String("ap_id", string(plan.ID)),
		)
		return plan, session.ReturnResultPayload{}, nil
	}
	childBudget := plan.Budget
	childBudget.DepthRemaining--

	// Scope handle for this subtree (one scope per plan AP).
	scope := session.ScopeHandle("scope-" + string(plan.ID))

	// Build child envelopes in emission order.
	children := make([]session.Envelope, len(seeds))
	for i, seed := range seeds {
		childID := r.cfg.ChildIDFn(plan, i)
		children[i] = session.NewChild(&plan, seed, childID, scope, childBudget)
	}

	// Randomize dispatch order. Sibling dispatch is randomized
	// (LOCKED 2026-05-19) — keeps v0 baseline honest about not
	// relying on emission order.
	order := r.cfg.Rand.Perm(len(children))

	// Recurse into each child, collecting return_result payloads.
	chainForChildren := append([]session.Envelope(nil), parentChain...)
	chainForChildren = append(chainForChildren, plan)

	resolvedChildren := make([]session.Envelope, len(children))
	bubbledPayloads := make([]session.ReturnResultPayload, 0, len(children))
	for _, idx := range order {
		resolved, payload, err := r.resolve(ctx, children[idx], &plan, chainForChildren)
		resolvedChildren[idx] = resolved
		if err != nil {
			// Surface child error; mark parent inhibited.
			plan.ExitReason = session.ExitInhibited
			r.subtree[plan.ID] = resolvedChildren
			return plan, session.ReturnResultPayload{}, err
		}
		if resolved.IsInhibited() {
			// Strict semantics: one inhibited child inhibits the parent.
			plan.ExitReason = session.ExitInhibited
			r.subtree[plan.ID] = resolvedChildren
			trace.Error(r.cfg.Tracer, ctx, "runner.parent_inhibited_by_child",
				slog.String("plan_id", string(plan.ID)),
				slog.String("child_id", string(resolved.ID)),
			)
			return plan, session.ReturnResultPayload{}, nil
		}
		// Only return_result children contribute to recomposition.
		// verifier_signal children are recorded in RunState; empty
		// emit_subtree children contribute nothing.
		if resolved.Execute != nil && resolved.Execute.Category != session.CategoryVerifierSignal {
			// Empty zero-payload check: payload.Result is the bubble.
			if !isZeroPayload(payload) {
				bubbledPayloads = append(bubbledPayloads, payload)
			}
		}

		// Verifier policy: after a return_result resolves, optionally
		// inject a critique sibling. Skipped for emit_subtree (its
		// recomposed bubble is what would be critiqued — a v1 concern)
		// and verifier_signal (no result to critique). Per the locked
		// design, the critique runs in the *same scope* as the AP it
		// critiques; its verifier_signal lands in RunState and does not
		// reach the recomposer.
		if resolved.Execute != nil && resolved.Execute.Category == session.CategoryReturnResult && !resolved.IsInhibited() {
			critiqued, cerr := r.maybeInjectCritique(ctx, plan, resolved, scope, childBudget, chainForChildren)
			if cerr != nil {
				plan.ExitReason = session.ExitInhibited
				r.subtree[plan.ID] = resolvedChildren
				return plan, session.ReturnResultPayload{}, cerr
			}
			if critiqued.ID != "" {
				// Resolved critique is recorded in the subtree under the
				// plan for trace fidelity. The verifier_signal it produced
				// is already in r.state.VerifierSignals via resolve's
				// category branch.
				resolvedChildren = append(resolvedChildren, critiqued)
			}
		}
	}
	r.subtree[plan.ID] = resolvedChildren

	// Recompose. Spec rides on the plan's emit_subtree output.
	if len(bubbledPayloads) == 0 {
		// All children were verifier_signals or empty subtrees. No
		// payload to recompose; the plan's effective bubble is empty.
		return plan, session.ReturnResultPayload{}, nil
	}
	if r.cfg.Recomposer == nil {
		return plan, session.ReturnResultPayload{}, ErrRecomposerRequired
	}
	spec := plan.Execute.EmitSubtree.Recompose
	composed, err := r.cfg.Recomposer.Recompose(ctx, spec, bubbledPayloads)
	if err != nil {
		plan.ExitReason = session.ExitInhibited
		trace.Error(r.cfg.Tracer, ctx, "runner.recompose_error",
			slog.String("plan_id", string(plan.ID)),
			slog.String("spec", string(spec)),
			slog.String("err", err.Error()),
		)
		return plan, session.ReturnResultPayload{}, err
	}
	return plan, composed, nil
}

// isZeroPayload reports whether a ReturnResultPayload is the zero
// value (no content, no confidence, no signals). Used to skip empty
// bubbles from emit_subtree children whose own subtree resolved to
// nothing (e.g. all-verifier-signal children).
func isZeroPayload(p session.ReturnResultPayload) bool {
	if p.Result.Content != "" || p.Result.Kind != "" || p.Confidence != 0 {
		return false
	}
	if p.Signals.GroundedPass != nil {
		return false
	}
	if len(p.Signals.Contradictions) > 0 || len(p.Signals.OpenQuestions) > 0 {
		return false
	}
	return true
}

func decisionString(d inhibitor.Decision) string {
	switch d {
	case inhibitor.Continue:
		return "continue"
	case inhibitor.Restart:
		return "restart"
	case inhibitor.Abort:
		return "abort"
	default:
		return "unknown"
	}
}

// defaultChildIDFn returns a closure that assigns sequential
// child IDs of the form "<parent_id>-c<seed_index>".
func defaultChildIDFn() func(session.Envelope, int) session.ID {
	return func(parent session.Envelope, seedIndex int) session.ID {
		return session.ID(fmt.Sprintf("%s-c%d", parent.ID, seedIndex))
	}
}

// maybeInjectCritique consults the verifier policy after a sibling
// produces a return_result and, if the policy or the child's
// NeedsVerification override says so, runs a critique AP on the result.
// Returns the resolved critique envelope (empty-ID if no critique ran).
//
// The critique is dispatched into the same scope as the AP it critiques.
// Its verifier_signal flows to RunState via resolve's category branch
// (per the "verifier_signal goes to the runtime, not the next AP" lock),
// so it never reaches the recomposer.
func (r *runner) maybeInjectCritique(
	ctx context.Context,
	plan session.Envelope,
	target session.Envelope,
	scope session.ScopeHandle,
	budget session.Budget,
	parentChain []session.Envelope,
) (session.Envelope, error) {
	// No policy and no parent override → nothing to do.
	parentOverride := target.NeedsVerification
	if r.cfg.VerifierPolicy == nil && !parentOverride {
		return session.Envelope{}, nil
	}

	// `action` is what the just-resolved AP did. The policy keys
	// per_action telemetry off this.
	var action session.Playbook
	if target.Evaluate != nil {
		action = target.Evaluate.Playbook
	}

	// Even without a policy, an explicit NeedsVerification override
	// forces a critique. With a policy, the policy decides (and honors
	// the override on top of the baseline).
	shouldEmit := parentOverride
	if r.cfg.VerifierPolicy != nil {
		shouldEmit = r.cfg.VerifierPolicy.ShouldCritique(action, parentOverride, r.cfg.Rand)
	}
	if !shouldEmit {
		return session.Envelope{}, nil
	}

	// Build the critique seed. Input carries the result content as the
	// thing to critique; the adapter's Evaluate is expected to pick
	// PlaybookCritique for input.kind == "critique_target". OutputSchema
	// is the verifier verdict shape (pass|fail|issues).
	seed := session.SubAPSeed{
		Input: session.Payload{
			Kind:    "critique_target",
			Content: target.Execute.ReturnResult.Result.Content,
		},
		OutputSchema:      "{verdict, issues}",
		Classification:    target.Classification,
		NeedsVerification: false, // no double-critique
	}
	critiqueID := session.ID(fmt.Sprintf("%s-critique", target.ID))
	critique := session.NewChild(&plan, seed, critiqueID, scope, budget)

	resolved, _, err := r.resolve(ctx, critique, &plan, parentChain)
	if err != nil {
		trace.Error(r.cfg.Tracer, ctx, "runner.critique_error",
			slog.String("target_id", string(target.ID)),
			slog.String("err", err.Error()),
		)
		return resolved, err
	}
	r.state.PolicyCritiquesEmitted++
	trace.Info(r.cfg.Tracer, ctx, "runner.critique_emitted",
		slog.String("target_id", string(target.ID)),
		slog.String("critique_id", string(resolved.ID)),
		slog.Bool("parent_override", parentOverride),
	)
	return resolved, nil
}
