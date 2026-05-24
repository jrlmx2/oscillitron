package notice

import (
	"strings"
	"testing"
)

// helper: a string of N bytes for token math
func filler(n int) string {
	return strings.Repeat("x", n)
}

func TestInspect_CleanCall_NoDetections(t *testing.T) {
	a := Inspect(CallInspection{
		SystemPrompt: "You are helpful.",
		UserContent:  "What is 2 + 2?",
		ContextSize:  8192,
	})
	if len(a.Detections) != 0 {
		t.Errorf("expected 0 detections; got %d: %+v", len(a.Detections), a.Detections)
	}
	if a.Score != 0.0 {
		t.Errorf("expected Score 0.0 for clean call; got %v", a.Score)
	}
}

func TestInspect_InputOverflowsContext(t *testing.T) {
	// 4 bytes per "token" → 40000 bytes = 10000 tokens, fills an
	// 8000-token context.
	a := Inspect(CallInspection{
		SystemPrompt: filler(40000),
		UserContent:  "Q?",
		ContextSize:  8000,
	})
	if !hasSignal(a, InputOverflowsContext) {
		t.Fatalf("expected InputOverflowsContext; got %+v", a.Detections)
	}
	d := findSignal(a, InputOverflowsContext)
	if d.Severity != Error {
		t.Errorf("Severity = %q, want Error", d.Severity)
	}
	if a.Score != 1.0 {
		t.Errorf("Score = %v, want 1.0 (Error caps at 1.0)", a.Score)
	}
}

func TestInspect_InputApproachingContext(t *testing.T) {
	// ~5500 tokens into an 8000-token window = 69%, below 80% — no
	// fire. Then 7000 tokens = 87%, fires.
	below := Inspect(CallInspection{
		SystemPrompt: filler(22000), // ~5500 tok
		UserContent:  filler(40),    // ~10 tok
		ContextSize:  8000,
	})
	if hasSignal(below, InputApproachingContext) {
		t.Errorf("69%% should not trigger InputApproachingContext")
	}

	above := Inspect(CallInspection{
		SystemPrompt: filler(28000), // ~7000 tok
		UserContent:  filler(40),    // ~10 tok
		ContextSize:  8000,
	})
	if !hasSignal(above, InputApproachingContext) {
		t.Errorf("87%% should trigger InputApproachingContext; got %+v", above.Detections)
	}
	d := findSignal(above, InputApproachingContext)
	if d.Severity != Warning {
		t.Errorf("Severity = %q, want Warning", d.Severity)
	}
}

func TestInspect_PersonaHeavy_Warning(t *testing.T) {
	// sysTok / usrTok = 4× → above default ratio 3.0; below
	// critical 10.0 → Warning.
	a := Inspect(CallInspection{
		SystemPrompt: filler(2000), // 500 tok
		UserContent:  filler(500),  // 125 tok
		// ContextSize unset — only persona detector runs.
	})
	if !hasSignal(a, PersonaHeavy) {
		t.Fatalf("expected PersonaHeavy; got %+v", a.Detections)
	}
	d := findSignal(a, PersonaHeavy)
	if d.Severity != Warning {
		t.Errorf("Severity = %q, want Warning for 4× ratio", d.Severity)
	}
	if d.Metric < 3.5 || d.Metric > 4.5 {
		t.Errorf("Metric ratio = %v, want ~4.0", d.Metric)
	}
}

func TestInspect_PersonaHeavy_HermesScenario_Error(t *testing.T) {
	// The Hermes-overload story: ~4097 token system prompt, ~122
	// token user content. Ratio = 33×. Above PersonaCriticalRatio
	// (10×) → Error.
	a := Inspect(CallInspection{
		SystemPrompt: filler(4097 * 4),
		UserContent:  filler(122 * 4),
	})
	d := findSignal(a, PersonaHeavy)
	if d == nil {
		t.Fatalf("expected PersonaHeavy on Hermes-overload scenario; got %+v", a.Detections)
	}
	if d.Severity != Error {
		t.Errorf("Severity = %q, want Error for 33× ratio (the Hermes story)", d.Severity)
	}
	if a.Score != 1.0 {
		t.Errorf("Score = %v, want 1.0 (Error caps)", a.Score)
	}
}

