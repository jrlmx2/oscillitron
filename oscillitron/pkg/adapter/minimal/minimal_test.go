package minimal

import (
	"strings"
	"testing"
)

func TestProcessInstructions_TaskAgnostic(t *testing.T) {
	// The minimal template must NOT contain MCQ-specific framing.
	// Task specifics belong in the case prompt (built by the
	// per-benchmark loader), never in the universal instructions.
	got := strings.ToLower(ProcessInstructions)
	mcqForbidden := []string{
		"single letter",
		"a, b, c, or d",
		"multiple-choice",
		"multiple choice",
		"final character",
	}
	for _, w := range mcqForbidden {
		if strings.Contains(got, w) {
			t.Errorf("ProcessInstructions contains MCQ-specific phrase %q; the universal template should be task-agnostic — task specifics belong in the loader's case prompt", w)
		}
	}
}

func TestProcessInstructions_HasResponseTags(t *testing.T) {
	// The XML-tagged format is the canonical wire shape across every
	// substrate. Both tags must appear in the instruction text so the
	// model gets the explicit format.
	for _, tag := range []string{"<response>", "</response>", "<confidence>", "</confidence>"} {
		if !strings.Contains(ProcessInstructions, tag) {
			t.Errorf("ProcessInstructions missing required tag %q; current text:\n%s", tag, ProcessInstructions)
		}
	}
}

func TestProcessInstructions_IsActuallyMinimal(t *testing.T) {
	// Pin the contract that minimal stays much smaller than the
	// default envelope. If this fails because we made the template
	// longer, reconsider whether the additions are pulling weight —
	// every extra sentence is context tax on small models.
	maxChars := 400
	if got := len(ProcessInstructions); got > maxChars {
		t.Errorf("ProcessInstructions is %d chars; should stay under %d (defeats the purpose otherwise)", got, maxChars)
	}
}

func TestProcessInstructions_NoLegacyEnvelope(t *testing.T) {
	// Belt-and-suspenders: assert no legacy JSON-envelope keywords
	// leak in. The old envelope had content/grounded_pass/etc.
	banned := []string{
		"JSON",
		"json object",
		`"content"`,
		`"grounded_pass"`,
	}
	for _, b := range banned {
		if strings.Contains(ProcessInstructions, b) {
			t.Errorf("ProcessInstructions contains %q; minimal should not mention legacy envelope or JSON shape", b)
		}
	}
}

// --- parse.go tests ---

func TestExtractResponseTag(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantVal   string
		wantFound bool
	}{
		{"basic", "<response>A</response>", "A", true},
		{"with whitespace", "<response>  hello  </response>", "hello", true},
		{"multiline content", "<response>\nThis is\nmulti-line\n</response>", "This is\nmulti-line", true},
		{"latex inside", `<response>\boxed{53}</response>`, `\boxed{53}`, true},
		{"last match wins", "<response>first</response> then <response>final</response>", "final", true},
		{"no tag", "just some text with letter A", "", false},
		{"empty input", "", "", false},
		{"prose-then-tag", "Let me think... <response>C</response>", "C", true},
		{"unclosed tag returns no match", "<response>oops", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := ExtractResponseTag(tc.raw)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if got != tc.wantVal {
				t.Errorf("got = %q, want %q", got, tc.wantVal)
			}
		})
	}
}

func TestExtractConfidenceTag(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantVal   float64
		wantFound bool
	}{
		{"basic decimal", "<confidence>0.85</confidence>", 0.85, true},
		{"with whitespace", "<confidence>  0.7  </confidence>", 0.7, true},
		{"integer", "<confidence>1</confidence>", 1.0, true},
		{"zero", "<confidence>0</confidence>", 0.0, true},
		{"last match wins", "<confidence>0.3</confidence> then <confidence>0.9</confidence>", 0.9, true},
		{"no tag", "no confidence reported", 0, false},
		{"empty input", "", 0, false},
		{"placeholder unfilled", "<confidence>0.0 to 1.0</confidence>", 0, false},
		{"non-numeric", "<confidence>high</confidence>", 0, false},
		{"unclosed tag", "<confidence>0.8", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := ExtractConfidenceTag(tc.raw)
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v", found, tc.wantFound)
			}
			if got != tc.wantVal {
				t.Errorf("got = %v, want %v", got, tc.wantVal)
			}
		})
	}
}
