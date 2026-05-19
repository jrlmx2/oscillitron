// CLAUDE GENERATED
// Package tree implements a pairwise tree-reduce Recomposer per
// library-plan §5.5. The point of a tree (vs. Concat's flat join) is
// twofold:
//
//  1. Conflict resolution surfaces at every merge node — disagreements
//     between adjacent outcomes are visible in the merge metadata,
//     not flattened away.
//  2. The pair-merge seam (PairMerger) is swappable: v0 ships a
//     deterministic combiner; a future LLM-mediated PairMerger can
//     drop in without touching the tree shape.
//
// Tree shape: outcomes are reduced left-to-right pairwise. With N
// outcomes, the reduction does N-1 merges in ceil(log2(N)) rounds.
// Order matters: pair (0,1), (2,3), ... then merge those pair-results
// pairwise, until one outcome remains.
package tree

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/recomposer"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// PairMerger merges two outcomes into one. Implementations may be
// deterministic (v0) or LLM-mediated (later).
type PairMerger interface {
	Merge(ctx context.Context, a, b session.Outcome) (session.Outcome, error)
}

// Tree is the recomposer; it owns the tree shape and delegates each
// pair merge to the PairMerger.
type Tree struct {
	Merger PairMerger
}

// New constructs a Tree recomposer with the given PairMerger. If
// merger is nil, DeterministicPairMerger{} is used.
func New(merger PairMerger) Tree {
	if merger == nil {
		merger = DeterministicPairMerger{}
	}
	return Tree{Merger: merger}
}

// Recompose implements recomposer.Recomposer.
func (t Tree) Recompose(ctx context.Context, outcomes []session.Envelope) (session.Envelope, error) {
	if len(outcomes) == 0 {
		return session.Envelope{}, errors.New("recomposer/tree: no outcomes to recompose")
	}

	// Collect only envelopes with non-nil Outcomes.
	current := make([]session.Outcome, 0, len(outcomes))
	for _, e := range outcomes {
		if e.Outcome != nil {
			current = append(current, *e.Outcome)
		}
	}
	if len(current) == 0 {
		return session.Envelope{}, errors.New("recomposer/tree: outcomes carry no Outcome bodies")
	}

	// Pairwise reduce until one remains.
	for len(current) > 1 {
		next := make([]session.Outcome, 0, (len(current)+1)/2)
		for i := 0; i < len(current); i += 2 {
			if i+1 == len(current) {
				// Odd one out — promote unchanged. Matches "carry" in a
				// reduce; alternative would be to merge with the previous
				// pair-result, but that biases the tree shape.
				next = append(next, current[i])
				continue
			}
			merged, err := t.Merger.Merge(ctx, current[i], current[i+1])
			if err != nil {
				return session.Envelope{}, fmt.Errorf("recomposer/tree: pair merge: %w", err)
			}
			next = append(next, merged)
		}
		current = next
	}

	// Inherit non-payload fields from the first envelope.
	base := outcomes[0]
	final := current[0]
	return session.Envelope{
		Type:           base.Type,
		Objective:      base.Objective,
		Classification: base.Classification,
		Notes:          base.Notes,
		Input: session.Input{
			Type:    "recomposed",
			Content: final.Verdict,
		},
		Outcome: &final,
	}, nil
}

// DeterministicPairMerger is the v0 PairMerger: no LLM, no semantic
// reasoning. Combines verdicts with bracketed labels, lowers
// confidence when verdicts disagree exactly, unions side fields.
type DeterministicPairMerger struct{}

// Merge implements PairMerger.
func (DeterministicPairMerger) Merge(_ context.Context, a, b session.Outcome) (session.Outcome, error) {
	merged := session.Outcome{
		ExitReason:     mergeExit(a.ExitReason, b.ExitReason),
		Signals:        appendUnique(a.Signals, b.Signals),
		OpenQuestions:  appendUnique(a.OpenQuestions, b.OpenQuestions),
		Contradictions: appendUnique(a.Contradictions, b.Contradictions),
		FeedsInto:      append(append([]session.ID(nil), a.FeedsInto...), b.FeedsInto...),
		TokensInput:    a.TokensInput + b.TokensInput,
		TokensOutput:   a.TokensOutput + b.TokensOutput,
	}

	// Verdict & confidence: agreement bonus / disagreement penalty.
	// Exact match isn't a strong semantic signal — but for v0 it's the
	// only check we have without an LLM judge.
	if normalize(a.Verdict) == normalize(b.Verdict) && a.Verdict != "" {
		merged.Verdict = a.Verdict
		merged.Confidence = clamp01(meanConfidence(a, b) * 1.10) // +10% agreement bonus
	} else {
		var parts []string
		if a.Verdict != "" {
			parts = append(parts, "[A] "+a.Verdict)
		}
		if b.Verdict != "" {
			parts = append(parts, "[B] "+b.Verdict)
		}
		merged.Verdict = strings.Join(parts, "\n\n")
		merged.Confidence = clamp01(meanConfidence(a, b) * 0.90) // -10% disagreement penalty
		if a.Verdict != "" && b.Verdict != "" {
			merged.Contradictions = appendUnique(merged.Contradictions, []string{
				"pair-merge disagreement between branch A and branch B",
			})
		}
	}
	return merged, nil
}

// mergeExit picks the more conservative exit reason. ExitInhibited
// dominates ExitDone dominates ExitBudgetExhausted.
func mergeExit(a, b session.ExitReason) session.ExitReason {
	rank := func(r session.ExitReason) int {
		switch r {
		case session.ExitInhibited:
			return 2
		case session.ExitDone:
			return 1
		case session.ExitBudgetExhausted:
			return 0
		}
		return 0
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

func meanConfidence(a, b session.Outcome) float64 {
	switch {
	case a.Confidence == 0 && b.Confidence == 0:
		return 0
	case a.Confidence == 0:
		return b.Confidence
	case b.Confidence == 0:
		return a.Confidence
	default:
		return (a.Confidence + b.Confidence) / 2
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func normalize(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func appendUnique(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, s := range dst {
		seen[s] = struct{}{}
	}
	for _, s := range src {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		dst = append(dst, s)
	}
	return dst
}

var (
	_ recomposer.Recomposer = Tree{}
	_ PairMerger            = DeterministicPairMerger{}
)
