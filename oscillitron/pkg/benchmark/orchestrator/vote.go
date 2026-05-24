package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/benchmark"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/stakes"
	"github.com/jrlmx2/oscillitron/pkg/trace"
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
	// Tracer emits per-attempt and final-tally events for
	// observability. Nil = trace.Discard{}. Events emitted:
	//
	//   vote.attempt_start    case, orchestrator, attempt_idx
	//   vote.attempt_done     case, orchestrator, attempt_idx,
	//                         extracted, tokens, duration_ms
	//   vote.attempt_error    case, orchestrator, attempt_idx, err
	//   vote.tally            case, orchestrator, successes, errors,
	//                         winning_answer, winning_votes,
	//                         vote_distribution
	Tracer trace.Tracer
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

	tracer := v.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}

	// v3.0: scale attempt count by case stakes.
	//   Low    → 1 attempt   (cheap path; voting is overkill)
	//   Medium → v.N         (configured default)
	//   High   → 2 × v.N     (double effort on high-stakes)
	// Zero stakes (unset) reads as Medium via stakes.Effective().
	effectiveN := stakes.AttemptScale(c.Stakes, v.N)
	effectiveStakes := stakes.Effective(c.Stakes)

	type result struct {
		raw        string
		tokens     int
		confidence float64
		err        error
	}
	results := make([]result, effectiveN)
	var wg sync.WaitGroup
	for i := range effectiveN {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Stamp attempt_idx so the adapter/governor events
			// downstream of this goroutine carry it too.
			aCtx := trace.WithCorrelation(ctx, "attempt_idx", strconv.Itoa(i))
			start := time.Now()
			trace.Info(tracer, aCtx, "vote.attempt_start")
			lease, err := v.Governor.Acquire(aCtx)
			if err != nil {
				results[i].err = fmt.Errorf("governor acquire: %w", err)
				trace.Error(tracer, aCtx, "vote.attempt_error",
					slog.String("err", results[i].err.Error()),
				)
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
			env.Stakes = effectiveStakes
			env.Evaluate = &session.Evaluate{
				Playbook:   session.PlaybookProcess,
				Confidence: 1.0,
			}
			out, err := v.Adapter.Execute(aCtx, env)
			if err != nil {
				results[i].err = fmt.Errorf("execute: %w", err)
				trace.Error(tracer, aCtx, "vote.attempt_error",
					slog.String("err", results[i].err.Error()),
				)
				return
			}
			if out.Execute == nil || out.Execute.ReturnResult == nil {
				results[i].err = fmt.Errorf("empty return_result")
				trace.Error(tracer, aCtx, "vote.attempt_error",
					slog.String("err", results[i].err.Error()),
				)
				return
			}
			results[i].raw = out.Execute.ReturnResult.Result.Content
			results[i].tokens = out.Execute.TokensUsed
			results[i].confidence = out.Execute.ReturnResult.Confidence
			extracted := v.Extractor.Extract(results[i].raw)
			trace.Info(tracer, aCtx, "vote.attempt_done",
				slog.String("extracted", extracted),
				slog.Int("tokens", results[i].tokens),
				slog.Float64("confidence", results[i].confidence),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		}()
	}
	wg.Wait()

	// Tally votes. Excluded: failed attempts and empty extractions.
	votes := map[string]int{}
	var rawParts []string
	totalTokens := 0
	successes := 0
	// v3.3: aggregate per-attempt confidence into the returned
	// Answer. Mean across successful attempts that reported one.
	// Zero attempts reporting confidence ⇒ Answer.Confidence stays 0.
	var confidenceSum float64
	confidenceCount := 0
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
			// Failed extraction — don't count as a vote, and don't
			// fold its confidence into the aggregate either. An
			// attempt that produced confident-but-unextractable text
			// (e.g., "I'm sure the answer is in the middle somewhere
			// (confidence: 0.9)") would otherwise inflate the mean
			// and mislead the downstream cope dispatcher. Bug
			// empirically: 35 firings on phi4-mini's 198-case Diamond
			// run vs 1 on qwen2.5:7b — disproportionately impacts
			// weaker substrates.
			continue
		}
		if r.confidence > 0 {
			confidenceSum += r.confidence
			confidenceCount++
		}
		votes[extracted]++
	}
	if successes == 0 {
		return benchmark.Answer{}, fmt.Errorf("vote: all %d attempts failed (first err: %w)", effectiveN, firstErr)
	}
	if len(votes) == 0 {
		// Every attempt produced text but extraction failed on all.
		// Surface the answers verbatim so the grader can record the
		// failure mode rather than silently passing an empty.
		trace.Info(tracer, ctx, "vote.tally",
			slog.Int("attempts", effectiveN),
			slog.Int("successes", successes),
			slog.Int("errors", effectiveN-successes),
			slog.String("stakes", string(effectiveStakes)),
			slog.String("winning_answer", ""),
			slog.Int("winning_votes", 0),
			slog.String("distribution", "all-extractions-empty"),
		)
		return benchmark.Answer{
			Raw:        strings.Join(rawParts, "\n---\n"),
			Extracted:  "",
			Calls:      successes,
			TokensUsed: totalTokens,
			Confidence: meanConfidence(confidenceSum, confidenceCount),
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

	errCount := effectiveN - successes
	trace.Info(tracer, ctx, "vote.tally",
		slog.Int("attempts", effectiveN),
		slog.Int("successes", successes),
		slog.Int("errors", errCount),
		slog.String("stakes", string(effectiveStakes)),
		slog.String("winning_answer", bestKey),
		slog.Int("winning_votes", bestCount),
		slog.String("distribution", formatVoteDistribution(votes)),
	)

	return benchmark.Answer{
		Raw:        strings.Join(rawParts, "\n---\n"),
		Extracted:  bestKey,
		Calls:      successes,
		TokensUsed: totalTokens,
		Confidence: meanConfidence(confidenceSum, confidenceCount),
	}, nil
}

// meanConfidence returns sum/count or 0 when count==0.
func meanConfidence(sum float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

// formatVoteDistribution renders the vote map as a stable
// "A=3,B=1,C=1"-style string. Sorted alphabetically for
// reproducibility in trace output.
func formatVoteDistribution(votes map[string]int) string {
	keys := make([]string, 0, len(votes))
	for k := range votes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, votes[k])
	}
	return strings.Join(parts, ",")
}

// Compile-time check.
var _ benchmark.Orchestrator = Vote{}
