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
	"time"
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

// HardwarePricing models self-hosted inference cost as wall-clock time
// on owned/leased hardware. The per-call cost is:
//
//	cost = duration.Seconds() × USDPerHour / 3600
//
// This replaces per-token pricing for local substrates where there is
// no API bill — the cost is amortized hardware + electricity × time.
type HardwarePricing struct {
	USDPerHour float64
}

// Cost returns the USD cost for the given wall-clock duration.
func (h HardwarePricing) Cost(d time.Duration) float64 {
	return d.Seconds() * h.USDPerHour / 3600
}

// Entry is one recorded call.
type Entry struct {
	Model           string
	TokensInput     int
	TokensOutput    int
	Duration        time.Duration
	ActualUSD       float64
	HardwareCostUSD float64 // wall-clock × hourly rate (zero when no HardwarePricing)
	FrontierUSD     float64
	SavingsUSD      float64 // FrontierUSD - max(ActualUSD, HardwareCostUSD)
}

// Summary aggregates entries.
type Summary struct {
	Entries              []Entry
	TotalActualUSD       float64
	TotalHardwareCostUSD float64
	TotalFrontierUSD     float64
	TotalSavingsUSD      float64
	TotalDuration        time.Duration
}

// Tracker records calls and computes summaries. Goroutine-safe.
type Tracker struct {
	frontier Pricing
	hardware *HardwarePricing // nil = no hardware cost tracking
	prices   map[string]Pricing
	mu       sync.Mutex
	entries  []Entry
	totals   Summary // running totals (Entries is filled on Summary())
}

// New constructs a tracker. The frontier pricing is the baseline used
// for every counterfactual calculation regardless of which model
// actually handled a call. Add per-model pricing with Register.
// opts may contain at most one HardwarePricing to enable time-based
// cost tracking for self-hosted substrates.
func New(frontier Pricing, opts ...HardwarePricing) *Tracker {
	t := &Tracker{
		frontier: frontier,
		prices:   make(map[string]Pricing),
	}
	if len(opts) > 0 {
		hp := opts[0]
		t.hardware = &hp
	}
	return t
}

// Register sets pricing for a model. Overwrites any prior entry.
func (t *Tracker) Register(model string, p Pricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prices[model] = p
}

// Record adds a call without duration and returns the computed Entry.
// For self-hosted substrates where wall-clock cost matters, use
// RecordWithDuration instead. If the model has no registered
// pricing, the call is recorded with ActualUSD=0 and the entry's
// Model field annotated; the counterfactual still computes against
// the frontier baseline.
func (t *Tracker) Record(model string, tokensIn, tokensOut int) Entry {
	return t.RecordWithDuration(model, tokensIn, tokensOut, 0)
}

// RecordWithDuration adds a call with its wall-clock duration and
// returns the computed Entry. When HardwarePricing is configured,
// HardwareCostUSD = duration × hourly rate. SavingsUSD is computed
// against whichever actual cost is higher (token-based or
// hardware-based) so the frontier comparison is conservative.
func (t *Tracker) RecordWithDuration(model string, tokensIn, tokensOut int, elapsed time.Duration) Entry {
	t.mu.Lock()
	defer t.mu.Unlock()

	var actual float64
	displayModel := model
	if p, ok := t.prices[model]; ok {
		actual = p.Cost(tokensIn, tokensOut)
	} else {
		displayModel = fmt.Sprintf("%s (unpriced)", model)
	}

	var hwCost float64
	if t.hardware != nil && elapsed > 0 {
		hwCost = t.hardware.Cost(elapsed)
	}

	frontier := t.frontier.Cost(tokensIn, tokensOut)
	effectiveActual := actual
	if hwCost > effectiveActual {
		effectiveActual = hwCost
	}
	e := Entry{
		Model:           displayModel,
		TokensInput:     tokensIn,
		TokensOutput:    tokensOut,
		Duration:        elapsed,
		ActualUSD:       actual,
		HardwareCostUSD: hwCost,
		FrontierUSD:     frontier,
		SavingsUSD:      frontier - effectiveActual,
	}
	t.entries = append(t.entries, e)
	t.totals.TotalActualUSD += actual
	t.totals.TotalHardwareCostUSD += hwCost
	t.totals.TotalFrontierUSD += frontier
	t.totals.TotalSavingsUSD += e.SavingsUSD
	t.totals.TotalDuration += elapsed
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
		Entries:              entries,
		TotalActualUSD:       t.totals.TotalActualUSD,
		TotalHardwareCostUSD: t.totals.TotalHardwareCostUSD,
		TotalFrontierUSD:     t.totals.TotalFrontierUSD,
		TotalSavingsUSD:      t.totals.TotalSavingsUSD,
		TotalDuration:        t.totals.TotalDuration,
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
