package recomposer

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// SynthStub is a deterministic in-process Synthesizer for tests and
// the demo. Default behavior joins Left + Right with a "+" marker so
// it is easy to assert a step ran; configurable via WithFormat.
type SynthStub struct {
	name       string
	format     func(req SynthesizeRequest) string
	err        error
	calls      atomic.Int64
	confidence float64
	kind       string
}

// NewSynthStub returns a stub that produces "{left}+{right}" per step
// and reports zero confidence (the recomposer falls back to weakest-
// link min on zero).
func NewSynthStub(name string) *SynthStub {
	return &SynthStub{
		name: name,
		format: func(req SynthesizeRequest) string {
			return fmt.Sprintf("%s+%s", req.Left.Result.Content, req.Right.Result.Content)
		},
	}
}

// WithFormat replaces the default "{left}+{right}" content builder.
func (s *SynthStub) WithFormat(fn func(req SynthesizeRequest) string) *SynthStub {
	s.format = fn
	return s
}

// WithError makes Synthesize fail with err on every call.
func (s *SynthStub) WithError(err error) *SynthStub { s.err = err; return s }

// WithConfidence overrides the response's Confidence. Setting it to a
// positive value bypasses the recomposer's weakest-link fallback.
func (s *SynthStub) WithConfidence(c float64) *SynthStub { s.confidence = c; return s }

// WithKind overrides Kind on the response (default "result").
func (s *SynthStub) WithKind(k string) *SynthStub { s.kind = k; return s }

// Name implements Synthesizer.
func (s *SynthStub) Name() string { return s.name }

// Calls returns how many Synthesize calls were made.
func (s *SynthStub) Calls() int64 { return s.calls.Load() }

// Synthesize implements Synthesizer.
func (s *SynthStub) Synthesize(ctx context.Context, req SynthesizeRequest) (SynthesizeResponse, error) {
	s.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return SynthesizeResponse{}, err
	}
	if s.err != nil {
		return SynthesizeResponse{}, s.err
	}
	return SynthesizeResponse{
		Content:    s.format(req),
		Kind:       s.kind,
		Confidence: s.confidence,
		TokensUsed: 75,
	}, nil
}

// stepIndexFormat is a convenience format that includes the StepIndex
// in the rendered content. Used in tests to verify the per-call counter
// rises monotonically across a fold.
func stepIndexFormat() func(SynthesizeRequest) string {
	return func(req SynthesizeRequest) string {
		return fmt.Sprintf("step%d(%s,%s)", req.StepIndex, req.Left.Result.Content, req.Right.Result.Content)
	}
}

var _ Synthesizer = (*SynthStub)(nil)

// Compile-time check that session is in use so a future refactor that
// removes the field doesn't leave an unused import in this file.
var _ = session.RecomposeNone
