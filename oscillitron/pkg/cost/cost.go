// CLAUDE GENERATED
// Package cost tracks per-call spend in two parallel ledgers: actual
// (what the chosen model cost) and counterfactual (what a frontier
// baseline would have cost for the same token volume). The
// counterfactual exists because Oscillitron's Phase 1 thesis is "we
// beat frontier on cost" — without the comparison column, "cheap" is
// unverifiable.
//
// Per library-plan §2.3 (Phase 1 deliverables) and §2.5 (decision
// gate). Populates session.Trace.CostUSD and
// session.Trace.CostVsFrontierBaselineUSD when a tracker is wired
// into the runner — that wiring lands when a real adapter does.
package cost

import (
	"fmt"
	"sync"
)

// Pricing is per-million-token rates for a single model. Use
// per-million to match how providers usually publish prices and to
// avoid float-precision games on per-token numbers.
type Pricing struct {
	InputUSDPerMTok  float64
	OutputUSDPerMTok float64
}

// Cost computes actual USD for a token volume.
func (p Pricing) Cost(tokensIn, tokensOut int) float64 {
	return (float64(tokensIn)*p.InputUSDPerMTok + float64(tokensOut)*p.OutputUSDPerMTok) / 1_000_000
}

// Entry is one recorded call.
type Entry struct {
	Model        string
	TokensInput  int
	TokensOutput int
	ActualUSD    float64
	FrontierUSD  float64
	SavingsUSD   float64 // FrontierUSD - ActualUSD
}

// Summary aggregates entries.
type Summary struct {
	Entries          []Entry
	TotalActualUSD   float64
	TotalFrontierUSD float64
	TotalSavingsUSD  float64
}

// Tracker records calls and computes summaries. Goroutine-safe.
type Tracker struct {
	frontier   Pricing
	prices     map[string]Pricing
	mu         sync.Mutex
	entries    []Entry
	totals     Summary // running totals (Entries is filled on Summary())
}

// New constructs a tracker. The frontier pricing is the baseline used
// for every counterfactual calculation regardless of which model
// actually handled a call. Add per-model pricing with Register.
func New(frontier Pricing) *Tracker {
	return &Tracker{
		frontier: frontier,
		prices:   make(map[string]Pricing),
	}
}

// Register sets pricing for a model. Overwrites any prior entry.
func (t *Tracker) Register(model string, p Pricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prices[model] = p
}

// Record adds a call and returns the computed Entry. If the model
// has no registered pricing, the call is recorded with ActualUSD=0
// and the entry's Model field annotated; the counterfactual still
// computes against the frontier baseline. An unpriced model is a
// configuration smell, not a runtime error — the eval harness will
// still produce a meaningful frontier total.
func (t *Tracker) Record(model string, tokensIn, tokensOut int) Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	var actual float64
	displayModel := model
	if p, ok := t.prices[model]; ok {
		actual = p.Cost(tokensIn, tokensOut)
	} else {
		displayModel = fmt.Sprintf("%s (unpriced)", model)
	}
	frontier := t.frontier.Cost(tokensIn, tokensOut)
	e := Entry{
		Model:        displayModel,
		TokensInput:  tokensIn,
		TokensOutput: tokensOut,
		ActualUSD:    actual,
		FrontierUSD:  frontier,
		SavingsUSD:   frontier - actual,
	}
	t.entries = append(t.entries, e)
	t.totals.TotalActualUSD += actual
	t.totals.TotalFrontierUSD += frontier
	t.totals.TotalSavingsUSD += e.SavingsUSD
	return e
}

// Summary returns a snapshot of recorded calls and running totals.
// Safe to call concurrently with Record.
func (t *Tracker) Summary() Summary {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]Entry, len(t.entries))
	copy(entries, t.entries)
	return Summary{
		Entries:          entries,
		TotalActualUSD:   t.totals.TotalActualUSD,
		TotalFrontierUSD: t.totals.TotalFrontierUSD,
		TotalSavingsUSD:  t.totals.TotalSavingsUSD,
	}
}

// SavingsRatio is TotalSavingsUSD / TotalFrontierUSD. Returns 0 when
// the frontier total is 0 (no calls recorded yet).
func (s Summary) SavingsRatio() float64 {
	if s.TotalFrontierUSD == 0 {
		return 0
	}
	return s.TotalSavingsUSD / s.TotalFrontierUSD
}
