// CLAUDE GENERATED
// Package runner walks the call tree of AP invocations under the
// call-tree model.
//
// Given a root envelope, the runner:
//
//  1. Dispatches it to the specialist registered for its BrainFunction
//     (via pkg/registry).
//  2. Checks the inhibitor against the root→current path after dispatch.
//  3. If the invocation emitted SubAPs, builds child envelopes from the
//     seeds, walks each subtree synchronously in order (v0 has no
//     sibling parallelism), and recomposes children outputs into the
//     parent's final Output via pkg/recomposer.
//  4. Returns the resolved root with its composed Output.
//
// Sync, no goroutines, no channels. Hardware parallelism is deferred
// (see parent CLAUDE.md). If a sub-AP returns an inhibited or errored
// envelope, the parent surfaces ExitInhibited and bails on remaining
// siblings — strict semantics; partial recomposition is not allowed.
package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/inhibitor"
	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/registry"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Config bundles the tree-walker's dependencies.
type Config struct {
	Registry   *registry.Registry
	Recomposer recomposer.Recomposer
	Inhibitor  inhibitor.Inhibitor
	Root       session.Envelope
	Logger     *slog.Logger // optional; nil-safe

	// MaxDepth bounds the call tree's depth as a belt-and-suspenders.
	// Distinct from Envelope.Budget.DepthRemaining (which is per-AP
	// allotment, possibly specialized by brain functions). Defaults to
	// 16 when zero or less.
	MaxDepth int

	// NextID generates IDs for child envelopes. Defaults to a monotonic
	// per-run counter when nil.
	NextID func() session.ID
}

// Reason describes the outcome of a Run.
type Reason string

const (
	ReasonSuccess         Reason = "success"
	ReasonInhibitorAbort  Reason = "inhibitor_abort"
	ReasonContextDone     Reason = "context_done"
	ReasonDispatchError   Reason = "dispatch_error"
	ReasonRecomposeError  Reason = "recompose_error"
	ReasonDepthExhausted  Reason = "depth_exhausted"
)

// Result is the runner's outcome. Root is the resolved root envelope
// — its Output is the composed final result on success, or carries the
// failure context (ExitInhibited) otherwise.
type Result struct {
	Root   session.Envelope
	Reason Reason
	Detail string
}

// Run walks the call tree rooted at cfg.Root and returns the resolved
// root. Returns an error only for validation failures and ctx errors
// the walker could not surface as ExitInhibited.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := validate(cfg); err != nil {
		return Result{}, err
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 16
	}
	if cfg.NextID == nil {
		cfg.NextID = defaultIDGen()
	}

	resolved, err := walk(ctx, cfg, nil, cfg.Root, 0)
	if err != nil {
		// ctx done — surface as a Result with the partially-resolved
		// root rather than discarding it.
		return Result{Root: resolved, Reason: ReasonContextDone, Detail: err.Error()}, nil
	}
	reason := ReasonSuccess
	detail := ""
	if resolved.IsInhibited() {
		reason = ReasonInhibitorAbort
		if resolved.Output != nil {
			detail = resolved.Output.Content
		}
	}
	return Result{Root: resolved, Reason: reason, Detail: detail}, nil
}

