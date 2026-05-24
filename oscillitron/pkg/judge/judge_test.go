package judge

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestDefaultSamplePolicy_v0Locks(t *testing.T) {
	p := DefaultSamplePolicy()
	if p.UngroundedRate != 1.0 {
		t.Errorf("UngroundedRate = %v, want 1.0 (locked 2026-05-19)", p.UngroundedRate)
	}
	if math.Abs(p.GroundedRate-0.1) > 1e-9 {
		t.Errorf("GroundedRate = %v, want 0.1 (locked 2026-05-19)", p.GroundedRate)
	}
}

// envFor builds a minimal return_result envelope, optionally grounded.
func envFor(grounded bool, verdict ...session.Verdict) session.Envelope {
	var pass *bool
	if grounded {
		b := true
		pass = &b
	}
	return session.Envelope{
		ID: "ap-target",
		Execute: &session.Execute{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result:     session.Payload{Kind: "result", Content: "x"},
				Confidence: 0.8,
				Signals:    session.Signals{GroundedPass: pass},
			},
		},
	}
}

func TestSampler_UngroundedAlwaysJudged(t *testing.T) {
	s := NewSampler(DefaultSamplePolicy())
	r := rand.New(rand.NewPCG(1, 2))
	env := envFor(false)
	for i := 0; i < 100; i++ {
		if !s.ShouldJudge(env, r) {
			t.Errorf("un-grounded target should always be judged at rate 1.0")
		}
	}
}

func TestSampler_GroundedSampledAtLockedRate(t *testing.T) {
	s := NewSampler(DefaultSamplePolicy())
	r := rand.New(rand.NewPCG(42, 1024))
	env := envFor(true)
	hits, trials := 0, 2000
	for i := 0; i < trials; i++ {
		if s.ShouldJudge(env, r) {
			hits++
		}
	}
	rate := float64(hits) / float64(trials)
	if rate < 0.07 || rate > 0.13 {
		t.Errorf("grounded sample rate observed = %v, want ~0.1 (±0.03)", rate)
	}
}

func TestSampler_SkipsNonReturnResultTargets(t *testing.T) {
	s := NewSampler(DefaultSamplePolicy())
	r := rand.New(rand.NewPCG(1, 2))
	// verifier_signal target — nothing to judge.
	env := session.Envelope{
		Execute: &session.Execute{
			Category:       session.CategoryVerifierSignal,
			VerifierSignal: &session.VerifierSignalPayload{Verdict: session.VerdictPass},
		},
	}
	if s.ShouldJudge(env, r) {
		t.Errorf("verifier_signal target should not be judged")
	}
	// nil Execute
	if s.ShouldJudge(session.Envelope{}, r) {
		t.Errorf("envelope with nil Execute should not be judged")
	}
}

func TestSampler_ZeroRateNeverJudges(t *testing.T) {
	s := NewSampler(SamplePolicy{UngroundedRate: 0, GroundedRate: 0})
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 100; i++ {
		if s.ShouldJudge(envFor(false), r) {
			t.Errorf("zero rate should never judge")
		}
	}
}

func TestIsGrounded(t *testing.T) {
	if IsGrounded(envFor(false)) {
		t.Errorf("ungrounded should report false")
	}
	if !IsGrounded(envFor(true)) {
		t.Errorf("grounded should report true")
	}
}

func TestStub_AgreesByDefault(t *testing.T) {
	s := NewStub("stub")
	resp, err := s.Judge(context.Background(), Request{LocalVerdict: session.VerdictPass})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if resp.Verdict != session.VerdictPass {
		t.Errorf("verdict = %q, want pass (mirrors local)", resp.Verdict)
	}
	if s.Calls() != 1 {
		t.Errorf("Calls() = %d, want 1", s.Calls())
	}
}

func TestStub_DisagreePredicate(t *testing.T) {
	s := NewStub("stub").WithDisagreeWhen(func(r Request) bool { return r.LocalVerdict == session.VerdictPass })
	// Disagree on pass → returns fail.
	resp, _ := s.Judge(context.Background(), Request{LocalVerdict: session.VerdictPass})
	if resp.Verdict != session.VerdictFail {
		t.Errorf("expected disagreement (fail); got %q", resp.Verdict)
	}
	// Agree on fail (predicate false) → mirrors fail.
	resp, _ = s.Judge(context.Background(), Request{LocalVerdict: session.VerdictFail})
	if resp.Verdict != session.VerdictFail {
		t.Errorf("expected agreement (fail); got %q", resp.Verdict)
	}
}

func TestStub_ErrorPath(t *testing.T) {
	want := errors.New("frontier down")
	s := NewStub("stub").WithError(want)
	_, err := s.Judge(context.Background(), Request{})
	if !errors.Is(err, want) {
		t.Errorf("got %v, want %v", err, want)
	}
}

func TestStub_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := NewStub("stub")
	_, err := s.Judge(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want canceled", err)
	}
}
