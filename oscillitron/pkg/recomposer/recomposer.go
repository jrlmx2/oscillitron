// CLAUDE GENERATED
// Package recomposer combines a parent invocation's resolved children
// into the parent's final Output. Under the call-tree model
// recomposition is load-bearing: every non-leaf invocation in the
// tree passes through a recomposer once its SubAPs have been
// dispatched and returned.
//
// v0 ships a generic Concat recomposer (simple content join, signals
// aggregated, confidence min). Per-brain-function recomposers (e.g. an
// LLM-driven recompose step that re-invokes the parent brain function
// with children's outputs as context) arrive later.
package recomposer

import (
	"context"
	"errors"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Recomposer collapses a parent's initial Output plus its resolved
// children into a single composed Output. The composed Output has no
// SubAPs (the children have, by definition, resolved).
type Recomposer interface {
	Recompose(ctx context.Context, parent session.Output, children []session.Envelope) (session.Output, error)
}

// Concat joins parent and children Content with a separator. Confidence
// is the weakest-link min across parent + children. Signals,
// contradictions, and open questions are deduplicated unions.
type Concat struct {
	// Separator placed between parent content and each child content.
	// Defaults to "\n\n---\n\n" when empty.
	Separator string
}

// Recompose implements Recomposer.
func (c Concat) Recompose(_ context.Context, parent session.Output, children []session.Envelope) (session.Output, error) {
	if len(children) == 0 {
		return session.Output{}, errors.New("recomposer: no children to recompose")
	}
	sep := c.Separator
	if sep == "" {
		sep = "\n\n---\n\n"
	}

	parts := make([]string, 0, 1+len(children))
	if parent.Content != "" {
		parts = append(parts, parent.Content)
	}

	minConf := parent.Confidence
	confSet := parent.Confidence > 0
	signals := append([]string(nil), parent.Signals...)
	contradictions := append([]string(nil), parent.Contradictions...)
	questions := append([]string(nil), parent.OpenQuestions...)

	for _, child := range children {
		if child.Output == nil {
			continue
		}
		if child.Output.Content != "" {
			parts = append(parts, child.Output.Content)
		}
		if child.Output.Confidence > 0 {
			if !confSet || child.Output.Confidence < minConf {
				minConf = child.Output.Confidence
				confSet = true
			}
		}
		signals = appendUnique(signals, child.Output.Signals)
		contradictions = appendUnique(contradictions, child.Output.Contradictions)
		questions = appendUnique(questions, child.Output.OpenQuestions)
	}

	if len(parts) == 0 {
		return session.Output{}, errors.New("recomposer: nothing to recompose (no content anywhere)")
	}
	if !confSet {
		minConf = 0
	}

	return session.Output{
		Content:        strings.Join(parts, sep),
		Classification: parent.Classification,
		Confidence:     minConf,
		Signals:        signals,
		Contradictions: contradictions,
		OpenQuestions:  questions,
		ExitReason:     session.ExitDone,
	}, nil
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