func TestInspect_PersonaHeavy_SkippedWhenUserContentTrivial(t *testing.T) {
	// Tiny user content (<= 8 tokens) → don't fire persona-heavy
	// even with a giant system prompt. Otherwise every "yes/no?"
	// or "A" call would trip it.
	a := Inspect(CallInspection{
		SystemPrompt: filler(4000),
		UserContent:  "A",
	})
	if hasSignal(a, PersonaHeavy) {
		t.Errorf("PersonaHeavy should not fire when user content is trivial")
	}
}

func TestInspect_ContextSizeZero_SkipsContextDetectors(t *testing.T) {
	// When ContextSize is unset (zero), we can't check the input
	// against the window. Persona check still runs.
	a := Inspect(CallInspection{
		SystemPrompt: filler(4000),
		UserContent:  filler(500),
		ContextSize:  0,
	})
	if hasSignal(a, InputOverflowsContext) {
		t.Errorf("InputOverflowsContext should not fire with ContextSize=0")
	}
	if hasSignal(a, InputApproachingContext) {
		t.Errorf("InputApproachingContext should not fire with ContextSize=0")
	}
	if !hasSignal(a, PersonaHeavy) {
		t.Errorf("PersonaHeavy should still fire (independent of context size)")
	}
}

func TestInspect_CustomTokenizer(t *testing.T) {
	// Demonstrate operator override: count words * 1.3 (closer to
	// real tokenizers for English).
	called := 0
	custom := func(s string) int {
		called++
		return len(strings.Fields(s))
	}
	a := Inspect(CallInspection{
		SystemPrompt: "one two three four five six seven eight nine ten",
		UserContent:  "what",
		ContextSize:  20,
		ApproxTokens: custom,
	})
	if called == 0 {
		t.Errorf("custom tokenizer was not called")
	}
	// 10 system + 1 user = 11 tokens into a 20-token window = 55%
	// → no fire.
	if hasSignal(a, InputApproachingContext) || hasSignal(a, InputOverflowsContext) {
		t.Errorf("55%% should not trip context detectors")
	}
}

func TestComputeScore_MultipleWarnings(t *testing.T) {
	d := []Detection{
		{Severity: Warning},
		{Severity: Warning},
	}
	s := computeScore(d)
	if s < 0.59 || s > 0.61 {
		t.Errorf("two warnings score = %v, want ~0.6", s)
	}
}

func TestComputeScore_WarningCappedBelowError(t *testing.T) {
	// Many warnings cap at 0.9 so Error (1.0) can still dominate.
	d := []Detection{
		{Severity: Warning}, {Severity: Warning}, {Severity: Warning},
		{Severity: Warning}, {Severity: Warning},
	}
	s := computeScore(d)
	if s != 0.9 {
		t.Errorf("five warnings score = %v, want capped at 0.9", s)
	}
}

func TestDefaultApproxTokens(t *testing.T) {
	cases := map[string]int{
		"":         0, // 0 bytes → 0 tokens
		"abcd":     1, // 4 bytes → 1 token
		"abcdefgh": 2, // 8 bytes → 2 tokens
		"a":        1, // 1 byte → 1 token (rounds up via +3)
	}
	for s, want := range cases {
		if got := DefaultApproxTokens(s); got != want {
			t.Errorf("DefaultApproxTokens(%q) = %d, want %d", s, got, want)
		}
	}
}

// helpers

func hasSignal(a Assessment, s Signal) bool {
	return findSignal(a, s) != nil
}

func findSignal(a Assessment, s Signal) *Detection {
	for i := range a.Detections {
		if a.Detections[i].Signal == s {
			return &a.Detections[i]
		}
	}
	return nil
}
