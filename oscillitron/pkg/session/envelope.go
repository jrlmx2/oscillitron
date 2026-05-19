// CLAUDE GENERATED
// Package session defines the AP envelope under the call-tree model.
//
// An AP is an *invocation* of one brain function on one input
// (siloed). A complex problem is solved by an invocation emitting
// sub-APs (further invocations), recursively, until leaves return
// concrete results that recompose back up the tree.
//
// The envelope carries both the invocation in (BrainFunction, Input,
// OutputSchema, Budget) and the result out (Output, populated by the
// specialist on exit). Lean by design — token cost compounds over
// every hop. Fat learning-loop records live in pkg/trace, not here.
//
// See parent CLAUDE.md "Architecture" (locked 2026-05-18) and
// oscillitron/CLAUDE.md "Pending rework: call-tree model".
package session

import (
	"github.com/jrlmx2/oscillitron/pkg/classification"
)

// SchemaVersion lets the envelope evolve additively. Bumped only on
// breaking shape changes; readers can downgrade gracefully.
const SchemaVersion = "v1"

// ID uniquely identifies an envelope (an invocation in the call tree).
type ID string

// BrainFunction is the cognitive role an invocation invokes. Each AP
// invokes exactly one (siloed). The registry binds these names to
// specialist instances at runtime; names here are conventional.
type BrainFunction string

const (
	BrainPerception  BrainFunction = "perception"  // parse / classify input
	BrainRetrieval   BrainFunction = "retrieval"   // pull relevant context
	BrainPlanning    BrainFunction = "planning"    // decompose into steps
	BrainReasoning   BrainFunction = "reasoning"   // apply transformation, derive
	BrainCritic      BrainFunction = "critic"      // verify / check
	BrainComposition BrainFunction = "composition" // produce output artifact
)

// ExitReason records why an invocation exited.
type ExitReason string

const (
	// ExitDone — invocation finished within its budget. Output is final
	// for this AP (modulo sub-AP resolution and recomposition).
	ExitDone ExitReason = "done"
	// ExitBudgetExhausted — invocation hit its token/depth budget.
	// Output captures progress + remaining work.
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

// Envelope is one AP — a call to one brain function on one input, plus
// (after the invocation runs) the result it produced.
type Envelope struct {
	SchemaVersion  string               `json:"schema_version"`
	ID             ID                   `json:"id"`
	BrainFunction  BrainFunction        `json:"brain_function"`
	Classification classification.Level `json:"classification"`
	Input          Input                `json:"input"`
	// OutputSchema is the contract describing what a successful Output
	// must contain. The producing brain function's prompt prepends this
	// as the preloaded self-classification requirement: every LLM
	// invocation is required to classify its output against this schema.
	OutputSchema string `json:"output_schema"`
	// ParentRef is the parent invocation's ID (nil for the root).
	ParentRef *ID    `json:"parent_ref,omitempty"`
	Budget    Budget `json:"budget"`
	// Output is nil until the invocation runs and the adapter
	// populates it. After sub-AP resolution + recomposition, Output may
	// be replaced with the composed result.
	Output *Output `json:"output,omitempty"`
	// Trace is lean per-AP metrics. The fat learning-loop record lives
	// in pkg/trace, off the inference path.
	Trace Trace `json:"trace"`
	// Audit is populated by the audit ledger from Phase 4 onward. Nil
	// through Phase 3.
	Audit *Audit `json:"audit,omitempty"`
}

// Input is what the invocation operates on.
type Input struct {
	Type        string `json:"type"`         // "prompt", "subap_result", etc.
	Content     string `json:"content"`
	ContentHash string `json:"content_hash"` // sha256:hex; orchestrator populates
}

// SubAPSeed is what an invocation emits to spawn a child AP. Enough
// information to construct the child envelope at dispatch time.
type SubAPSeed struct {
	BrainFunction BrainFunction `json:"brain_function"`
	Input         Input         `json:"input"`
	OutputSchema  string        `json:"output_schema"`
}

// Output is the invocation's product. Populated by the specialist's
// adapter when the invocation exits.
type Output struct {
	// Content is the actual produced result.
	Content string `json:"content"`
	// Classification is the LLM-emitted self-classification against
	// OutputSchema. Preloaded prompt requirement forces this.
	Classification string `json:"classification"`
	// Confidence is the LLM-emitted confidence in this output. Drives
	// inhibitor checks and (eventually) recomposer weighting.
	Confidence float64 `json:"confidence"`
	// Signals are amorphous LLM-emitted grounding notes. Same channel
	// the inhibitor reads for drift detection.
	Signals []string `json:"signals,omitempty"`
	// Contradictions are self-reported contradictions with prior
	// context. Separate from Signals because the inhibitor weights
	// them more heavily.
	Contradictions []string `json:"contradictions,omitempty"`
	// OpenQuestions are unresolved threads the invocation noticed but
	// did not pursue. May seed future SubAPs.
	OpenQuestions []string `json:"open_questions,omitempty"`
	// SubAPs are the child invocations this one wants spawned before
	// it is complete. Empty means this is a leaf (no further descent).
	SubAPs []SubAPSeed `json:"sub_aps,omitempty"`
	// ExitReason records why the invocation exited.
	ExitReason ExitReason `json:"exit_reason"`
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

// NewRoot constructs a root envelope (no parent) — the entry point a
// caller fires at the tree-walker. Generated ID is the caller's
// responsibility; budget governs the whole subtree.
func NewRoot(id ID, bf BrainFunction, prompt, outputSchema string, budget Budget) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersion,
		ID:            id,
		BrainFunction: bf,
		Input:         Input{Type: "prompt", Content: prompt},
		OutputSchema:  outputSchema,
		Budget:        budget,
	}
}

// IsLeaf reports whether this invocation requested no further sub-APs.
// A leaf invocation's Output is the final result for this branch (no
// recomposition needed below).
func (e *Envelope) IsLeaf() bool {
	return e.Output != nil && len(e.Output.SubAPs) == 0
}

// IsInhibited reports whether the invocation (or its subtree) was
// aborted by the inhibitor.
func (e *Envelope) IsInhibited() bool {
	return e.Output != nil && e.Output.ExitReason == ExitInhibited
}

// IsComplete reports whether the invocation has run (Output is set).
// Does not imply the subtree below it has resolved.
func (e *Envelope) IsComplete() bool {
	return e.Output != nil
}
