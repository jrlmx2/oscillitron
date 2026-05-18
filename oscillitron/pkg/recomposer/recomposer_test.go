// CLAUDE GENERATED
package recomposer

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func env(verdict string, conf float64, signals ...string) session.Envelope {
	return session.Envelope{
		Classification: classification.Internal,
		Outcome: &session.Outcome{
			Verdict:    verdict,
			Confidence: conf,
			Signals:    signals,
		},
	}
}

func TestConcatJoinsVerdicts(t *testing.T) {
	got, err := Concat{Separator: " | "}.Recompose(context.Background(),
		[]session.Envelope{env("a", 0.9), env("b", 0.7), env("c", 0.8)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Outcome.Verdict != "a | b | c" {
		t.Errorf("Verdict = %q", got.Outcome.Verdict)
	}
}

func TestConcatTakesMinConfidence(t *testing.T) {
	got, _ := Concat{}.Recompose(context.Background(),
		[]session.Envelope{env("a", 0.9), env("b", 0.4), env("c", 0.8)})
	if got.Outcome.Confidence != 0.4 {
		t.Errorf("Confidence = %f, want 0.4", got.Outcome.Confidence)
	}
}

func TestConcatDedupesSignals(t *testing.T) {
	got, _ := Concat{}.Recompose(context.Background(),
		[]session.Envelope{env("a", 0.5, "x", "y"), env("b", 0.5, "y", "z")})
	if len(got.Outcome.Signals) != 3 {
		t.Errorf("Signals = %v, want 3 unique", got.Outcome.Signals)
	}
	joined := strings.Join(got.Outcome.Signals, ",")
	for _, s := range []string{"x", "y", "z"} {
		if !strings.Contains(joined, s) {
			t.Errorf("missing %q in %v", s, got.Outcome.Signals)
		}
	}
}

func TestConcatRejectsEmpty(t *testing.T) {
	if _, err := (Concat{}).Recompose(context.Background(), nil); err == nil {
		t.Error("want error on empty input")
	}
}

func TestConcatRejectsNoOutcomes(t *testing.T) {
	chain := []session.Envelope{{}, {}}
	if _, err := (Concat{}).Recompose(context.Background(), chain); err == nil {
		t.Error("want error when no envelope carries an outcome")
	}
}

func TestConcatInheritsClassificationFromFirst(t *testing.T) {
	a := env("a", 0.5)
	a.Classification = classification.Confidential
	b := env("b", 0.5)
	b.Classification = classification.Public
	got, _ := Concat{}.Recompose(context.Background(), []session.Envelope{a, b})
	if got.Classification != classification.Confidential {
		t.Errorf("Classification = %v, want Confidential (from first)", got.Classification)
	}
}
