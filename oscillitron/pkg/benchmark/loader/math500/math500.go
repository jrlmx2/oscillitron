// Package math500 loads MATH-500 cases from disk.
//
// Dataset: https://huggingface.co/datasets/HuggingFaceH4/MATH-500
//
// MATH-500 is a 500-problem subset of the MATH dataset spanning
// seven subjects (Algebra, Counting & Probability, Geometry,
// Intermediate Algebra, Number Theory, Prealgebra, Precalculus)
// and five difficulty levels (1 = easiest, 5 = hardest). Each
// problem expects an open-ended answer wrapped in \boxed{...}.
//
// Operator-downloaded JSON; not auto-fetched at runtime. Convert
// the HuggingFace JSONL to a JSON array via the operator script
// described in cmd/bench/cases/README.md.
//
// Expected JSON shape:
//
//	[
//	  {
//	    "problem": "Let f(x) = 3x + 2. Find f(f(f(1))).",
//	    "answer": "53",
//	    "subject": "Algebra",
//	    "level": 1,
//	    "unique_id": "test/algebra/24.json"
//	  },
//	  …
//	]
//
// Unlike GPQA / MMLU-Pro this is NOT multiple-choice. The grader
// pairs with pkg/benchmark/grader.BoxedAnswer, which extracts the
// final \boxed{...} expression from the model's response and
// compares against the case's Expected answer.
//
// Voting semantics on free-form answers are a known open question:
// exact-match aggregation collapses when 5 attempts produce
// different latex forms of the same answer (e.g., 1/2 vs \frac{1}{2}
// vs 0.5). The v0 path uses exact-match anyway — the failure mode
// is itself an empirical signal worth measuring. Answer-equivalence-
// aware voting is a follow-up.
package math500

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// rawCase mirrors the upstream JSON shape. The dataset's `solution`
// field is the long-form reasoning trace; we intentionally don't
// surface it in the prompt (the model should derive its own
// solution, not pattern-match on a provided one).
type rawCase struct {
	Problem  string `json:"problem"`
	Answer   string `json:"answer"`
	Subject  string `json:"subject,omitempty"`
	Level    int    `json:"level,omitempty"`
	UniqueID string `json:"unique_id"`
}

// Loader reads cases from a JSON file. Implements benchmark.Loader.
type Loader struct {
	// Path is the JSON file path. Required.
	Path string
	// NameStr appears in benchmark reports. Defaults to "math-500".
	NameStr string
	// Limit optionally caps the number of cases loaded (0 = all).
	Limit int
}

// Name implements benchmark.Loader.
func (l Loader) Name() string {
	if l.NameStr != "" {
		return l.NameStr
	}
	return "math-500"
}

// Load implements benchmark.Loader.
func (l Loader) Load(_ context.Context) ([]benchmark.Case, error) {
	if l.Path == "" {
		return nil, fmt.Errorf("math500: Path is required")
	}
	data, err := os.ReadFile(l.Path)
	if err != nil {
		return nil, fmt.Errorf("math500: read %s: %w", l.Path, err)
	}
	var raws []rawCase
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("math500: parse %s: %w", l.Path, err)
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("math500: %s contains zero cases", l.Path)
	}

	// Stable order: sort by unique_id so Load is deterministic
	// across runs.
	sort.Slice(raws, func(i, j int) bool { return raws[i].UniqueID < raws[j].UniqueID })

	cases := make([]benchmark.Case, 0, len(raws))
	for _, r := range raws {
		if l.Limit > 0 && len(cases) >= l.Limit {
			break
		}
		c, err := buildCase(r)
		if err != nil {
			return nil, fmt.Errorf("math500: case %q: %w", r.UniqueID, err)
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// buildCase converts one rawCase into a benchmark.Case.
//
// Prompt format intentionally references the boxed convention up
// front — models trained on math benchmarks expect this. The
// closing-position imperative still applies (terminate the
// response with the boxed answer) but is phrased to fit free-form
// math output rather than MCQ letters.
func buildCase(r rawCase) (benchmark.Case, error) {
	if r.UniqueID == "" {
		return benchmark.Case{}, fmt.Errorf("missing unique_id")
	}
	if r.Problem == "" {
		return benchmark.Case{}, fmt.Errorf("missing problem")
	}
	if r.Answer == "" {
		return benchmark.Case{}, fmt.Errorf("missing answer")
	}

	var b strings.Builder
	b.WriteString("Solve the following problem. Show your work step by step, ")
	b.WriteString("then state your final answer inside \\boxed{...} as the ")
	b.WriteString("last expression in your response.\n\n")
	b.WriteString("Problem: ")
	b.WriteString(r.Problem)

	md := map[string]string{}
	if r.Subject != "" {
		md["subject"] = r.Subject
	}
	if r.Level > 0 {
		md["level"] = strconv.Itoa(r.Level)
	}
	md["unique_id"] = r.UniqueID

	return benchmark.Case{
		ID:       idFromUniqueID(r.UniqueID),
		Prompt:   b.String(),
		Expected: r.Answer,
		Metadata: md,
	}, nil
}

// idFromUniqueID turns "test/algebra/24.json" into "math500-algebra-24"
// for a more readable Case.ID. Falls back to the full unique_id if
// the format isn't the expected three-segment path.
func idFromUniqueID(u string) string {
	// Strip optional .json suffix and split on "/".
	s := strings.TrimSuffix(u, ".json")
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		return "math500-" + s
	}
	// parts: [split, subject, number] e.g. ["test", "algebra", "24"]
	return "math500-" + parts[1] + "-" + parts[2]
}

// Compile-time check.
var _ benchmark.Loader = Loader{}
