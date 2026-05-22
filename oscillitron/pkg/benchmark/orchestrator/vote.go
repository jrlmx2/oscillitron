// CLAUDE GENERATED
package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

// Vote runs N independent adapter attempts per case and majority-
// votes the extracted answer. The orchestration arm of the bench —
// measures whether N cheap-model calls + voting can match or beat
// one frontier call (Single).
//
// Tie-breaking is deterministic: alphabetical on the extracted form.
// That keeps benchmark runs reproducible when two answers tie on
// vote count.
//
// Concurrency: attempts run in parallel via goroutines. Each attempt
// Acquires a lease from the Governor (when wired) before calling the
// adapter; Acquire blocks if VRAM headroom is short. Without a
// Governor, all N attempts dispatch immediately — fine for testing,
// risky in production.
//
// An empty extracted vote (extractor returned "") is treated as a
// failed extraction and excluded from the tally — counting empties
// as the majority would silently mask broken extractors.
type Vote struct {
	// NameStr appears in benchmark reports (e.g., "haiku-vote-5").
	// Required.
	NameStr string
	// Adapter is the substrate. Required.
	Adapter adapter.Adapter
	// N is the number of independent attempts per case. Required > 0.
	N int
	// Extractor turns each attempt's raw response into the canonical
	// answer form votes count on. Required.
	Extractor Extractor
	// Governor optionally bounds concurrent attempts against a shared
	// VRAM budget.
	Governor *vram.Governor
}

// Name implements benchmark.Orchestrator.
func (v Vote) Name() string { return v.NameStr }

// Answer implements benchmark.Orchestrator. Runs N attempts in
// parallel, extracts each, returns the majority vote.
func (v Vote) Answer(ctx context.Context, c benchmark.Case) (benchmark.Answer, error) {
	if v.Adapter == nil {
		return benchmark.Answer{}, fmt.Errorf("vote: Adapter is required")
	}
	if v.N <= 0 {
		return benchmark.Answer{}, fmt.Errorf("vote: N must be > 0, got %d", v.N)
	}
	if v.Extractor == nil {
		return benchmark.Answer{}, fmt.Errorf("vote: Extractor is required")
	}

	type result struct {
		raw    string
		tokens int
		err    error
	}
	results := make([]result, v.N)
	var wg sync.WaitGroup
	for i := 0; i < v.N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := v.Governor.Acquire(ctx)
			if err != nil {
				results[i].err = fmt.Errorf("governor acquire: %w", err)
				return
			}
			defer lease.Release()

			env := session.NewRoot(
				session.ID(fmt.Sprintf("bench-%s-attempt-%d", c.ID, i)),
				c.Prompt,
				"{answer}",
				classification.Internal,
				session.Budget{TokensRemaining: 32_000, DepthRemaining: 1},
			)
			env.Evaluate = &session.Evaluate{
				Playbook:   session.PlaybookProcess,
				Confidence: 1.0,
			}
			out, err := v.Adapter.Execute(ctx, env)
			if err != nil {
				results[i].err = fmt.Errorf("execute: %w", err)
				return
			}
			if out.Execute == nil || out.Execute.ReturnResult == nil {
				results[i].err = fmt.Errorf("empty return_result")
				return
			}
			results[i].raw = out.Execute.ReturnResult.Result.Content
			results[i].tokens = out.Execute.TokensUsed
		}()
	}
	wg.Wait()

	// Tally votes. Excluded: failed attempts and empty extractions.
	votes := map[string]int{}
	var rawParts []string
	totalTokens := 0
	successes := 0
	var firstErr error
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		extracted := v.Extractor.Extract(r.raw)
		rawParts = append(rawParts, r.raw)
		totalTokens += r.tokens
		successes++
		if extracted == "" {
			// Failed extraction — don't count as a vote.
			continue
		}
		votes[extracted]++
	}
	if successes == 0 {
		return benchmark.Answer{}, fmt.Errorf("vote: all %d attempts failed (first err: %w)", v.N, firstErr)
	}
	if len(votes) == 0 {
		// Every attempt produced text but extraction failed on all.
		// Surface the answers verbatim so the grader can record the
		// failure mode rather than silently passing an empty.
		return benchmark.Answer{
			Raw:        strings.Join(rawParts, "\n---\n"),
			Extracted:  "",
			Calls:      successes,
			TokensUsed: totalTokens,
		}, nil
	}

	// Pick the majority. Tie-break alphabetical (deterministic).
	bestKey := ""
	bestCount := -1
	for k, count := range votes {
		if count > bestCount {
			bestKey = k
			bestCount = count
			continue
		}
		if count == bestCount && k < bestKey {
			bestKey = k
		}
	}

	return benchmark.Answer{
		Raw:        strings.Join(rawParts, "\n---\n"),
		Extracted:  bestKey,
		Calls:      successes,
		TokensUsed: totalTokens,
	}, nil
}

// Compile-time check.
var _ benchmark.Orchestrator = Vote{}