// walk runs one node in the call tree and returns the resolved
// envelope. The path argument is the root→current sequence (current
// not yet appended).
func walk(ctx context.Context, cfg Config, path []session.Envelope, current session.Envelope, depth int) (session.Envelope, error) {
	if err := ctx.Err(); err != nil {
		current.Output = inhibitedOutput("context cancelled before dispatch: " + err.Error())
		return current, err
	}
	if depth >= cfg.MaxDepth {
		current.Output = inhibitedOutput(
			fmt.Sprintf("depth %d hit MaxDepth %d", depth, cfg.MaxDepth))
		return current, nil
	}
	if current.Budget.DepthRemaining < 0 {
		current.Output = &session.Output{
			ExitReason: session.ExitBudgetExhausted,
			Content:    "depth budget exhausted before dispatch",
		}
		return current, nil
	}

	// Dispatch the invocation.
	osc, err := cfg.Registry.Lookup(current.BrainFunction)
	if err != nil {
		current.Output = inhibitedOutput("dispatch error: " + err.Error())
		return current, nil
	}
	current = osc.Invoke(ctx, current)

	// Inhibitor check on the path including this newly-resolved node.
	pathWithCurrent := append(append([]session.Envelope(nil), path...), current)
	v := cfg.Inhibitor.Check(pathWithCurrent)
	if v.Decision != inhibitor.Continue {
		detail := v.Reason
		if v.Decision == inhibitor.Restart {
			// No checkpointing yet — downgrade to abort with annotated reason.
			detail = "restart-as-abort (no checkpointing yet): " + v.Reason
		}
		if cfg.Logger != nil {
			cfg.Logger.Info("inhibitor abort",
				"decision", v.Decision, "reason", v.Reason,
				"path_depth", len(pathWithCurrent),
				"session", current.ID)
		}
		current.Output = inhibitedOutput(detail)
		return current, nil
	}

	// Leaf — no SubAPs, no recomposition. Already inhibited (e.g. adapter
	// error) also returns directly.
	if current.IsInhibited() || current.IsLeaf() {
		return current, nil
	}

	// Has SubAPs — walk each subtree in order.
	seeds := current.Output.SubAPs
	children := make([]session.Envelope, 0, len(seeds))
	for i, seed := range seeds {
		if err := ctx.Err(); err != nil {
			return current, err
		}
		child := buildChild(current, seed, cfg.NextID())
		resolvedChild, err := walk(ctx, cfg, pathWithCurrent, child, depth+1)
		if err != nil {
			return current, err
		}
		children = append(children, resolvedChild)
		if resolvedChild.IsInhibited() {
			// Strict: any inhibited child inhibits the parent. Stop
			// dispatching siblings — partial recomposition is not allowed.
			current.Output = inhibitedOutput(
				fmt.Sprintf("child %d (%s) inhibited; abandoning remaining %d siblings",
					i, resolvedChild.BrainFunction, len(seeds)-i-1))
			return current, nil
		}
	}

	// Recompose.
	composed, err := cfg.Recomposer.Recompose(ctx, *current.Output, children)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Error("recompose error",
				"session", current.ID, "err", err)
		}
		current.Output = inhibitedOutput("recompose error: " + err.Error())
		return current, nil
	}
	current.Output = &composed
	return current, nil
}

func buildChild(parent session.Envelope, seed session.SubAPSeed, id session.ID) session.Envelope {
	parentID := parent.ID
	return session.Envelope{
		SchemaVersion:  session.SchemaVersion,
		ID:             id,
		BrainFunction:  seed.BrainFunction,
		Classification: parent.Classification,
		Input:          seed.Input,
		OutputSchema:   seed.OutputSchema,
		ParentRef:      &parentID,
		Budget: session.Budget{
			TokensRemaining: parent.Budget.TokensRemaining, // adapter-level decrement later
			DepthRemaining:  parent.Budget.DepthRemaining - 1,
		},
	}
}

func inhibitedOutput(content string) *session.Output {
	return &session.Output{
		ExitReason: session.ExitInhibited,
		Content:    content,
	}
}

func validate(cfg Config) error {
	if cfg.Registry == nil {
		return errors.New("runner: Config.Registry is nil")
	}
	if cfg.Recomposer == nil {
		return errors.New("runner: Config.Recomposer is nil")
	}
	if cfg.Inhibitor == nil {
		return errors.New("runner: Config.Inhibitor is nil")
	}
	if cfg.Root.BrainFunction == "" {
		return errors.New("runner: Config.Root.BrainFunction is empty")
	}
	if _, err := cfg.Registry.Lookup(cfg.Root.BrainFunction); err != nil {
		return fmt.Errorf("runner: root brain function %q not registered", cfg.Root.BrainFunction)
	}
	return nil
}

func defaultIDGen() func() session.ID {
	var counter atomic.Int64
	start := time.Now().UnixNano()
	return func() session.ID {
		n := counter.Add(1)
		return session.ID(fmt.Sprintf("sess-%d-%d", start, n))
	}
}
