// CLAUDE GENERATED
// Package stub provides a no-network Adapter implementation for tests
// and the demo runner. It returns a hardcoded Outcome shaped by the
// adapter's configuration — useful for exercising the orchestration
// path without a real model.
package stub

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Mode controls what the stub does on each Call.
type Mode int

const (
	// ModeDone — return ExitDone with a synthesized verdict echoing the
	// inbound objective. The default and most common test mode.
	ModeDone Mode = iota
	// ModeBudgetExhausted — return ExitBudgetExhausted so the caller
	// must hand the AP onward.
	ModeBudgetExhausted
	// ModeError — return an error from Call.
	ModeError
)

// Adapter is a deterministic, configurable stub.
type Adapter struct {
	name         string
	mode         Mode
	confidence   float64
	signals      []string
	feedsInto    []session.ID
	tokensInput  int
	tokensOutput int
	calls        atomic.Int64
}

// New constructs a stub adapter.
func New(name string, mode Mode) *Adapter {
	return &Adapter{
		name:       name,
		mode:       mode,
		confidence: 0.5,
	}
}

// WithConfidence sets the confidence value returned in the Outcome.
func (a *Adapter) WithConfidence(c float64) *Adapter {
	a.confidence = c
	return a
}

// WithSignals sets the signals slice returned in the Outcome.
func (a *Adapter) WithSignals(s ...string) *Adapter {
	a.signals = append([]string(nil), s...)
	return a
}

// WithFeedsInto sets the FeedsInto slice returned in the Outcome.
func (a *Adapter) WithFeedsInto(ids ...session.ID) *Adapter {
	a.feedsInto = append([]session.ID(nil), ids...)
	return a
}

// WithTokens sets the TokensInput / TokensOutput counts the stub
// reports on each Call. Lets the cost tracker exercise.
func (a *Adapter) WithTokens(in, out int) *Adapter {
	a.tokensInput, a.tokensOutput = in, out
	return a
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return a.name }

// Calls reports how many times Call has been invoked. Useful in tests.
func (a *Adapter) Calls() int64 { return a.calls.Load() }

// Call implements adapter.Adapter.
func (a *Adapter) Call(ctx context.Context, env session.Envelope) (session.Outcome, error) {
	a.calls.Add(1)

	// Respect context cancellation — the orchestrator may abort us.
	if err := ctx.Err(); err != nil {
		return session.Outcome{}, err
	}

	switch a.mode {
	case ModeError:
		return session.Outcome{}, errors.New("stub adapter: simulated error")
	case ModeBudgetExhausted:
		return session.Outcome{
			ExitReason:    session.ExitBudgetExhausted,
			Verdict:       fmt.Sprintf("[%s] partial progress on: %s", a.name, env.Objective),
			Signals:       a.signals,
			Confidence:    a.confidence,
			OpenQuestions: []string{"what remains: continue from here"},
			FeedsInto:     a.feedsInto,
			TokensInput:   a.tokensInput,
			TokensOutput:  a.tokensOutput,
		}, nil
	case ModeDone:
		fallthrough
	default:
		return session.Outcome{
			ExitReason:   session.ExitDone,
			Verdict:      fmt.Sprintf("[%s] handled: %s", a.name, env.Objective),
			Signals:      a.signals,
			Confidence:   a.confidence,
			FeedsInto:    a.feedsInto,
			TokensInput:  a.tokensInput,
			TokensOutput: a.tokensOutput,
		}, nil
	}
}

// Compile-time check that *Adapter implements adapter.Adapter.
var _ adapter.Adapter = (*Adapter)(nil)
