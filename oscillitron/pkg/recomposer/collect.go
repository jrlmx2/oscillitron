package recomposer

import (
	"context"
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Collect gathers all children's reasoning into a single text block
// instead of folding/synthesizing. The downstream extractor reads the
// full reasoning trail and pulls the answer.
type Collect struct{}

func (Collect) Recompose(_ context.Context, spec session.RecomposeSpec, children []session.ReturnResultPayload) (session.ReturnResultPayload, error) {
	if spec == session.RecomposeNone {
		return session.ReturnResultPayload{}, nil
	}
	if len(children) == 0 {
		return session.ReturnResultPayload{}, ErrNoChildren
	}
	if len(children) == 1 {
		return children[0], nil
	}

	var b strings.Builder
	minConf := children[0].Confidence
	for i, c := range children {
		if c.Confidence > 0 && (c.Confidence < minConf || minConf == 0) {
			minConf = c.Confidence
		}
		fmt.Fprintf(&b, "[Analysis %d]\n%s\n\n", i+1, strings.TrimSpace(c.Result.Content))
	}

	return session.ReturnResultPayload{
		Result:     session.Payload{Kind: "result", Content: strings.TrimSpace(b.String())},
		Confidence: minConf,
		Signals:    mergeSignals(children[0].Signals, children[len(children)-1].Signals),
	}, nil
}

var _ Recomposer = Collect{}
