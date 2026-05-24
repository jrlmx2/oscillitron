package grader

import (
	"context"
	"regexp"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// BoxedAnswer grades open-ended math benchmarks (MATH-500, AIME)
// by extracting the final \boxed{...} expression from the model's
// response and comparing against Case.Expected.
//
// Extraction is last-match-wins (matches the dispatcher pattern
// used everywhere else in this codebase): models often produce
// intermediate \boxed expressions while working out the problem
// before committing to the final answer.
//
// Normalization is intentionally light in v0:
//
//   - Whitespace trimmed from both sides
//   - Trailing period/comma stripped
//   - Surrounding $ or $$ stripped (some models wrap the expression
//     in display-math delimiters even inside the boxed argument)
//
// Latex equivalence ("\\frac{1}{2}" vs "1/2" vs "0.5") is NOT
// handled. Standard practice in math benchmarks is to use a
// dedicated normalizer (Hendrycks's math_equivalence.py is the
// canonical one). v0 ships exact-match-after-light-norm and
// surfaces the equivalence failures as an empirical signal worth
// measuring — the right next step is to plug in a real normalizer
// once we see how much it actually matters on the data.
type BoxedAnswer struct {
	// NameStr appears in benchmark reports. Defaults to "boxed".
	NameStr string
}

// boxedRE matches \boxed{...} with one level of brace nesting
// supported (sufficient for expressions like \boxed{\frac{1}{2}}).
// Go's RE2 doesn't support recursive matching, so deeper nesting
// would need a state-machine extractor — rare enough in MATH-500
// that we punt for v0.
var boxedRE = regexp.MustCompile(`\\boxed\{((?:[^{}]|\{[^{}]*\})*)\}`)

// Name implements benchmark.Grader.
func (b BoxedAnswer) Name() string {
	if b.NameStr != "" {
		return b.NameStr
	}
	return "boxed"
}

// Grade implements benchmark.Grader.
func (b BoxedAnswer) Grade(_ context.Context, c benchmark.Case, a benchmark.Answer) (benchmark.Verdict, error) {
	extracted := strings.TrimSpace(a.Extracted)
	if extracted == "" {
		extracted = ExtractBoxed(a.Raw)
	}
	expected := normalize(c.Expected)
	got := normalize(extracted)

	pass := got != "" && got == expected
	score := 0.0
	if pass {
		score = 1.0
	}
	notes := ""
	if !pass {
		if got == "" {
			notes = "no \\boxed{} expression found"
		} else {
			notes = "extracted=" + got + " expected=" + expected
		}
	}
	return benchmark.Verdict{
		GraderName: b.Name(),
		Pass:       pass,
		Score:      score,
		Notes:      notes,
	}, nil
}

// ExtractBoxed pulls the last \boxed{...} expression from raw text.
// Returns "" if none found. The returned string is the inner
// expression with surrounding whitespace trimmed but no further
// normalization.
func ExtractBoxed(raw string) string {
	matches := boxedRE.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return ""
	}
	return strings.TrimSpace(last[1])
}

// normalize applies the v0 light-normalization rules described in
// the package doc. Exported only as `normalize` (lowercase) — not
// part of the public surface yet; promoted only when consumers
// motivate it.
func normalize(s string) string {
	s = strings.TrimSpace(s)
	// Strip display-math delimiters.
	s = strings.TrimPrefix(s, "$$")
	s = strings.TrimSuffix(s, "$$")
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimSuffix(s, "$")
	s = strings.TrimSpace(s)
	// Trailing punctuation.
	s = strings.TrimRight(s, ".,;")
	s = strings.TrimSpace(s)
	return s
}

// Compile-time check.
var _ benchmark.Grader = BoxedAnswer{}
