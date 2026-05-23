// CLAUDE GENERATED
package grader

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

func TestExtractLetter_VariousShapes(t *testing.T) {
	cases := []struct {
		raw     string
		letters string
		want    string
	}{
		{"A", "ABCD", "A"},                // exact letter
		{"a", "ABCD", "A"},                // lowercase
		{"   B  ", "ABCD", "B"},           // whitespace
		{"The answer is A.", "ABCD", "A"}, // sentence
		{"(C)", "ABCD", "C"},              // parens
		{"After analysis, B is correct because... so D", "ABCD", "D"}, // last match wins
		{"Therefore the answer is **C**.", "ABCD", "C"},
		{"I think the answer is option B", "ABCD", "B"},
		{"The chemical reaction proceeds via mechanism A→B→C→D", "ABCD", "D"}, // last word-boundary
		{"42", "ABCD", ""},                     // no letter
		{"the cat sat", "ABCD", ""},            // letters inside words don't count (word-boundary)
		{"H is the answer", "ABCDEFGHIJ", "H"}, // extended letter set
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got := ExtractLetter(c.raw, c.letters)
			if got != c.want {
				t.Errorf("ExtractLetter(%q, %q) = %q, want %q", c.raw, c.letters, got, c.want)
			}
		})
	}
}

func TestMultichoice_PassesOnCorrectExtracted(t *testing.T) {
	g := Multichoice{}
	v, err := g.Grade(context.Background(),
		benchmark.Case{ID: "x", Expected: "C"},
		benchmark.Answer{Extracted: "C"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if !v.Pass || v.Score != 1.0 {
		t.Errorf("expected pass+1.0; got %+v", v)
	}
}

func TestMultichoice_FailsOnWrongExtracted(t *testing.T) {
	g := Multichoice{}
	v, _ := g.Grade(context.Background(),
		benchmark.Case{ID: "x", Expected: "A"},
		benchmark.Answer{Extracted: "B"})
	if v.Pass || v.Score != 0.0 {
		t.Errorf("expected fail+0.0; got %+v", v)
	}
	if v.Notes == "" {
		t.Errorf("expected notes on failure")
	}
}

func TestMultichoice_FallsBackToRawExtraction(t *testing.T) {
	g := Multichoice{}
	v, _ := g.Grade(context.Background(),
		benchmark.Case{ID: "x", Expected: "B"},
		benchmark.Answer{Raw: "I think the answer is B"})
	if !v.Pass {
		t.Errorf("expected pass via raw extraction; got %+v", v)
	}
}

func TestMultichoice_EmptyExtractedFails(t *testing.T) {
	g := Multichoice{}
	v, _ := g.Grade(context.Background(),
		benchmark.Case{ID: "x", Expected: "A"},
		benchmark.Answer{Raw: "no letter here"})
	if v.Pass {
		t.Errorf("expected fail when no letter can be extracted")
	}
	if v.Notes != "no letter extracted" {
		t.Errorf("Notes = %q, want 'no letter extracted'", v.Notes)
	}
}

func TestMultichoice_CaseInsensitive(t *testing.T) {
	g := Multichoice{}
	v, _ := g.Grade(context.Background(),
		benchmark.Case{ID: "x", Expected: "c"},
		benchmark.Answer{Extracted: "C"})
	if !v.Pass {
		t.Errorf("expected case-insensitive match; got %+v", v)
	}
}

func TestMultichoice_CustomLetters(t *testing.T) {
	g := Multichoice{Letters: "ABCDEFGHIJ"}
	v, _ := g.Grade(context.Background(),
		benchmark.Case{ID: "x", Expected: "H"},
		benchmark.Answer{Raw: "the correct option is H"})
	if !v.Pass {
		t.Errorf("expected pass on H; got %+v", v)
	}
}
