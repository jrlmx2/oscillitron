// CLAUDE GENERATED
package minimal

import (
	"strings"
	"testing"
)

func TestProcessInstructions_MentionsLetters(t *testing.T) {
	// Smoke test: the minimal template must steer the model toward a
	// single closing letter. Without that, last-match-wins extraction
	// has nothing to match.
	want := []string{"single letter", "A, B, C, or D", "end your response"}
	got := strings.ToLower(ProcessInstructions)
	for _, w := range want {
		if !strings.Contains(got, strings.ToLower(w)) {
			t.Errorf("ProcessInstructions missing %q; got:\n%s", w, ProcessInstructions)
		}
	}
}

func TestProcessInstructions_RequiresConfidence(t *testing.T) {
	// The v3 inadequacy framework rests on confidence as the primary
	// notice signal. Even in minimal mode we must ask for it; only
	// the JSON envelope around it goes away.
	want := []string{"confidence", "0.0 to 1.0"}
	got := strings.ToLower(ProcessInstructions)
	for _, w := range want {
		if !strings.Contains(got, strings.ToLower(w)) {
			t.Errorf("ProcessInstructions missing %q (confidence is load-bearing); got:\n%s", w, ProcessInstructions)
		}
	}
}

func TestProcessInstructions_IsActuallyMinimal(t *testing.T) {
	// Pin the contract that minimal is much smaller than the default
	// envelope (~970 chars). If this fails because we made the
	// template longer, reconsider whether the additions are pulling
	// weight — every extra sentence is context tax on the small
	// model. Confidence raised the floor a bit; 600 chars is still
	// well under the ~970-char default.
	maxChars := 600
	if got := len(ProcessInstructions); got > maxChars {
		t.Errorf("ProcessInstructions is %d chars; should stay under %d (defeats the purpose otherwise)", got, maxChars)
	}
}

func TestProcessInstructions_NoJSONEnvelope(t *testing.T) {
	// The whole point of this package is to drop the JSON envelope.
	// Belt and suspenders: assert no JSON shape keywords leak in.
	banned := []string{
		"JSON",
		"json object",
		`"content"`,
		`"confidence"`,
		`"grounded_pass"`,
	}
	for _, b := range banned {
		if strings.Contains(ProcessInstructions, b) {
			t.Errorf("ProcessInstructions contains %q; minimal should not mention JSON shape", b)
		}
	}
}
