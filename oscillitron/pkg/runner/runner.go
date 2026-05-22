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

	"sync"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/judge"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
	"github.com/jrlmx2/oscillitron/pkg/verifier"
	"github.com/jrlmx2/oscillitron/pkg/vram"
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
	// Cost is an optional observer of the cost ledger the adapter writes
	// into. When set, RunState.CostSummary is populated from this
	// tracker after Run returns. The runner itself does not call Record;
	// adapters do that (see pkg/adapter/hermes). Wiring the tracker here
	// is purely so callers have one entry point for call-tree state and
	// aggregate cost.
	Cost *cost.Tracker
	// Judge is the frontier-model audit (the second-opinion layer that
	// feeds VerifierPolicy.RecordJudgeAgreement). After a critique
	// resolves, the JudgeSampler decides whether to sample; if so, the
	// Judge produces an independent verdict and the runner records
	// agreement on the policy. Judge errors are non-fatal — they
	// surface in JudgeErrors but do not fail the run.
	//
	// Wiring Judge without JudgeSampler is a configuration error caught
	// at Run-time; wiring JudgeSampler without Judge is allowed (the
	// sampler short-circuits, useful for telemetry-only runs).
	Judge judge.Judge
	// JudgeSampler owns the 100% un-grounded / 10% grounded sampling
	// rates (locked 2026-05-19). If nil while Judge is set, the runner
	// uses judge.DefaultSamplePolicy().
	JudgeSampler *judge.Sampler
	// Governor optionally coordinates VRAM headroom with every other
	// component hitting the same inference substrate (grader, bench
	// drivers, anything else calling the adapter through this runner).
	//
	// When set, the runner Acquires a lease around each AP's
	// Evaluate+Execute window and Releases before descending (so
	// children claim their own slots — holding through descend would
	// deadlock at depth ≥ ceiling). The runner does NOT own VRAM
	// configuration — the governor self-manages probe, estimator,
	// ModelSpec, ceiling, and reserve. The runner just consults it.
	//
	// Nil = no VRAM management (operator's explicit opt-out, fine for
	// tests; in production wire a Governor or accept the consequences).
	Governor *vram.Governor
	// MaxConcurrency caps the number of sibling goroutines in flight
	// during emit_subtree dispatch.
	//
	//   - 0: no per-wave goroutine cap (governor's per-call Acquire
	//     still serializes inflight calls per its budget).
	//   - 1: strict serial dispatch.
	//   - N>1: cap at N siblings concurrent. If a Governor is also
	//     wired, the effective cap is min(N, governor.Ceiling).
	//
	// Strict cancellation: the first sibling to fire inhibitor.Abort
	// cancels the others via context.
	MaxConcurrency int
	// MaxInputBytes optionally caps the size of an AP envelope's
	// Input.Content. Calls exceeding the budget are inhibited with
	// "input_bytes_exceeded" — surfaces prompt bloat as a test failure
	// rather than silent VRAM growth. 0 = no cap.
	MaxInputBytes int
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
	// CostSummary is populated from Config.Cost.Summary() at the end of
	// Run when a tracker is wired. Zero-valued when no tracker is set.
	CostSummary cost.Summary
	// JudgeSamplesTaken counts targets that were sampled by the
	// JudgeSampler (i.e., a Judge.Judge call was attempted). A subset
	// of PolicyCritiquesEmitted — only critiques that resolved with a
	// verifier_signal verdict can be judge-sampled.
	JudgeSamplesTaken int
	// JudgeAgreements counts judge calls where the frontier verdict
	// matched the local critique. Sum of agreements + disagreements +
	// errors equals JudgeSamplesTaken.
	JudgeAgreements int
	// JudgeDisagreements counts judge calls where the frontier verdict
	// differed from the local critique.
	JudgeDisagreements int
	// JudgeErrors counts judge calls that returned an error. The
	// runner does NOT fail the run on judge errors; an error is treated
	// as "no audit signal for this AP" and is not fed to the policy.
	JudgeErrors int
	// PerPlaybookTokens aggregates Evaluate + Execute TokensUsed by
	// playbook so the operator can see which playbook's prompt is
	// fattest. Includes evaluate calls under the empty-playbook key
	// (they happen before the playbook is picked).
	PerPlaybookTokens map[session.Playbook]int64
	// ConcurrentDispatches counts how many dispatch waves used N>1
	// goroutines (i.e., MaxConcurrency>1 took effect). 0 means every
	// descend ran serially.
	ConcurrentDispatches int
}

