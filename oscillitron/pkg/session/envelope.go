// CLAUDE GENERATED
// Package session defines the canonical session envelope — the
// AP/handoff shape between oscillators.
//
// Per library-plan §5.1 and design-notes.md "Action potentials are
// summaries": the Outcome field IS the AP body. A specialist exits a
// session by producing an Outcome; that Outcome propagates to the
// next specialist as its Input.
//
// Schema-stability commitment (library-plan §2.4): Phase 2+ may ADD
// fields, but must not rename or restructure existing ones. Phase 1
// envelopes must remain replayable.
package session

import (
	"github.com/jrlmx2/oscillitron/pkg/classification"
)

// ID uniquely identifies a session.
type ID string

// Type categorizes the session's role in a reasoning chain.
type Type string

const (
	TypeDecompose  Type = "decompose"
	TypeAnalyze    Type = "analyze"
	TypeMerge      Type = "merge"
	TypeSynthesize Type = "synthesize"
)

// ExitReason records why a session exited. Per design-notes.md
// "Specialist lifecycle: session-bounded with token-budget threshold":
// downstream specialists need to know which kind of summary they're
// receiving — they trigger different next moves.
type ExitReason string

const (
	// ExitDone — the specialist finished the task within its budget.
	// The Outcome IS the answer.
	ExitDone ExitReason = "done"
	// ExitBudgetExhausted — the specialist hit the token-budget
	// threshold (~70% of context window) before finishing. The Outcome
	// is "where I got, what I tried, what remains."
	ExitBudgetExhausted ExitReason = "budget_exhausted"
	// ExitInhibited — the inhibitor aborted the session before the
	// specialist exited naturally. The Outcome captures what was tried
	// and the inhibition signal.
	ExitInhibited ExitReason = "inhibited"
)

// Envelope is the canonical session envelope. Carries everything a
// downstream specialist (or the orchestrator) needs to reason about
// the session and what produced it.
type Envelope struct {
	ID             ID                   `json:"session_id"`
	Type           Type                 `json:"session_type"`
	Objective      string               `json:"objective"`
	Classification classification.Level `json:"classification"`
	Notes          Notes                `json:"notes"`
	Input          Input                `json:"input"`
	Outcome        *Outcome             `json:"outcome,omitempty"`
	Routing        Routing              `json:"routing"`
	Trace          Trace                `json:"trace"`
	Audit          *Audit               `json:"audit,omitempty"` // nil through Phase 3; populated Phase 4
}

// Notes carries context that doesn't fit cleanly in Input.
type Notes struct {
	Constraints  []string `json:"constraints"`
	PriorSignals []string `json:"prior_signals"`
	ContextTags  []string `json:"context_tags"`
}

// Input is what the specialist is asked to work on.
type Input struct {
	Type        string `json:"type"` // "prompt", "outcome_handoff", etc.
	Content     string `json:"content"`
	ContentHash string `json:"content_hash"` // sha256:hex; populated by the orchestrator
}

// Outcome is the specialist's exit summary — the AP body.
//
// TokensInput/TokensOutput are populated by adapters that report
// usage (the Hermes adapter parses ACP usage_update notifications).
// The oscillator copies them into Envelope.Trace before emitting so
// the cost layer sees them without inspecting Outcome. Zero values
// are fine for adapters that don't report usage (e.g. stub).
type Outcome struct {
	ExitReason     ExitReason `json:"exit_reason"`
	Verdict        string     `json:"verdict"`
	Signals        []string   `json:"signals"`
	Confidence     float64    `json:"confidence"`
	OpenQuestions  []string   `json:"open_questions"`
	Contradictions []string   `json:"contradictions"`
	FeedsInto      []ID       `json:"feeds_into"`
	TokensInput    int        `json:"tokens_input,omitempty"`
	TokensOutput   int        `json:"tokens_output,omitempty"`
}

// Routing records what backend handled the session and why.
type Routing struct {
	Model                    string `json:"model"`
	ModelHash                string `json:"model_hash"`
	Reason                   string `json:"reason"`
	ClassificationConstraint string `json:"classification_constraint"`
}

// Trace records per-session operational metrics.
type Trace struct {
	TokensInput               int     `json:"tokens_input"`
	TokensOutput              int     `json:"tokens_output"`
	DurationMs                int64   `json:"duration_ms"`
	ParentSession             *ID     `json:"parent_session,omitempty"`
	CostUSD                   float64 `json:"cost_usd"`
	CostVsFrontierBaselineUSD float64 `json:"cost_vs_frontier_baseline_usd"`
}

// Audit is populated by the audit ledger from Phase 4 onward. Nil
// through Phase 3.
type Audit struct {
	LedgerID  string `json:"ledger_id"`
	SignedAt  string `json:"signed_at"` // RFC3339
	Signature string `json:"signature"`
}

// IsTerminal reports whether the envelope's outcome represents a
// stopping point — either a finished answer or an inhibitor abort.
// Budget-exhausted outcomes are NOT terminal: they're meant to be
// handed to another specialist.
func (e *Envelope) IsTerminal() bool {
	if e.Outcome == nil {
		return false
	}
	switch e.Outcome.ExitReason {
	case ExitDone, ExitInhibited:
		return true
	}
	return false
}
