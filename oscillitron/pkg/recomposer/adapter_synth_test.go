// CLAUDE GENERATED
package recomposer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestAdapterSynth_Synthesize_HappyPath(t *testing.T) {
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "merged answer"}, 0.85)
	s := AdapterSynth{Adapter: a}
	resp, err := s.Synthesize(context.Background(), SynthesizeRequest{
		Left:          session.ReturnResultPayload{Result: session.Payload{Content: "L"}},
		Right:         session.ReturnResultPayload{Result: session.Payload{Content: "R"}},
		RecomposeSpec: session.RecomposeSequential,
		StepIndex:     0,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if resp.Content != "merged answer" {
		t.Errorf("Content = %q, want merged answer", resp.Content)
	}
	if resp.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", resp.Confidence)
	}
}

func TestAdapterSynth_Synthesize_RequiresAdapter(t *testing.T) {
	s := AdapterSynth{}
	_, err := s.Synthesize(context.Background(), SynthesizeRequest{})
	if err == nil {
		t.Fatal("expected error with nil adapter")
	}
}

func TestAdapterSynth_Synthesize_AdapterErrorPropagates(t *testing.T) {
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithExecuteError(errors.New("substrate down"))
	s := AdapterSynth{Adapter: a}
	_, err := s.Synthesize(context.Background(), SynthesizeRequest{})
	if err == nil || !strings.Contains(err.Error(), "substrate down") {
		t.Errorf("expected wrapped substrate error, got %v", err)
	}
}

func TestAdapterSynth_Name(t *testing.T) {
	s := AdapterSynth{Adapter: stub.New("hermes-local")}
	if got := s.Name(); got != "adapter-synth(hermes-local)" {
		t.Errorf("Name() = %q", got)
	}
	if got := (AdapterSynth{}).Name(); got != "adapter-synth(<nil>)" {
		t.Errorf("nil-adapter Name() = %q", got)
	}
}

func TestAdapterSynth_PluggableIntoSynthRecomposer(t *testing.T) {
	// End-to-end: AdapterSynth plugged into Synth recomposer for a
	// sequential fold. The stub returns a fixed merged content;
	// per-step confidence drops in Synth via the weakest-link rule
	// when adapter returns 0.
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Kind: "result", Content: "M"}, 0.9)
	rec := Synth{Synthesizer: AdapterSynth{Adapter: a}}
	got, err := rec.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{
			{Result: session.Payload{Content: "A"}, Confidence: 0.7},
			{Result: session.Payload{Content: "B"}, Confidence: 0.6},
			{Result: session.Payload{Content: "C"}, Confidence: 0.95},
		})
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if got.Result.Content != "M" {
		t.Errorf("content = %q, want M", got.Result.Content)
	}
}
