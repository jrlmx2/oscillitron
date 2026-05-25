package recomposer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func newChild(content string, conf float64) session.ReturnResultPayload {
	return session.ReturnResultPayload{
		Result:     session.Payload{Kind: "result", Content: content},
		Confidence: conf,
	}
}

// longContent pads s past SelectionThreshold so the Synth fold path
// is exercised instead of the short-answer selection path.
func longContent(s string) string {
	return s + strings.Repeat("_", SelectionThreshold+1)
}

func longChild(content string, conf float64) session.ReturnResultPayload {
	return newChild(longContent(content), conf)
}

func TestSynth_Sequential(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		longChild("a", 0.9),
		longChild("b", 0.7),
		longChild("c", 0.85),
	}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	// Sequential fold: reduce(reduce(a,b), c).
	// SynthStub joins with "+", applied to the long-padded content.
	wantContent := longContent("a") + "+" + longContent("b") + "+" + longContent("c")
	if got.Result.Content != wantContent {
		t.Errorf("content mismatch (length %d vs %d)", len(got.Result.Content), len(wantContent))
	}
	if got.Confidence != 0.7 {
		t.Errorf("confidence = %v, want 0.7", got.Confidence)
	}
	if stub.Calls() != 2 {
		t.Errorf("Calls() = %d, want 2", stub.Calls())
	}
}

func TestSynth_PairwiseOrdering(t *testing.T) {
	stub := NewSynthStub("synth").WithFormat(stepIndexFormat())
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		longChild("a", 0.9),
		longChild("b", 0.9),
		longChild("c", 0.9),
		longChild("d", 0.9),
	}
	got, err := r.Recompose(context.Background(), session.RecomposePairwise, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	la, lb, lc, ld := longContent("a"), longContent("b"), longContent("c"), longContent("d")
	want := "step2(step0(" + la + "," + lb + "),step1(" + lc + "," + ld + "))"
	if got.Result.Content != want {
		t.Errorf("content mismatch (length %d vs %d)", len(got.Result.Content), len(want))
	}
	if stub.Calls() != 3 {
		t.Errorf("Calls() = %d, want 3 (N-1)", stub.Calls())
	}
}

func TestSynth_PairwiseOddCountPassesTrailingThrough(t *testing.T) {
	stub := NewSynthStub("synth").WithFormat(stepIndexFormat())
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		longChild("a", 0.9), longChild("b", 0.9),
		longChild("c", 0.9), longChild("d", 0.9),
		longChild("e", 0.9),
	}
	got, err := r.Recompose(context.Background(), session.RecomposePairwise, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	la, lb, lc, ld, le := longContent("a"), longContent("b"), longContent("c"), longContent("d"), longContent("e")
	want := "step3(step2(step0(" + la + "," + lb + "),step1(" + lc + "," + ld + "))," + le + ")"
	if got.Result.Content != want {
		t.Errorf("content mismatch (length %d vs %d)", len(got.Result.Content), len(want))
	}
	if stub.Calls() != 4 {
		t.Errorf("Calls() = %d, want 4", stub.Calls())
	}
}

func TestSynth_ShortAnswerSelection(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		newChild("C", 1.0),
		newChild("A", 0.8),
		newChild("B", 0.5),
	}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if got.Result.Content != "C" {
		t.Errorf("content = %q, want C (highest confidence)", got.Result.Content)
	}
	if got.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0", got.Confidence)
	}
	if stub.Calls() != 0 {
		t.Errorf("synthesizer should not be called for short-answer selection; got %d", stub.Calls())
	}
}

func TestSynth_ShortAnswerSelection_MixedLengthUsesSynth(t *testing.T) {
	stub := NewSynthStub("synth")
	r := Synth{Synthesizer: stub}
	children := []session.ReturnResultPayload{
		newChild("A", 1.0),
		longChild("long explanation", 0.8),
	}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential, children)
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if stub.Calls() != 1 {
		t.Errorf("should use synthesis fold when not all children are short; got %d calls", stub.Calls())
	}
	_ = got
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
		[]session.ReturnResultPayload{longChild("a", 0.5), longChild("b", 0.5)})
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
	stub := NewSynthStub("synth").WithConfidence(0.95)
	r := Synth{Synthesizer: stub}
	got, err := r.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{longChild("a", 0.6), longChild("b", 0.4)})
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
			Result:  session.Payload{Kind: "result", Content: longContent("a")},
			Signals: session.Signals{GroundedPass: &tru, Contradictions: []string{"x"}},
		},
		{
			Result:  session.Payload{Kind: "result", Content: longContent("b")},
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
