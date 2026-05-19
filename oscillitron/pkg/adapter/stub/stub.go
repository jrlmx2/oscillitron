// CLAUDE GENERATED
// Package stub provides a no-network Adapter for tests and the demo.
// It returns a hardcoded Output shaped by the adapter's configuration
// — useful for exercising the orchestration path without a real model.
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
	// ModeDone — return ExitDone with content derived from input.
	ModeDone Mode = iota
	// ModeBudgetExhausted — return ExitBudgetExhausted; the tree-walker
	// surfaces this as a partial-leaf result.
	ModeBudgetExhausted
	// ModeError — return an error from Call.
	ModeError
)

// Adapter is a deterministic, configurable stub.
type Adapter struct {
	name           string
	mode           Mode
	confidence     float64
	classification string
	signals        []string
	subAPs         []session.SubAPSeed
	calls          atomic.Int64
}

// New constructs a stub adapter.
func New(name string, mode Mode) *Adapter {
	return &Adapter{
		name:       name,
		mode:       mode,
		confidence: 0.5,
	}
}

// WithConfidence sets the confidence returned in Output.
func (a *Adapter) WithConfidence(c float64) *Adapter { a.confidence = c; return a }

// WithClassification sets the LLM-emitted self-classification.
func (a *Adapter) WithClassification(c string) *Adapter { a.classification = c; return a }

// WithSignals sets the amorphous grounding signals.
func (a *Adapter) WithSignals(s ...string) *Adapter {
	a.signals = append([]string(nil), s...)
	return a
}

// WithSubAPs makes the adapter emit child invocations. Each call to
// the adapter returns the same seeds, which the tree-walker dispatches.
func (a *Adapter) WithSubAPs(seeds ...session.SubAPSeed) *Adapter {
	a.subAPs = append([]session.SubAPSeed(nil), seeds...)
	return a
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return a.name }

// Calls reports how many times Call has been invoked. Useful in tests.
func (a *Adapter) Calls() int64 { return a.calls.Load() }

// Call implements adapter.Adapter.
func (a *Adapter) Call(ctx context.Context, env session.Envelope) (session.Output, error) {
	a.calls.Add(1)

	if err := ctx.Err(); err != nil {
		return session.Output{}, err
	}

	switch a.mode {
	case ModeError:
		return session.Output{}, errors.New("stub adapter: simulated error")
	case ModeBudgetExhausted:
		return session.Output{
			ExitReason:     session.ExitBudgetExhausted,
			Content:        fmt.Sprintf("[%s] partial progress on: %s", a.name, env.Input.Content),
			Classification: a.classification,
			Confidence:     a.confidence,
			Signals:        a.signals,
			SubAPs:         a.subAPs,
			OpenQuestions:  []string{"what remains: continue from here"},
		}, nil
	case ModeDone:
		fallthrough
	default:
		return session.Output{
			ExitReason:     session.ExitDone,
			Content:        fmt.Sprintf("[%s] handled: %s", a.name, env.Input.Content),
			Classification: a.classification,
			Confidence:     a.confidence,
			Signals:        a.signals,
			SubAPs:         a.subAPs,
		}, nil
	}
}

// Compile-time check.
var _ adapter.Adapter = (*Adapter)(nil)