// VerifierSignalRecord is one critique / verify_grounded outcome
// captured by the runtime.
type VerifierSignalRecord struct {
	APID    session.ID
	Verdict session.Verdict
	Issues  []session.Issue
}

// Run walks the call tree from root and returns the Result.
//
// VRAM management: when Config.Governor is non-nil, the runner
// Acquires/Releases a lease around each AP's Evaluate+Execute window
// (released before descend to avoid deadlock at depth ≥ ceiling).
// When nil, the runner runs without VRAM throttling — operator's
// explicit opt-out, fine for tests, dangerous in production. The
// runner itself owns no VRAM configuration; the governor self-manages
// its probe, estimator, model, ceiling, and reserve.
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
	if cfg.Cost != nil {
		r.state.CostSummary = cfg.Cost.Summary()
	}
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
	// mu protects state, subtree, and cfg.Rand for concurrent access
	// when MaxConcurrency > 1. All shared mutations go through helpers
	// (lockedStateInc, lockedSubtreeSet, lockedRandPerm) so the locking
	// discipline is in one place.
	mu sync.Mutex
}

// --- locked helpers (mu must NOT be held by caller) ---

func (r *runner) lockedStateInc(field *int) {
	r.mu.Lock()
	*field++
	r.mu.Unlock()
}

func (r *runner) lockedAddVerifierSignal(rec VerifierSignalRecord) {
	r.mu.Lock()
	r.state.VerifierSignals = append(r.state.VerifierSignals, rec)
	r.mu.Unlock()
}

func (r *runner) lockedAddPerPlaybookTokens(pb session.Playbook, tokens int64) {
	if tokens == 0 {
		return
	}
	r.mu.Lock()
	if r.state.PerPlaybookTokens == nil {
		r.state.PerPlaybookTokens = map[session.Playbook]int64{}
	}
	r.state.PerPlaybookTokens[pb] += tokens
	r.mu.Unlock()
}

func (r *runner) lockedSetSubtree(id session.ID, children []session.Envelope) {
	r.mu.Lock()
	r.subtree[id] = children
	r.mu.Unlock()
}

func (r *runner) lockedRandPerm(n int) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg.Rand.Perm(n)
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
		r.lockedStateInc(&r.state.InhibitCount)
		return env, session.ReturnResultPayload{}, nil
	}

	// Input size budget. Surfaces prompt bloat early.
	if r.cfg.MaxInputBytes > 0 && len(env.Input.Content) > r.cfg.MaxInputBytes {
		env.ExitReason = session.ExitInhibited
		trace.Error(r.cfg.Tracer, ctx, "runner.input_bytes_exceeded",
			slog.String("ap_id", string(env.ID)),
			slog.Int("bytes", len(env.Input.Content)),
			slog.Int("max", r.cfg.MaxInputBytes),
		)
		r.lockedStateInc(&r.state.InhibitCount)
		return env, session.ReturnResultPayload{}, nil
	}

	// Governor coordination. Hold one lease for this AP's
	// Evaluate+Execute adapter-call window only. Released before
	// descend so children can claim their own slots — holding through
	// the recursive subtree would deadlock at depth ≥ ceiling.
	//
	// Acquire returns a no-op lease on a nil governor, so the call
	// shape is uniform whether or not the operator wired one.
	lease, lerr := r.cfg.Governor.Acquire(ctx)
	if lerr != nil {
		env.ExitReason = session.ExitInhibited
		trace.Error(r.cfg.Tracer, ctx, "runner.governor_acquire_error",
			slog.String("ap_id", string(env.ID)),
			slog.String("err", lerr.Error()),
		)
		return env, session.ReturnResultPayload{}, lerr
	}
	// Defer release covers every early-return error path; we also
	// explicitly release before descend (Lease.Release is idempotent,
	// so the defer there becomes a no-op).
	defer lease.Release()

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
	r.lockedStateInc(&r.state.EvaluateCount)
	env = evalEnv
	if env.Evaluate != nil {
		// Telemetry: evaluate tokens count under the empty-playbook key
		// (the playbook IS the evaluate output — pre-pick attribution).
		r.lockedAddPerPlaybookTokens("", int64(env.Evaluate.TokensUsed))
	}
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
	r.lockedStateInc(&r.state.ExecuteCount)
	env = execEnv
	if env.Execute != nil && env.Evaluate != nil {
		r.lockedAddPerPlaybookTokens(env.Evaluate.Playbook, int64(env.Execute.TokensUsed))
	}

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
			r.lockedStateInc(&r.state.InhibitCount)
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
		r.lockedAddVerifierSignal(VerifierSignalRecord{
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
		// Release before descending — children will Acquire their own
		// slots. Holding through descend would deadlock the governor
		// at depth ≥ ceiling.
		lease.Release()
		return r.descend(ctx, env, parentChain)

	default:
		return env, session.ReturnResultPayload{}, fmt.Errorf("%w: unknown category %q", ErrInvalidExecuteCategory, env.Execute.Category)
	}
}

