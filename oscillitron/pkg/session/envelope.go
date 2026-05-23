// CLAUDE GENERATED
// Package session defines the AP envelope under the uniform-node +
// evaluate/execute call-tree model.
//
// An AP is *one call*. Every AP runs the same two-step workflow:
//
//  1. Evaluate — an LLM call picks the right Playbook for this AP from
//     the v0 playbook set (plan / process / critique / verify_grounded /
//     compose).
//  2. Execute — runs the chosen playbook; produces a result that falls
//     into one of three Categories (emit_subtree / return_result /
//     verifier_signal).
//
// Specialization lives in the playbook substrate keyed by action, not
// in distinct node types. There is no BrainFunction field on the
// envelope — the playbook is *picked* by evaluate, not declared by the
// caller.
//
// The envelope is the lean record the next invocation reads. Token cost
// compounds over every hop, so anything that is a learning-loop signal
// (verifier feedback, retrieval refs, cost ledger entries, full subtree
// topology) goes in pkg/trace, not here.
//
// See parent CLAUDE.md "Architecture" (locks 2026-05-18 through
// 2026-05-20) and scratch/design-notes.md "JSON envelope sketch".
package session

import (
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/stakes"
)

// SchemaVersion lets the envelope evolve additively. Bumped only on
// breaking shape changes; readers can downgrade gracefully.
const SchemaVersion = "v2"

// ID uniquely identifies an envelope (one AP in the call tree).
type ID string

// ScopeHandle names a parent's scope channel — the shared sibling
// result bus that compose APs pull from. Nil at the root (no parent).
type ScopeHandle string

// Playbook is the action tag the evaluate step picks. One per AP.
// Each playbook produces output in exactly one Category (see below).
type Playbook string

const (
	// PlaybookPlan decomposes a task. Output category: emit_subtree.
	PlaybookPlan Playbook = "plan"
	// PlaybookProcess does work on a task. Output category: return_result.
	PlaybookProcess Playbook = "process"
	// PlaybookCritique inspects a prior result. Output category: verifier_signal.
	PlaybookCritique Playbook = "critique"
	// PlaybookVerifyGrounded runs a grounded check (compile, exec,
	// retrieval-match) carried on the envelope's VerifySpec. Output
	// category: verifier_signal.
	PlaybookVerifyGrounded Playbook = "verify_grounded"
	// PlaybookCompose reduces sibling results pulled from a parent
	// scope channel. Output category: return_result.
	PlaybookCompose Playbook = "compose"
)

// Category discriminates the Execute payload. Encoded on the envelope
// so the runner knows what to do next.
type Category string

const (
	// CategoryEmitSubtree — produces sub-APs into the parent's scope;
	// doesn't return up. Used by plan.
	CategoryEmitSubtree Category = "emit_subtree"
	// CategoryReturnResult — value flows up the tree to the parent.
	// Used by process and compose.
	CategoryReturnResult Category = "return_result"
	// CategoryVerifierSignal — pass/fail/issues flows to the runtime,
	// not the next AP. Runtime owns retry/proceed/escalate policy.
	// Used by critique and verify_grounded.
	CategoryVerifierSignal Category = "verifier_signal"
)

// RecomposeSpec tells the parent's recomposer what shape the children
// have. Carried on a plan AP's emit_subtree payload.
type RecomposeSpec string

const (
	RecomposePairwise   RecomposeSpec = "pairwise"
	RecomposeSequential RecomposeSpec = "sequential"
	RecomposeNone       RecomposeSpec = "none"
)

// Verdict is a verifier_signal's pass/fail summary.
type Verdict string

const (
	VerdictPass   Verdict = "pass"
	VerdictFail   Verdict = "fail"
	VerdictIssues Verdict = "issues"
)

// Severity ranks an Issue's seriousness for a verifier_signal payload.
type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// ExitReason records why an invocation exited. Predicates read this.
type ExitReason string

const (
	// ExitDone — invocation finished within its budget. Execute payload
	// is final for this AP (modulo sub-AP resolution and recomposition).
	ExitDone ExitReason = "done"
	// ExitBudgetExhausted — invocation hit its token/depth budget.
	ExitBudgetExhausted ExitReason = "budget_exhausted"
	// ExitInhibited — inhibitor aborted this invocation or subtree.
	ExitInhibited ExitReason = "inhibited"
)

// Budget governs a subtree's resource ceiling. Per-AP allotment.
// DepthRemaining is decremented as sub-APs descend; TokensRemaining is
// charged by the adapter as it runs.
type Budget struct {
	TokensRemaining int `json:"tokens_remaining"`
	DepthRemaining  int `json:"depth_remaining"`
}

// Payload is the shared shape for an AP's input and a return_result's
// output. Polymorphic by Kind ("task", "result", "scope_pull", ...).
type Payload struct {
	Kind        string `json:"kind"`
	Content     string `json:"content"`
	ContentHash string `json:"content_hash,omitempty"`
}

