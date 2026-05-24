package recomposer

import (
	"context"
	"errors"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func newChild(content string, conf float64) session.ReturnResultPayload {
	return session.ReturnResultPayload{
		Result:     session.Payload{Kind: "result", Content: content},
		Confidence: conf,
	}
}

func TestSynth_Sequential(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		newChild("a", 0.9),
		newChild("b", 0.7),
		newChild("c", 0.85),
	}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	// Sequential fold: reduce(reduce(a,b), c) = "a+b+c".
	if got.Result.Content != "a+b+c" {
		t.Errorf("content = %q, want %q", got.Result.Content, "a+b+c")
	}
	// Weakest-link min confidence (0.7).
	if got.Confidence != 0.7 {
		t.Errorf("confidence = %v, want 0.7", got.Confidence)
	}
	// N-1 = 2 synthesizer calls.
	if stub.Calls() != 2 {
		t.Errorf("Calls() = %d, want 2", stub.Calls())
	}
}

func TestSynth_PairwiseOrdering(t *testing.T) {
	// Use step-index format so we can confirm pairwise structure:
	// 4 children → 3 reductions across 2 rounds. Round 1 reduces
	// (a,b) at step 0 and (c,d) at step 1; round 2 reduces those two
	// at step 2.
	stub := NewSynthStub("synth").WithFormat(stepIndexFormat())
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		newChild("a", 0.9),
		newChild("b", 0.9),
		newChild("c", 0.9),
		newChild("d", 0.9),
	}
	got, err := r.Recompose(context.Background(), session.RecomposePairwise, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	want := "step2(step0(a,b),step1(c,d))"
	if got.Result.Content != want {
		t.Errorf("content = %q, want %q", got.Result.Content, want)
	}
	if stub.Calls() != 3 {
		t.Errorf("Calls() = %d, want 3 (N-1)", stub.Calls())
	}
}

func TestSynth_PairwiseOddCountPassesTrailingThrough(t *testing.T) {
	// 5 children. Round 1 reduces (a,b)(c,d) → 2 results + e passes
	// through → 3 elements at round 2 start. Round 2 reduces the first
	// two and passes the third through → 2 elements. Round 3 reduces
	// those two. Total reductions: 2 + 1 + 1 = 4 = N - 1.
	stub := NewSynthStub("synth").WithFormat(stepIndexFormat())
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		newChild("a", 0.9), newChild("b", 0.9),
		newChild("c", 0.9), newChild("d", 0.9),
		newChild("e", 0.9),
	}
	got, err := r.Recompose(context.Background(), session.RecomposePairwise, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	want := "step3(step2(step0(a,b),step1(c,d)),e)"
	if got.Result.Content != want {
		t.Errorf("content = %q, want %q", got.Result.Content, want)
	}
	if stub.Calls() != 4 {
		t.Errorf("Calls() = %d, want 4", stub.Calls())
	}
}

func TestSynth_SingleChildPassthrough(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{newChild("only", 0.42)})
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if got.Result.Content != "only" {
		t.Errorf("content = %q, want passthrough %q", got.Result.Content, "only")
	}
	if stub.Calls() != 0 {
		t.Errorf("synthesizer should not be called for single-child input; got %d", stub.Calls())
	}
}

func TestSynth_NoneReturnsZeroPayload(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	got, err := r.Recompose(context.Background(), session.RecomposeNone,
		[]session.ReturnResultPayload{newChild("a", 0.5)})
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if got.Result.Content != "" || got.Confidence != 0 {
		t.Errorf("RecomposeNone should return zero payload; got %+v", got)
	}
	if stub.Calls() != 0 {
		t.Errorf("synthesizer should not be called on RecomposeNone")
	}
}

func TestSynth_EmptyChildrenError(t *testing.T) {
	r := Synth{Synthesizer: NewSynthStub("synth")}
	_, err := r.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{})
	if !errors.Is(err, ErrNoChildren) {
		t.Errorf("got %v, want ErrNoChildren", err)
	}
}

func TestSynth_MissingSynthesizerError(t *testing.T) {
	r := Synth{} // no synthesizer
	_, err := r.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{newChild("a", 0.5), newChild("b", 0.5)})
	if !errors.Is(err, ErrSynthesizerRequired) {
		t.Errorf("got %v, want ErrSynthesizerRequired", err)
	}
}

func TestSynth_UnknownSpecError(t *testing.T) {
	r := Synth{Synthesizer: NewSynthStub("synth")}
	_, err := r.Recompose(context.Background(), session.RecomposeSpec("bogus"),
		[]session.ReturnResultPayload{newChild("a", 0.5), newChild("b", 0.5)})
	if !errors.Is(err, ErrUnknownSpec) {
		t.Errorf("got %v, want ErrUnknownSpec", err)
	}
}

func TestSynth_SynthesizerErrorPropagates(t *testing.T) {
	boom := errors.New("frontier down")
	stub := NewSynthStub("synth").WithError(boom)
	r := Synth{Synthesizer: stub}
	_, err := r.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{newChild("a", 0.5), newChild("b", 0.5)})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want wrapped synthesizer error", err)
	}
}

func TestSynth_ContextCancellation(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Recompose(ctx, session.RecomposeSequential,
		[]session.ReturnResultPayload{newChild("a", 0.5), newChild("b", 0.5)})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestSynth_ConfidenceOverride(t *testing.T) {
	// Stub returns a fixed 0.95 — should override the weakest-link min.
	stub := NewSynthStub("synth").WithConfidence(0.95)
	r := Synth{Synthesizer: stub}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{newChild("a", 0.6), newChild("b", 0.4)})
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if got.Confidence != 0.95 {
		t.Errorf("confidence = %v, want 0.95 (override)", got.Confidence)
	}
}

func TestSynth_SignalsMergedAcrossSteps(t *testing.T) {
	tru := true
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		{
			Result:  session.Payload{Kind: "result", Content: "a"},
			Signals: session.Signals{GroundedPass: &tru, Contradictions: []string{"x"}},
		},
		{
			Result:  session.Payload{Kind: "result", Content: "b"},
			Signals: session.Signals{GroundedPass: &tru, OpenQuestions: []string{"q?"}},
		},
	}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if got.Signals.GroundedPass == nil || !*got.Signals.GroundedPass {
		t.Errorf("GroundedPass should be true (both inputs true): %+v", got.Signals.GroundedPass)
	}
	if len(got.Signals.Contradictions) != 1 || got.Signals.Contradictions[0] != "x" {
		t.Errorf("Contradictions = %v", got.Signals.Contradictions)
	}
	if len(got.Signals.OpenQuestions) != 1 || got.Signals.OpenQuestions[0] != "q?" {
		t.Errorf("OpenQuestions = %v", got.Signals.OpenQuestions)
	}
}
