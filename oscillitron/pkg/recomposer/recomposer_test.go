// CLAUDE GENERATED
package recomposer

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func childEnv(content string, conf float64, signals ...string) session.Envelope {
	return session.Envelope{
		Output: &session.Output{
			Content:    content,
			Confidence: conf,
			Signals:    signals,
		},
	}
}

func TestConcatJoinsChildren(t *testing.T) {
	parent := session.Output{Content: "parent framing", Confidence: 0.9}
	children := []session.Envelope{
		childEnv("a", 0.8),
		childEnv("b", 0.6),
		childEnv("c", 0.7),
	}
	got, err := Concat{Separator: " | "}.Recompose(context.Background(), parent, children)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "parent framing | a | b | c"
	if got.Content != want {
		t.Errorf("Content = %q, want %q", got.Content, want)
	}
	if got.Confidence != 0.6 {
		t.Errorf("Confidence = %v, want 0.6 (weakest link)", got.Confidence)
	}
	if got.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want Done", got.ExitReason)
	}
}

func TestConcatDedupesSignals(t *testing.T) {
	parent := session.Output{Signals: []string{"p1"}}
	children := []session.Envelope{
		childEnv("x", 0.5, "shared", "child-a"),
		childEnv("y", 0.5, "shared", "child-b"),
	}
	got, err := Concat{}.Recompose(context.Background(), parent, children)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Signals) != 4 {
		t.Errorf("Signals = %v, want 4 unique", got.Signals)
	}
	joined := strings.Join(got.Signals, ",")
	for _, s := range []string{"p1", "shared", "child-a", "child-b"} {
		if !strings.Contains(joined, s) {
			t.Errorf("missing %q in %v", s, got.Signals)
		}
	}
}

func TestConcatNoChildrenErrors(t *testing.T) {
	_, err := Concat{}.Recompose(context.Background(), session.Output{Content: "lonely"}, nil)
	if err == nil {
		t.Fatal("expected error with no children")
	}
}

func TestConcatSkipsChildWithoutOutput(t *testing.T) {
	parent := session.Output{Content: "p"}
	children := []session.Envelope{
		{}, // no Output
		childEnv("c", 0.5),
	}
	got, err := Concat{Separator: "/"}.Recompose(context.Background(), parent, children)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Content != "p/c" {
		t.Errorf("Content = %q, want p/c", got.Content)
	}
}