// VerifySpec carries a grounded-check description for a
// verify_grounded AP. Opaque to the envelope; consumed by the adapter.
type VerifySpec struct {
	Kind string `json:"kind"` // "compile" | "exec" | "retrieval_match" | ...
	Spec string `json:"spec"`
}

// Envelope is one AP — the call, plus (after evaluate and execute run)
// the playbook pick and its execute payload.
type Envelope struct {
	SchemaVersion string `json:"schema_version"`
	ID            ID     `json:"id"`
	// ParentID is the parent AP's ID. Nil at the root.
	ParentID *ID `json:"parent_id,omitempty"`
	// RootID is the topmost AP's ID. Equals ID at the root.
	RootID ID `json:"root_id"`
	// Path is the root→this chain (inclusive). The inhibitor's stateful
	// detectors (confidence drop windows, cumulative contradictions,
	// repetition) read this.
	Path []ID `json:"path"`
	// ScopeHandle is the parent's scope channel — the shared bus
	// sibling results land on. Nil at the root.
	ScopeHandle *ScopeHandle `json:"scope_handle,omitempty"`

	// Input is what this AP operates on.
	Input Payload `json:"input"`
	// OutputSchema is the contract for what a successful Execute must
	// produce. Doubles as the preloaded prompt requirement that forces
	// the producing LLM to self-classify against it.
	OutputSchema string `json:"output_schema"`
	// Classification is the expected confidentiality level (from the
	// envelope contract).
	Classification classification.Level `json:"classification"`

	// Stakes is the effort-routing annotation distinct from
	// Classification. Confidentiality answers "where can this data
	// go"; Stakes answers "how careful do we need to be about being
	// right". v3.0 wires the bench Vote orchestrator to scale its
	// attempt count by Stakes; v3.4 extends to coping decisions.
	//
	// Zero value (empty string) is intentionally NOT one of the
	// valid levels and reads as Medium via stakes.Effective() — so
	// envelopes constructed before v3.0 (or by callers that don't
	// set Stakes) get the same default behavior as pre-v3.0.
	Stakes stakes.Level `json:"stakes,omitempty"`

	// NeedsVerification is the parent override: forces a critique pass
	// regardless of the verifier-policy sampling roll. Suppression by
	// parent is not allowed in v0 — parent can pin, not unpin.
	NeedsVerification bool `json:"needs_verification"`
	// VerifySpec is populated only when this AP will execute
	// verify_grounded. Carries the check (compile / exec / retrieval).
	VerifySpec *VerifySpec `json:"verify_spec,omitempty"`

	Budget Budget `json:"budget"`

	// Evaluate is populated after the evaluate step runs.
	Evaluate *Evaluate `json:"evaluate,omitempty"`
	// Execute is populated after the execute step runs.
	Execute *Execute `json:"execute,omitempty"`

	// ExitReason records why the AP exited. Empty string until exit.
	ExitReason ExitReason `json:"exit_reason,omitempty"`
	// Trace is lean per-AP metrics. The fat learning-loop record lives
	// in pkg/trace, off the inference path.
	Trace Trace `json:"trace"`
	// Audit is populated by the audit ledger from Phase 4 onward.
	Audit *Audit `json:"audit,omitempty"`
}

// Evaluate captures the first-step LLM call that picks the playbook.
// Only Playbook is structurally consumed downstream. Rationale is for
// the trace and human inspection; nothing reads it programmatically.
type Evaluate struct {
	Playbook   Playbook `json:"playbook"`
	Rationale  string   `json:"rationale,omitempty"`
	Confidence float64  `json:"confidence"`
	TokensUsed int      `json:"tokens_used"`
}

// Execute captures the second-step playbook output. Exactly one of
// EmitSubtree / ReturnResult / VerifierSignal is populated, matching
// Category.
type Execute struct {
	Category       Category               `json:"category"`
	EmitSubtree    *EmitSubtreePayload    `json:"emit_subtree,omitempty"`
	ReturnResult   *ReturnResultPayload   `json:"return_result,omitempty"`
	VerifierSignal *VerifierSignalPayload `json:"verifier_signal,omitempty"`
	TokensUsed     int                    `json:"tokens_used"`
}

// EmitSubtreePayload is the execute output of plan. SubAPs are spawned
// into the parent's scope; Recompose tells the parent's recomposer
// what shape to expect.
type EmitSubtreePayload struct {
	SubAPs    []SubAPSeed   `json:"sub_aps"`
	Recompose RecomposeSpec `json:"recompose"`
}

// SubAPSeed is what plan emits per child. Enough to construct a child
// envelope at dispatch time. The runner assigns IDs, inherits budget,
// and stamps Path / ParentID / RootID / ScopeHandle.
type SubAPSeed struct {
	Input             Payload              `json:"input"`
	OutputSchema      string               `json:"output_schema"`
	Classification    classification.Level `json:"classification"`
	NeedsVerification bool                 `json:"needs_verification,omitempty"`
	VerifySpec        *VerifySpec          `json:"verify_spec,omitempty"`
}

