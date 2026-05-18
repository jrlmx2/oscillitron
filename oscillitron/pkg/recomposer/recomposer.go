// CLAUDE GENERATED
// Package recomposer merges the outcomes of a decomposed oscillation
// back into a single envelope. Per library-plan §5.5: the concat
// implementation joins verdicts in order; a tree.Recomposer (Phase 5)
// will pairwise-merge with conflict resolution.
//
// Ships the interface and a concat implementation in this file.
// Future impls (tree merge, voting, weighted blend) live in
// subpackages.
package recomposer

import (
	"context"
	"errors"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Recomposer collapses an ordered slice of outcome-bearing envelopes
// into a single envelope.
type Recomposer interface {
	Recompose(ctx context.Context, outcomes []session.Envelope) (session.Envelope, error)
}

// Concat joins outcome verdicts in input order with a separator. The
// returned envelope's Outcome.Confidence is the minimum across
// inputs (the weakest link sets the confidence floor). Signals,
// open questions, and contradictions are concatenated and
// deduplicated in input order.
type Concat struct {
	// Separator placed between joined verdicts. Defaults to "\n\n---\n\n".
	Separator string
}

// Recompose implements Recomposer.
func (c Concat) Recompose(_ context.Context, outcomes []session.Envelope) (session.Envelope, error) {
	if len(outcomes) == 0 {
		return session.Envelope{}, errors.New("recomposer: no outcomes to recompose")
	}
	sep := c.Separator
	if sep == "" {
		sep = "\n\n---\n\n"
	}

	verdicts := make([]string, 0, len(outcomes))
	minConf := 1.0
	confSet := false
	var signals, questions, contradictions []string
	var feedsInto []session.ID
	for _, e := range outcomes {
		if e.Outcome == nil {
			continue
		}
		verdicts = append(verdicts, e.Outcome.Verdict)
		if e.Outcome.Confidence > 0 {
			if !confSet || e.Outcome.Confidence < minConf {
				minConf = e.Outcome.Confidence
				confSet = true
			}
		}
		signals = appendUnique(signals, e.Outcome.Signals)
		questions = appendUnique(questions, e.Outcome.OpenQuestions)
		contradictions = appendUnique(contradictions, e.Outcome.Contradictions)
		feedsInto = append(feedsInto, e.Outcome.FeedsInto...)
	}
	if len(verdicts) == 0 {
		return session.Envelope{}, errors.New("recomposer: outcomes carry no verdicts")
	}
	if !confSet {
		minConf = 0
	}

	// Inherit non-payload fields from the first envelope as a sensible
	// default — classification, type, objective. Caller can override.
	base := outcomes[0]
	merged := session.Envelope{
		Type:           base.Type,
		Objective:      base.Objective,
		Classification: base.Classification,
		Notes:          base.Notes,
		Input: session.Input{
			Type:    "recomposed",
			Content: strings.Join(verdicts, sep),
		},
		Outcome: &session.Outcome{
			ExitReason:     session.ExitDone,
			Verdict:        strings.Join(verdicts, sep),
			Signals:        signals,
			Confidence:     minConf,
			OpenQuestions:  questions,
			Contradictions: contradictions,
			FeedsInto:      feedsInto,
		},
	}
	return merged, nil
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

var _ Recomposer = Concat{}