// descend dispatches an emit_subtree AP's children, recurses into
// each, collects return_result payloads, and recomposes per the
// plan's RecomposeSpec.
//
// Concurrent dispatch (Config.MaxConcurrency > 1 and/or
// Config.VRAMProbe set) runs siblings in parallel via goroutines
// bounded by a semaphore. Strict cancellation: the first sibling to
// fire inhibitor.Abort (or to error) cancels in-flight siblings via
// context, matching the locked "one inhibited child inhibits the
// parent" rule.
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
	// relying on emission order. Use the locked helper for safe
	// concurrent access to r.cfg.Rand.
	order := r.lockedRandPerm(len(children))

	// Recurse into each child, collecting return_result payloads.
	chainForChildren := append([]session.Envelope(nil), parentChain...)
	chainForChildren = append(chainForChildren, plan)

	concurrency := r.computeConcurrency(ctx, len(children))
	if concurrency > 1 {
		r.lockedStateInc(&r.state.ConcurrentDispatches)
		trace.Info(r.cfg.Tracer, ctx, "runner.descend_concurrent",
			slog.String("plan_id", string(plan.ID)),
			slog.Int("siblings", len(children)),
			slog.Int("concurrency", concurrency),
		)
	}

	type outcome struct {
		idx      int
		resolved session.Envelope
		payload  session.ReturnResultPayload
		critique session.Envelope // empty ID if no critique was injected
		err      error
	}

	dispatch := func(dispCtx context.Context, cancel context.CancelFunc, sem chan struct{}, idx int) outcome {
		if err := dispCtx.Err(); err != nil {
			return outcome{idx: idx, err: err}
		}
		if sem != nil {
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := dispCtx.Err(); err != nil {
				return outcome{idx: idx, err: err}
			}
		}
		// Note: governor Acquire happens inside resolve(), around the
		// Evaluate+Execute call window, NOT here. Holding a lease
		// across the recursive descend would deadlock the governor
		// at depth ≥ ceiling.
		resolved, payload, err := r.resolve(dispCtx, children[idx], &plan, chainForChildren)
		o := outcome{idx: idx, resolved: resolved, payload: payload, err: err}
		if err != nil || resolved.IsInhibited() {
			cancel() // strict cancellation
			return o
		}
		// Critique injection. Same conditions as the serial path.
		if resolved.Execute != nil && resolved.Execute.Category == session.CategoryReturnResult {
			critiqued, cerr := r.maybeInjectCritique(dispCtx, plan, resolved, scope, childBudget, chainForChildren)
			if cerr != nil {
				o.err = cerr
				cancel()
				return o
			}
			if critiqued.ID != "" {
				o.critique = critiqued
			}
		}
		return o
	}

	outcomes := make([]outcome, len(children))
	dispCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if concurrency <= 1 {
		// Serial path. Identical to the pre-concurrency behavior, kept
		// as the fast path and for backwards compatibility.
		for _, idx := range order {
			outcomes[idx] = dispatch(dispCtx, cancel, nil, idx)
			if outcomes[idx].err != nil || outcomes[idx].resolved.IsInhibited() {
				break // strict semantics: stop dispatching after first inhibit
			}
		}
	} else {
		// Concurrent path. Goroutine-per-sibling, semaphore-bounded.
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for _, idx := range order {
			idx := idx
			wg.Add(1)
			go func() {
				defer wg.Done()
				outcomes[idx] = dispatch(dispCtx, cancel, sem, idx)
			}()
		}
		wg.Wait()
	}

	// Reconstruct resolvedChildren + bubbledPayloads in randomized
	// dispatch order so the recomposer sees the same payload sequence
	// it would see in serial mode. Critiques appended after.
	resolvedChildren := make([]session.Envelope, 0, len(children)+len(children))
	for _, idx := range order {
		o := outcomes[idx]
		// Even cancelled siblings get recorded so the subtree map is
		// faithful. Cancelled-by-context outcomes have err == ctx.Err()
		// and an empty resolved envelope; we still slot them in.
		resolvedChildren = append(resolvedChildren, o.resolved)
	}
	// Critiques in the same order their target dispatched in.
	for _, idx := range order {
		if outcomes[idx].critique.ID != "" {
			resolvedChildren = append(resolvedChildren, outcomes[idx].critique)
		}
	}

	// Surface failures: any error or any inhibited child marks the
	// parent inhibited. First-encountered error in randomized order
	// wins (matches the serial behavior).
	var firstErr error
	parentInhibited := false
	for _, idx := range order {
		o := outcomes[idx]
		if o.err != nil && firstErr == nil {
			// Don't propagate ctx.Canceled from siblings that were
			// cancelled by a peer — surface the real cause.
			if !errors.Is(o.err, context.Canceled) {
				firstErr = o.err
			}
		}
		if o.resolved.IsInhibited() {
			parentInhibited = true
		}
	}
	if firstErr != nil {
		plan.ExitReason = session.ExitInhibited
		r.lockedSetSubtree(plan.ID, resolvedChildren)
		return plan, session.ReturnResultPayload{}, firstErr
	}
	if parentInhibited {
		plan.ExitReason = session.ExitInhibited
		r.lockedSetSubtree(plan.ID, resolvedChildren)
		trace.Error(r.cfg.Tracer, ctx, "runner.parent_inhibited_by_child",
			slog.String("plan_id", string(plan.ID)),
		)
		return plan, session.ReturnResultPayload{}, nil
	}

	// Build bubbledPayloads in randomized order, skipping
	// verifier_signal children (whose verdict is in RunState) and
	// zero payloads.
	bubbledPayloads := make([]session.ReturnResultPayload, 0, len(children))
	for _, idx := range order {
		o := outcomes[idx]
		if o.resolved.Execute == nil || o.resolved.Execute.Category == session.CategoryVerifierSignal {
			continue
		}
		if !isZeroPayload(o.payload) {
			bubbledPayloads = append(bubbledPayloads, o.payload)
		}
	}
	r.lockedSetSubtree(plan.ID, resolvedChildren)

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

