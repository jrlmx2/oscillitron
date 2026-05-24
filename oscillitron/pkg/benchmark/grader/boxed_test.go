// CLAUDE GENERATED
package grader

import (
	"context"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

func TestExtractBoxed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"basic", `The answer is \boxed{42}.`, "42"},
		{"latex frac", `So we get \boxed{\frac{1}{2}}.`, `\frac{1}{2}`},
		{"with trailing punct", `\boxed{53}.`, "53"},
		{"multiple boxed, last wins", `First \boxed{wrong}, but actually \boxed{right}.`, "right"},
		{"no boxed", `The answer is 42.`, ""},
		{"empty input", ``, ""},
		{"nested braces", `\boxed{f(x) = \{1, 2, 3\}}`, `f(x) = \{1, 2, 3\}`},
		{"trim inner whitespace", `\boxed{   53   }`, "53"},
		{"mid-stream then final", `Considering \boxed{x=1}, we conclude with \boxed{x=2}`, "x=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractBoxed(tc.raw); got != tc.want {
				t.Errorf("ExtractBoxed(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBoxedAnswer_Grade_Pass(t *testing.T) {
	g := BoxedAnswer{}
	c := benchmark.Case{ID: "x", Expected: "53"}
	a := benchmark.Answer{Raw: `Working through it: f(1)=5, f(5)=17, f(17)=53. So \boxed{53}.`}
	v, err := g.Grade(context.Background(), c, a)
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if !v.Pass {
		t.Errorf("Pass = false, want true; notes=%q", v.Notes)
	}
	if v.Score != 1.0 {
		t.Errorf("Score = %v, want 1.0", v.Score)
	}
}

func TestBoxedAnswer_Grade_Fail_WrongAnswer(t *testing.T) {
	g := BoxedAnswer{}
	c := benchmark.Case{ID: "x", Expected: "53"}
	a := benchmark.Answer{Raw: `\boxed{54}`}
	v, _ := g.Grade(context.Background(), c, a)
	if v.Pass {
		t.Errorf("Pass = true, want false")
	}
	if v.Notes != "extracted=54 expected=53" {
		t.Errorf("Notes = %q", v.Notes)
	}
}

func TestBoxedAnswer_Grade_Fail_NoBoxed(t *testing.T) {
	g := BoxedAnswer{}
	c := benchmark.Case{ID: "x", Expected: "53"}
	a := benchmark.Answer{Raw: `The answer is 53.`}
	v, _ := g.Grade(context.Background(), c, a)
	if v.Pass {
		t.Errorf("Pass = true on missing-boxed (exact-match discipline)")
	}
	if v.Notes != `no \boxed{} expression found` {
		t.Errorf("Notes = %q", v.Notes)
	}
}

func TestBoxedAnswer_Grade_Normalization(t *testing.T) {
	g := BoxedAnswer{}
	// Trailing period, surrounding $, whitespace.
	c := benchmark.Case{ID: "x", Expected: " 53. "}
	a := benchmark.Answer{Raw: `\boxed{$53$}.`}
	v, _ := g.Grade(context.Background(), c, a)
	if !v.Pass {
		t.Errorf("Normalization should make $53$ == 53 == ' 53. '; got notes=%q", v.Notes)
	}
}

func TestBoxedAnswer_Grade_LatexEquivalence_NotHandled(t *testing.T) {
	// Documents the v0 limitation: \frac{1}{2} != 1/2 even though
	// they're mathematically the same. If this test starts failing,
	// the normalizer got smarter — update the test docstring and
	// shape accordingly.
	g := BoxedAnswer{}
	c := benchmark.Case{ID: "x", Expected: "1/2"}
	a := benchmark.Answer{Raw: `\boxed{\frac{1}{2}}`}
	v, _ := g.Grade(context.Background(), c, a)
	if v.Pass {
		t.Errorf("v0 normalizer should not equate \\frac{1}{2} and 1/2; got pass=true (good news but update test)")
	}
}

func TestBoxedAnswer_PrefersExtractedField(t *testing.T) {
	// If an orchestrator already extracted (e.g., vote majority on
	// boxed text), use that directly rather than re-extracting from
	// Raw.
	g := BoxedAnswer{}
	c := benchmark.Case{ID: "x", Expected: "53"}
	a := benchmark.Answer{
		Raw:       `garbage with \boxed{wrong} in it`,
		Extracted: "53", // orchestrator's call wins
	}
	v, _ := g.Grade(context.Background(), c, a)
	if !v.Pass {
		t.Errorf("Should pass on Extracted=53 even when Raw is garbage; notes=%q", v.Notes)
	}
}

func TestBoxedAnswer_NameStr(t *testing.T) {
	if got := (BoxedAnswer{}).Name(); got != "boxed" {
		t.Errorf("default Name = %q, want boxed", got)
	}
	if got := (BoxedAnswer{NameStr: "math-judge"}).Name(); got != "math-judge" {
		t.Errorf("override Name = %q, want math-judge", got)
	}
}
