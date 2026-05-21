// CLAUDE GENERATED
package judge

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Stub is a configurable in-process Judge for tests and the demo. By
// default it agrees with the local critique on every call. Configure
// with WithDisagreeWhen to override.
type Stub struct {
	name        string
	disagreeOn  func(req Request) bool
	verdict     session.Verdict
	issues      []session.Issue
	err         error
	calls       atomic.Int64
	tokensUsed  int
}

// NewStub returns a stub judge that always agrees with the local
// critique unless reconfigured.
func NewStub(name string) *Stub {
	return &Stub{
		name:       name,
		tokensUsed: 50,
	}
}

// WithDisagreeWhen installs a predicate; when it returns true for a
// request, the judge returns a disagreeing verdict. Useful for forcing
// the runner's disagreement-recording path in tests.
func (s *Stub) WithDisagreeWhen(p func(req Request) bool) *Stub { s.disagreeOn = p; return s }

// WithVerdict sets the judge's verdict on agreement. Default behavior
// is to mirror the local verdict; setting this overrides.
func (s *Stub) WithVerdict(v session.Verdict, issues ...session.Issue) *Stub {
	s.verdict = v
	s.issues = append([]session.Issue(nil), issues...)
	return s
}

// WithError makes the judge return err on every call. The runner
// treats this as "no audit signal for this AP."
func (s *Stub) WithError(err error) *Stub { s.err = err; return s }

// WithTokens overrides the reported token usage.
func (s *Stub) WithTokens(n int) *Stub { s.tokensUsed = n; return s }

// Name implements Judge.
func (s *Stub) Name() string { return s.name }

// Calls returns how many times Judge was invoked.
func (s *Stub) Calls() int64 { return s.calls.Load() }

// Judge implements Judge.
func (s *Stub) Judge(ctx context.Context, req Request) (Response, error) {
	s.calls.Add(1)
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if s.err != nil {
		return Response{}, s.err
	}
	verdict := req.LocalVerdict
	if s.verdict != "" {
		verdict = s.verdict
	}
	if s.disagreeOn != nil && s.disagreeOn(req) {
		verdict = flipVerdict(req.LocalVerdict)
	}
	return Response{
		Verdict:    verdict,
		Issues:     append([]session.Issue(nil), s.issues...),
		TokensUsed: s.tokensUsed,
	}, nil
}

// flipVerdict returns a verdict that disagrees with v. pass <-> fail;
// issues <-> pass.
func flipVerdict(v session.Verdict) session.Verdict {
	switch v {
	case session.VerdictPass:
		return session.VerdictFail
	case session.VerdictFail:
		return session.VerdictPass
	case session.VerdictIssues:
		return session.VerdictPass
	}
	return session.VerdictFail
}

// ErrStubUnconfigured is returned by WithError-wired stubs that want a
// recognizable sentinel.
var ErrStubUnconfigured = errors.New("judge stub: unconfigured")
