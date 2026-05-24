package mmlu_pro

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureJSON = `[
  {
    "question_id": 70,
    "question": "Find the degree for the given field extension Q(sqrt(2), sqrt(3), sqrt(18)) over Q.",
    "options": ["0", "4", "2", "6", "8", "1", "5", "3", "10", "7"],
    "answer": "B",
    "answer_index": 1,
    "category": "math",
    "src": "ori_mmlu-abstract_algebra"
  },
  {
    "question_id": 12,
    "question": "Which is the largest known star?",
    "options": ["UY Scuti", "Stephenson 2-18", "VY Canis Majoris", "Betelgeuse"],
    "answer": "B",
    "answer_index": 1,
    "category": "physics"
  },
  {
    "question_id": 5,
    "question": "Seven-option question?",
    "options": ["a", "b", "c", "d", "e", "f", "g"],
    "answer": "G",
    "answer_index": 6,
    "category": "other"
  }
]`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mmlu_pro_test.json")
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
	_, err := Loader{Path: "/nonexistent/mmlu_pro.json"}.Load(context.Background())
	if err == nil {
		t.Fatal("expected error on missing file")
	}
}

func TestLoader_HappyPath_10Options(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, err := Loader{Path: path}.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("got %d cases, want 3", len(cases))
	}
	// Cases must be sorted by question_id: 5, 12, 70.
	wantIDs := []string{"mmlu-pro-5", "mmlu-pro-12", "mmlu-pro-70"}
	for i, want := range wantIDs {
		if cases[i].ID != want {
			t.Errorf("cases[%d].ID = %q, want %q", i, cases[i].ID, want)
		}
	}

	// The 70 case has 10 options and expected answer "B".
	c := cases[2]
	if c.Expected != "B" {
		t.Errorf("Expected = %q, want B", c.Expected)
	}
	for _, opt := range []string{"0", "4", "2", "6", "8", "1", "5", "3", "10", "7"} {
		if !strings.Contains(c.Prompt, opt) {
			t.Errorf("Prompt missing option text %q", opt)
		}
	}
	// All 10 letter labels should appear (A through J).
	for _, l := range []string{"A)", "B)", "C)", "D)", "E)", "F)", "G)", "H)", "I)", "J)"} {
		if !strings.Contains(c.Prompt, l) {
			t.Errorf("Prompt missing letter label %q", l)
		}
	}
	if !strings.Contains(c.Prompt, "A through J") {
		t.Errorf("Prompt should include closing-position imperative referencing the J-cap; got %q", c.Prompt)
	}
}

func TestLoader_FourOptions_TruncatesLetterRange(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, _ := Loader{Path: path}.Load(context.Background())
	// Case 12 has 4 options.
	c := cases[1]
	if c.Expected != "B" {
		t.Errorf("Expected = %q, want B", c.Expected)
	}
	if !strings.Contains(c.Prompt, "A through D") {
		t.Errorf("4-option case should close at D; got %q", c.Prompt)
	}
	// No E onward.
	if strings.Contains(c.Prompt, "E)") {
		t.Errorf("4-option prompt should not have E)")
	}
}

func TestLoader_SevenOptions(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, _ := Loader{Path: path}.Load(context.Background())
	c := cases[0] // question_id 5
	if c.Expected != "G" {
		t.Errorf("Expected = %q, want G", c.Expected)
	}
	if !strings.Contains(c.Prompt, "A through G") {
		t.Errorf("7-option case should close at G; got %q", c.Prompt)
	}
}

func TestLoader_MetadataPopulated(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, _ := Loader{Path: path}.Load(context.Background())
	c := cases[2] // question_id 70
	if c.Metadata["category"] != "math" {
		t.Errorf("category = %q, want math", c.Metadata["category"])
	}
	if c.Metadata["src"] != "ori_mmlu-abstract_algebra" {
		t.Errorf("src = %q, want ori_mmlu-abstract_algebra", c.Metadata["src"])
	}
	if c.Metadata["option_count"] != "10" {
		t.Errorf("option_count = %q, want 10", c.Metadata["option_count"])
	}
}

func TestLoader_Limit(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, err := Loader{Path: path, Limit: 2}.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cases) != 2 {
		t.Errorf("got %d cases, want 2 (Limit honored)", len(cases))
	}
}

func TestLoader_NameStr(t *testing.T) {
	if got := (Loader{}).Name(); got != "mmlu-pro" {
		t.Errorf("default Name = %q, want mmlu-pro", got)
	}
	if got := (Loader{NameStr: "custom"}).Name(); got != "custom" {
		t.Errorf("override Name = %q, want custom", got)
	}
}

func TestLoader_RejectsBadData(t *testing.T) {
	bads := map[string]string{
		"missing question_id": `[{"question": "q", "options": ["a", "b"], "answer": "A"}]`,
		"missing question":    `[{"question_id": 1, "options": ["a", "b"], "answer": "A"}]`,
		"too few options":     `[{"question_id": 1, "question": "q", "options": ["a"], "answer": "A"}]`,
		"too many options":    `[{"question_id": 1, "question": "q", "options": ["1","2","3","4","5","6","7","8","9","0","X"], "answer": "A"}]`,
		"multi-char answer":   `[{"question_id": 1, "question": "q", "options": ["a","b"], "answer": "AB"}]`,
		"answer out of range": `[{"question_id": 1, "question": "q", "options": ["a","b"], "answer": "Z"}]`,
		"empty json":          `[]`,
	}
	for label, content := range bads {
		path := writeFixture(t, content)
		_, err := Loader{Path: path}.Load(context.Background())
		if err == nil {
			t.Errorf("%s: expected error, got nil", label)
		}
	}
}

func TestLoader_DeterministicOrder(t *testing.T) {
	// Same input twice should produce identical Case IDs in the same order.
	path := writeFixture(t, fixtureJSON)
	cases1, _ := Loader{Path: path}.Load(context.Background())
	cases2, _ := Loader{Path: path}.Load(context.Background())
	if len(cases1) != len(cases2) {
		t.Fatalf("len mismatch")
	}
	for i := range cases1 {
		if cases1[i].ID != cases2[i].ID {
			t.Errorf("order differs at [%d]: %q vs %q", i, cases1[i].ID, cases2[i].ID)
		}
		if cases1[i].Prompt != cases2[i].Prompt {
			t.Errorf("prompt differs at [%d]", i)
		}
	}
}