// ReturnResultPayload is the execute output of process and compose.
// Result flows up to the parent.
type ReturnResultPayload struct {
	Result     Payload `json:"result"`
	Confidence float64 `json:"confidence"`
	Signals    Signals `json:"signals"`
}

// Signals is the lean local-signal bundle the parent recomposer and
// curation layer read. The rest of the signal record lives in
// pkg/trace.
type Signals struct {
	// GroundedPass is the result of a co-located grounded check, if
	// any. Pointer so absence is distinguishable from false.
	GroundedPass   *bool    `json:"grounded_pass,omitempty"`
	Contradictions []string `json:"contradictions,omitempty"`
	OpenQuestions  []string `json:"open_questions,omitempty"`
}

// VerifierSignalPayload is the execute output of critique and
// verify_grounded. Goes to the runtime, not the next AP.
type VerifierSignalPayload struct {
	Verdict Verdict `json:"verdict"`
	Issues  []Issue `json:"issues,omitempty"`
}

// Issue is a single finding inside a VerifierSignalPayload.
type Issue struct {
	Severity Severity `json:"severity"`
	Where    string   `json:"where,omitempty"`
	What     string   `json:"what"`
}

// Trace records lean per-AP operational metrics. The fat learning-loop
// trace (verifier feedback, retrieval refs, full tree topology, etc.)
// lives in pkg/trace.
type Trace struct {
	TokensInput               int     `json:"tokens_input"`
	TokensOutput              int     `json:"tokens_output"`
	DurationMs                int64   `json:"duration_ms"`
	CostUSD                   float64 `json:"cost_usd"`
	CostVsFrontierBaselineUSD float64 `json:"cost_vs_frontier_baseline_usd"`
}

// Audit is populated by the audit ledger from Phase 4 onward.
type Audit struct {
	LedgerID  string `json:"ledger_id"`
	SignedAt  string `json:"signed_at"` // RFC3339
	Signature string `json:"signature"`
}

// NewRoot constructs a root envelope (no parent, no scope) — the entry
// point a caller fires at the runner. Path is [id]; the runner walks
// down from here.
func NewRoot(id ID, prompt, outputSchema string, level classification.Level, budget Budget) Envelope {
	return Envelope{
		SchemaVersion:  SchemaVersion,
		ID:             id,
		RootID:         id,
		Path:           []ID{id},
		Input:          Payload{Kind: "task", Content: prompt},
		OutputSchema:   outputSchema,
		Classification: level,
		Budget:         budget,
	}
}

// NewChild constructs a child envelope from a parent + seed. The
// runner is responsible for ID assignment, scope routing, and budget
// derivation; this helper centralizes the path/parent/root plumbing.
func NewChild(parent *Envelope, seed SubAPSeed, childID ID, scope ScopeHandle, budget Budget) Envelope {
	path := make([]ID, 0, len(parent.Path)+1)
	path = append(path, parent.Path...)
	path = append(path, childID)
	parentID := parent.ID
	scopeCopy := scope
	return Envelope{
		SchemaVersion:     SchemaVersion,
		ID:                childID,
		ParentID:          &parentID,
		RootID:            parent.RootID,
		Path:              path,
		ScopeHandle:       &scopeCopy,
		Input:             seed.Input,
		OutputSchema:      seed.OutputSchema,
		Classification:    seed.Classification,
		NeedsVerification: seed.NeedsVerification,
		VerifySpec:        seed.VerifySpec,
		Budget:            budget,
	}
}

// IsComplete reports whether the AP has exited. Does not imply the
// subtree below it has resolved.
func (e *Envelope) IsComplete() bool {
	return e.ExitReason != ""
}

// IsLeaf reports whether this AP requested no further descent. A leaf
// is any completed AP whose execute payload did not emit a subtree.
// (Process / compose / critique / verify_grounded are always leaves;
// plan is a leaf only when it emits zero sub-APs.)
func (e *Envelope) IsLeaf() bool {
	if !e.IsComplete() {
		return false
	}
	if e.Execute == nil {
		return true
	}
	if e.Execute.Category != CategoryEmitSubtree {
		return true
	}
	return e.Execute.EmitSubtree == nil || len(e.Execute.EmitSubtree.SubAPs) == 0
}

// IsInhibited reports whether the AP (or its subtree) was aborted by
// the inhibitor.
func (e *Envelope) IsInhibited() bool {
	return e.ExitReason == ExitInhibited
}

// Depth is the number of AP hops from the root to this AP (inclusive).
// Root has Depth 1; its children have Depth 2; and so on.
func (e *Envelope) Depth() int {
	return len(e.Path)
}