// computeConcurrency derives the per-wave goroutine cap.
//
//   - MaxConcurrency=1 → 1 (strict serial).
//   - MaxConcurrency=0 → siblings (no per-wave goroutine cap; the
//     governor's per-call Acquire is the throttle).
//   - MaxConcurrency=N>1 → min(N, siblings).
//   - With a Governor wired, further tightened by governor.Ceiling.
//
// The runner owns no VRAM math. When set, the governor handles all
// per-byte accounting through its Acquire/Release contract; the wave
// cap exists only to bound goroutine count (each blocks on Acquire
// regardless).
func (r *runner) computeConcurrency(ctx context.Context, siblings int) int {
	if siblings <= 1 {
		return 1
	}
	if r.cfg.MaxConcurrency == 1 {
		return 1
	}

	cap := siblings
	if r.cfg.MaxConcurrency > 1 && r.cfg.MaxConcurrency < cap {
		cap = r.cfg.MaxConcurrency
	}
	if r.cfg.Governor != nil {
		if c := r.cfg.Governor.Ceiling(); c > 0 && c < cap {
			cap = c
		}
	}
	if cap < 1 {
		cap = 1
	}
	if cap > 1 {
		trace.Info(r.cfg.Tracer, ctx, "runner.wave_cap",
			slog.Int("cap", cap),
			slog.Int("siblings", siblings),
		)
	}
	return cap
}

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
	// the override on top of the baseline). Lock the Rand for the
	// duration of the call — math/rand/v2.Rand isn't safe for
	// concurrent use, and ShouldCritique calls Float64 on it.
	shouldEmit := parentOverride
	if r.cfg.VerifierPolicy != nil {
		r.mu.Lock()
		shouldEmit = r.cfg.VerifierPolicy.ShouldCritique(action, parentOverride, r.cfg.Rand)
		r.mu.Unlock()
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
	r.lockedStateInc(&r.state.PolicyCritiquesEmitted)
	trace.Info(r.cfg.Tracer, ctx, "runner.critique_emitted",
		slog.String("target_id", string(target.ID)),
		slog.String("critique_id", string(resolved.ID)),
		slog.Bool("parent_override", parentOverride),
	)

	// Judge-sampling audit. Only meaningful when the critique resolved
	// into a verifier_signal (i.e., a local verdict exists to compare
	// against). The sampler decides 100% un-grounded vs. 10% grounded
	// based on the *target's* Signals.GroundedPass, then the runner
	// records agreement on the verifier policy.
	r.maybeAuditWithJudge(ctx, target, resolved, action)
	return resolved, nil
}

