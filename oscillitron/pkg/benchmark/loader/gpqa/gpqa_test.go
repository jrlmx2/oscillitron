package gpqa

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureJSON = `[
  {
    "id": "gpqa-test-001",
    "question": "What is the chemical symbol for water?",
    "correct_answer": "H2O",
    "incorrect_answers": ["CO2", "NaCl", "O2"],
    "subdomain": "chemistry"
  },
  {
    "id": "gpqa-test-002",
    "question": "What is the speed of light in vacuum (m/s)?",
    "correct_answer": "299792458",
    "incorrect_answers": ["300000", "186000", "1000000"]
  }
]`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gpqa_test.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestLoader_RequiresPath(t *testing.T) {
	_, err := Loader{}.Load(context.Background())
	if err == nil {
		t.Fatal("expected error with empty Path")
	}
}

func TestLoader_FileNotFound(t *testing.T) {
	_, err := Loader{Path: "/nonexistent/gpqa.json"}.Load(context.Background())
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}

func TestLoader_HappyPath(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, err := Loader{Path: path}.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	c := cases[0]
	if c.ID != "gpqa-test-001" {
		t.Errorf("ID = %q", c.ID)
	}
	if c.Expected == "" || !strings.ContainsAny(c.Expected, "ABCD") {
		t.Errorf("Expected = %q, want A/B/C/D", c.Expected)
	}
	if !strings.Contains(c.Prompt, "What is the chemical symbol for water?") {
		t.Errorf("Prompt should include the question; got %q", c.Prompt)
	}
	if !strings.Contains(c.Prompt, "H2O") {
		t.Errorf("Prompt should include the correct answer text; got %q", c.Prompt)
	}
	// All 4 options should appear in the prompt.
	for _, opt := range []string{"H2O", "CO2", "NaCl", "O2"} {
		if !strings.Contains(c.Prompt, opt) {
			t.Errorf("Prompt missing option %q", opt)
		}
	}
	// Subdomain metadata propagates when present.
	if c.Metadata["subdomain"] != "chemistry" {
		t.Errorf("subdomain metadata = %q", c.Metadata["subdomain"])
	}
}

func TestLoader_DeterministicPlacement(t *testing.T) {
	// Same ID → same Expected letter across loads.
	path := writeFixture(t, fixtureJSON)
	cases1, _ := Loader{Path: path}.Load(context.Background())
	cases2, _ := Loader{Path: path}.Load(context.Background())
	for i := range cases1 {
		if cases1[i].Expected != cases2[i].Expected {
			t.Errorf("case %d: Expected non-deterministic: %q vs %q",
				i, cases1[i].Expected, cases2[i].Expected)
		}
		if cases1[i].Prompt != cases2[i].Prompt {
			t.Errorf("case %d: Prompt non-deterministic", i)
		}
	}
}

func TestLoader_CorrectAnswerActuallyAtExpectedLetter(t *testing.T) {
	// Spot-check that the prompt actually places the correct answer
	// at the letter Expected says.
	path := writeFixture(t, fixtureJSON)
	cases, _ := Loader{Path: path}.Load(context.Background())
	for _, c := range cases {
		// Find "{Expected}) " in the prompt and verify the next non-
		// whitespace text matches the correct answer.
		marker := c.Expected + ") "
		idx := strings.Index(c.Prompt, marker)
		if idx < 0 {
			t.Errorf("case %s: expected marker %q not in prompt", c.ID, marker)
			continue
		}
		after := c.Prompt[idx+len(marker):]
		// Lines end with \n; take up to that.
		if newline := strings.Index(after, "\n"); newline >= 0 {
			after = after[:newline]
		}
		// The correct answer for c00 is H2O, for c01 is 299792458.
		var wantText string
		switch c.ID {
		case "gpqa-test-001":
			wantText = "H2O"
		case "gpqa-test-002":
			wantText = "299792458"
		}
		if strings.TrimSpace(after) != wantText {
			t.Errorf("case %s: letter %s holds %q, want correct answer %q",
				c.ID, c.Expected, after, wantText)
		}
	}
}

func TestLoader_Limit(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, _ := Loader{Path: path, Limit: 1}.Load(context.Background())
	if len(cases) != 1 {
		t.Errorf("Limit=1 returned %d cases, want 1", len(cases))
	}
}

func TestLoader_SortedByID(t *testing.T) {
	// Out-of-order JSON should still produce sorted output.
	raw := `[
		{"id": "z", "question": "q", "correct_answer": "a", "incorrect_answers": ["b","c","d"]},
		{"id": "a", "question": "q", "correct_answer": "a", "incorrect_answers": ["b","c","d"]},
		{"id": "m", "question": "q", "correct_answer": "a", "incorrect_answers": ["b","c","d"]}
	]`
	path := writeFixture(t, raw)
	cases, _ := Loader{Path: path}.Load(context.Background())
	if cases[0].ID != "a" || cases[1].ID != "m" || cases[2].ID != "z" {
		t.Errorf("not sorted: %v", []string{cases[0].ID, cases[1].ID, cases[2].ID})
	}
}

func TestLoader_RejectsBadShape(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"missing id", `[{"question": "q", "correct_answer": "a", "incorrect_answers": ["b","c","d"]}]`},
		{"missing question", `[{"id": "x", "correct_answer": "a", "incorrect_answers": ["b","c","d"]}]`},
		{"missing correct", `[{"id": "x", "question": "q", "incorrect_answers": ["b","c","d"]}]`},
		{"no incorrect answers", `[{"id": "x", "question": "q", "correct_answer": "a", "incorrect_answers": []}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeFixture(t, c.raw)
			_, err := Loader{Path: path}.Load(context.Background())
			if err == nil {
				t.Error("expected error on malformed input")
			}
		})
	}
}

func TestLoader_EmptyCorpus(t *testing.T) {
	path := writeFixture(t, `[]`)
	_, err := Loader{Path: path}.Load(context.Background())
	if err == nil {
		t.Fatal("expected error on empty array")
	}
}

func TestLoader_Name(t *testing.T) {
	if (Loader{}).Name() != "gpqa-diamond" {
		t.Errorf("default name wrong")
	}
	if (Loader{NameStr: "custom"}).Name() != "custom" {
		t.Errorf("override not honored")
	}
}
