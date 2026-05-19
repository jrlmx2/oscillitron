// CLAUDE GENERATED
package tree

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func env(verdict string, conf float64, signals ...string) session.Envelope {
	return session.Envelope{
		Outcome: &session.Outcome{
			ExitReason: session.ExitDone,
			Verdict:    verdict,
			Confidence: conf,
			Signals:    signals,
		},
	}
}

func TestTree_EmptyErrors(t *testing.T) {
	if _, err := New(nil).Recompose(context.Background(), nil); err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestTree_Single(t *testing.T) {
	out, err := New(nil).Recompose(context.Background(), []session.Envelope{env("A", 0.5, "x")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome.Verdict != "A" || out.Outcome.Confidence != 0.5 {
		t.Errorf("single passthrough wrong: %+v", out.Outcome)
	}
}

func TestTree_AgreementBoostsConfidence(t *testing.T) {
	out, err := New(nil).Recompose(context.Background(), []session.Envelope{
		env("same answer", 0.5),
		env("same answer", 0.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome.Verdict != "same answer" {
		t.Errorf("verdict = %q, want %q", out.Outcome.Verdict, "same answer")
	}
	// 0.5 * 1.10 = 0.55
	if out.Outcome.Confidence < 0.54 || out.Outcome.Confidence > 0.56 {
		t.Errorf("confidence = %v, want ~0.55", out.Outcome.Confidence)
	}
	for _, c := range out.Outcome.Contradictions {
		if strings.Contains(c, "disagreement") {
			t.Errorf("agreement merge should not record disagreement: %v", out.Outcome.Contradictions)
		}
	}
}

func TestTree_DisagreementPenalizesAndRecords(t *testing.T) {
	out, err := New(nil).Recompose(context.Background(), []session.Envelope{
		env("answer A", 0.6),
		env("answer B", 0.8),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 0.7 * 0.90 = 0.63
	if out.Outcome.Confidence < 0.62 || out.Outcome.Confidence > 0.64 {
		t.Errorf("confidence = %v, want ~0.63", out.Outcome.Confidence)
	}
	if !strings.Contains(out.Outcome.Verdict, "[A]") || !strings.Contains(out.Outcome.Verdict, "[B]") {
		t.Errorf("verdict missing branch labels: %q", out.Outcome.Verdict)
	}
	found := false
	for _, c := range out.Outcome.Contradictions {
		if strings.Contains(c, "disagreement") {
			found = true
		}
	}
	if !found {
		t.Errorf("disagreement should be recorded in contradictions: %v", out.Outcome.Contradictions)
	}
}

func TestTree_OddCountCarry(t *testing.T) {
	// 3 inputs: pair (0,1) merges; (2) carries up; then pair the result with (2).
	out, err := New(nil).Recompose(context.Background(), []session.Envelope{
		env("X", 0.5, "sig-x"),
		env("X", 0.5, "sig-y"),
		env("X", 0.5, "sig-z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Round 1: pair (0,1) merges with agreement boost -> 0.5*1.10=0.55;
	//           (2) carries unchanged -> 0.5.
	// Round 2: pair (0.55, 0.5) -> mean=0.525, agreement boost -> 0.5775.
	if out.Outcome.Confidence < 0.57 || out.Outcome.Confidence > 0.59 {
		t.Errorf("confidence = %v, want ~0.5775", out.Outcome.Confidence)
	}
	want := map[string]bool{"sig-x": false, "sig-y": false, "sig-z": false}
	for _, s := range out.Outcome.Signals {
		want[s] = true
	}
	for k, v := range want {
		if !v {
			t.Errorf("signal %q missing from merged outcome (signals=%v)", k, out.Outcome.Signals)
		}
	}
}

func TestTree_TokensSummed(t *testing.T) {
	mk := func(in, out int) session.Envelope {
		return session.Envelope{Outcome: &session.Outcome{
			ExitReason: session.ExitDone, Verdict: "v", Confidence: 0.5,
			TokensInput: in, TokensOutput: out,
		}}
	}
	got, err := New(nil).Recompose(context.Background(), []session.Envelope{
		mk(10, 20), mk(30, 40),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome.TokensInput != 40 || got.Outcome.TokensOutput != 60 {
		t.Errorf("tokens not summed: in=%d out=%d", got.Outcome.TokensInput, got.Outcome.TokensOutput)
	}
}