// maybeAuditWithJudge runs the judge sampling layer (locked 2026-05-19)
// against a just-resolved critique. Errors from the judge are recorded
// in RunState.JudgeErrors but never fail the run — a frontier judge
// outage shouldn't be able to kill a working call tree.
func (r *runner) maybeAuditWithJudge(
	ctx context.Context,
	target, critique session.Envelope,
	action session.Playbook,
) {
	if r.cfg.Judge == nil {
		return
	}
	if critique.Execute == nil || critique.Execute.Category != session.CategoryVerifierSignal ||
		critique.Execute.VerifierSignal == nil {
		// Critique didn't produce a verifier_signal — nothing for the
		// judge to second-guess.
		return
	}
	sampler := r.cfg.JudgeSampler
	if sampler == nil {
		// Caller wired a Judge but no Sampler: use the v0 locked policy.
		defaultSampler := judge.NewSampler(judge.DefaultSamplePolicy())
		sampler = defaultSampler
	}
	r.mu.Lock()
	shouldJudge := sampler.ShouldJudge(target, r.cfg.Rand)
	r.mu.Unlock()
	if !shouldJudge {
		return
	}

	r.lockedStateInc(&r.state.JudgeSamplesTaken)
	localVerdict := critique.Execute.VerifierSignal.Verdict
	req := judge.Request{
		Target:       target,
		LocalVerdict: localVerdict,
		LocalIssues:  critique.Execute.VerifierSignal.Issues,
	}
	resp, err := r.cfg.Judge.Judge(ctx, req)
	if err != nil {
		r.lockedStateInc(&r.state.JudgeErrors)
		trace.Error(r.cfg.Tracer, ctx, "runner.judge_error",
			slog.String("target_id", string(target.ID)),
			slog.String("critique_id", string(critique.ID)),
			slog.String("err", err.Error()),
		)
		return
	}
	agreed := resp.Verdict == localVerdict
	if agreed {
		r.lockedStateInc(&r.state.JudgeAgreements)
	} else {
		r.lockedStateInc(&r.state.JudgeDisagreements)
	}
	if r.cfg.VerifierPolicy != nil {
		r.cfg.VerifierPolicy.RecordJudgeAgreement(action, agreed)
	}
	trace.Info(r.cfg.Tracer, ctx, "runner.judge_audited",
		slog.String("target_id", string(target.ID)),
		slog.String("critique_id", string(critique.ID)),
		slog.String("local_verdict", string(localVerdict)),
		slog.String("judge_verdict", string(resp.Verdict)),
		slog.Bool("agreed", agreed),
		slog.Bool("grounded", judge.IsGrounded(target)),
	)
}
