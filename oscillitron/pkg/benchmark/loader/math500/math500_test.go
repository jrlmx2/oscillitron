// CLAUDE GENERATED
package math500

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureJSON = `[
  {
    "problem": "Let f(x) = 3x + 2. Find f(f(f(1))).",
    "answer": "53",
    "subject": "Algebra",
    "level": 1,
    "unique_id": "test/algebra/24.json"
  },
  {
    "problem": "What is the area of a circle with radius 3?",
    "answer": "9\\pi",
    "subject": "Geometry",
    "level": 2,
    "unique_id": "test/geometry/7.json"
  },
  {
    "problem": "Solve for x: 2x + 5 = 11.",
    "answer": "3",
    "subject": "Prealgebra",
    "level": 1,
    "unique_id": "test/prealgebra/100.json"
  }
]`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "math500_test.json")
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
	_, err := Loader{Path: "/nonexistent/math500.json"}.Load(context.Background())
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
	if len(cases) != 3 {
		t.Fatalf("got %d cases, want 3", len(cases))
	}
	// Cases must be sorted by unique_id (lexicographic):
	// test/algebra/24.json < test/geometry/7.json < test/prealgebra/100.json
	wantIDs := []string{
		"math500-algebra-24",
		"math500-geometry-7",
		"math500-prealgebra-100",
	}
	for i, want := range wantIDs {
		if cases[i].ID != want {
			t.Errorf("cases[%d].ID = %q, want %q", i, cases[i].ID, want)
		}
	}

	// Each case carries the answer in Expected.
	if cases[0].Expected != "53" {
		t.Errorf("Expected = %q, want 53", cases[0].Expected)
	}
	if cases[1].Expected != `9\pi` {
		t.Errorf("Expected = %q, want 9\\pi", cases[1].Expected)
	}

	// Prompt includes the problem text and the \boxed{} imperative.
	c := cases[0]
	if !strings.Contains(c.Prompt, "Let f(x) = 3x + 2") {
		t.Errorf("Prompt should include the problem; got %q", c.Prompt)
	}
	if !strings.Contains(c.Prompt, `\boxed{`) {
		t.Errorf("Prompt should mention \\boxed{}; got %q", c.Prompt)
	}

	// Prompt must NOT leak the answer or any solution text.
	if strings.Contains(c.Prompt, "53") {
		t.Errorf("Prompt leaks the answer text; got %q", c.Prompt)
	}
}

func TestLoader_MetadataPopulated(t *testing.T) {
	path := writeFixture(t, fixtureJSON)
	cases, _ := Loader{Path: path}.Load(context.Background())
	c := cases[0]
	if c.Metadata["subject"] != "Algebra" {
		t.Errorf("subject = %q, want Algebra", c.Metadata["subject"])
	}
	if c.Metadata["level"] != "1" {
		t.Errorf("level = %q, want 1", c.Metadata["level"])
	}
	if c.Metadata["unique_id"] != "test/algebra/24.json" {
		t.Errorf("unique_id = %q", c.Metadata["unique_id"])
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
	if got := (Loader{}).Name(); got != "math-500" {
		t.Errorf("default Name = %q, want math-500", got)
	}
	if got := (Loader{NameStr: "custom"}).Name(); got != "custom" {
		t.Errorf("override Name = %q, want custom", got)
	}
}

func TestLoader_RejectsBadData(t *testing.T) {
	bads := map[string]string{
		"missing unique_id": `[{"problem": "p", "answer": "1"}]`,
		"missing problem":   `[{"problem": "", "answer": "1", "unique_id": "test/x/1.json"}]`,
		"missing answer":    `[{"problem": "p", "answer": "", "unique_id": "test/x/1.json"}]`,
		"empty json":        `[]`,
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

func TestIDFromUniqueID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"test/algebra/24.json", "math500-algebra-24"},
		{"test/precalculus/9.json", "math500-precalculus-9"},
		{"weird-format", "math500-weird-format"},
		{"only/two.json", "math500-only/two"},
	}
	for _, tc := range cases {
		if got := idFromUniqueID(tc.in); got != tc.want {
			t.Errorf("idFromUniqueID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
