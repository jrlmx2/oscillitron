// CLAUDE GENERATED
// Package mmlu_pro loads MMLU-Pro cases from disk.
//
// Dataset: https://huggingface.co/datasets/TIGER-Lab/MMLU-Pro
//
// Operator-downloaded JSON; not auto-fetched at runtime (bench runs
// must be reproducible and offline-capable, same convention as the
// gpqa loader). Convert the HuggingFace parquet to JSON via the
// operator script described in cmd/bench/cases/README.md.
//
// Expected JSON shape (mirrors the upstream dataset schema):
//
//	[
//	  {
//	    "question_id": 70,
//	    "question": "Find the degree for…",
//	    "options": ["0", "4", "2", "6", "8", "1", "5", "3", "10", "7"],
//	    "answer": "B",
//	    "answer_index": 1,
//	    "category": "math",
//	    "src": "ori_mmlu-abstract_algebra"
//	  },
//	  …
//	]
//
// Some MMLU-Pro questions have fewer than 10 options. The loader
// renders A through whatever-the-last-letter-is (up to J) and
// surfaces the count to the prompt's closing-position imperative.
//
// Unlike the gpqa loader, MMLU-Pro keeps the source's `answer`
// letter directly — the dataset commits to a stable letter per
// question and standard MMLU-Pro benchmarks evaluate against it.
// A permutation mode (deterministic re-shuffle per case ID) is
// not in v0 scope; if positional-bias defense is needed later,
// add it as an opt-in flag.
package mmlu_pro

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

// MaxOptions is the largest option set MMLU-Pro questions use.
// The Multichoice grader is parameterized to handle A..J via its
// Letters field, but the loader caps here to avoid surprises if
// the upstream dataset ever drifts.
const MaxOptions = 10

// rawCase mirrors the upstream JSON shape. Fields the loader
// doesn't use (cot_content, answer_index) are intentionally
// omitted — answer is already a letter, so answer_index would be
// redundant; cot_content is reasoning trace useful for eval-set
// background but not for the run prompt.
type rawCase struct {
	QuestionID int      `json:"question_id"`
	Question   string   `json:"question"`
	Options    []string `json:"options"`
	Answer     string   `json:"answer"`
	Category   string   `json:"category,omitempty"`
	Src        string   `json:"src,omitempty"`
}

// Loader reads cases from a JSON file. Implements benchmark.Loader.
type Loader struct {
	// Path is the JSON file path. Required.
	Path string
	// NameStr appears in benchmark reports. Defaults to "mmlu-pro".
	NameStr string
	// Limit optionally caps the number of cases loaded (0 = all).
	Limit int
}

// Name implements benchmark.Loader.
func (l Loader) Name() string {
	if l.NameStr != "" {
		return l.NameStr
	}
	return "mmlu-pro"
}

// Load implements benchmark.Loader.
func (l Loader) Load(_ context.Context) ([]benchmark.Case, error) {
	if l.Path == "" {
		return nil, fmt.Errorf("mmlu_pro: Path is required")
	}
	data, err := os.ReadFile(l.Path)
	if err != nil {
		return nil, fmt.Errorf("mmlu_pro: read %s: %w", l.Path, err)
	}
	var raws []rawCase
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("mmlu_pro: parse %s: %w", l.Path, err)
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("mmlu_pro: %s contains zero cases", l.Path)
	}

	// Stable order: sort by question_id so loader.Load is
	// deterministic across runs even if the source JSON's array
	// order shifts (same discipline as the gpqa loader).
	sort.Slice(raws, func(i, j int) bool { return raws[i].QuestionID < raws[j].QuestionID })

	cases := make([]benchmark.Case, 0, len(raws))
	for _, r := range raws {
		if l.Limit > 0 && len(cases) >= l.Limit {
			break
		}
		c, err := buildCase(r)
		if err != nil {
			return nil, fmt.Errorf("mmlu_pro: case %d: %w", r.QuestionID, err)
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// buildCase converts one rawCase into a benchmark.Case. The source
// answer letter is used directly — no permutation. The prompt is
// built with the closing-position imperative (see pkg/adapter/minimal
// bug #5 fix) parameterized to the actual option count.
func buildCase(r rawCase) (benchmark.Case, error) {
	if r.QuestionID == 0 {
		return benchmark.Case{}, fmt.Errorf("missing question_id")
	}
	if r.Question == "" {
		return benchmark.Case{}, fmt.Errorf("missing question")
	}
	if len(r.Options) < 2 {
		return benchmark.Case{}, fmt.Errorf("need at least 2 options, got %d", len(r.Options))
	}
	if len(r.Options) > MaxOptions {
		return benchmark.Case{}, fmt.Errorf("more than %d options not supported (got %d)", MaxOptions, len(r.Options))
	}
	answer := strings.TrimSpace(strings.ToUpper(r.Answer))
	if len(answer) != 1 {
		return benchmark.Case{}, fmt.Errorf("answer must be one letter, got %q", r.Answer)
	}
	answerIdx := int(answer[0] - 'A')
	if answerIdx < 0 || answerIdx >= len(r.Options) {
		return benchmark.Case{}, fmt.Errorf("answer %q out of range for %d options", r.Answer, len(r.Options))
	}

	lastLetter := string(rune('A' + len(r.Options) - 1))

	var b strings.Builder
	b.WriteString("Question: ")
	b.WriteString(r.Question)
	b.WriteString("\n\n")
	for i, opt := range r.Options {
		fmt.Fprintf(&b, "%s) %s\n", string(rune('A'+i)), opt)
	}
	b.WriteString("\nEnd your response with the single letter (A through ")
	b.WriteString(lastLetter)
	b.WriteString(") as your final character.")

	md := map[string]string{
		"option_count": strconv.Itoa(len(r.Options)),
	}
	if r.Category != "" {
		md["category"] = r.Category
	}
	if r.Src != "" {
		md["src"] = r.Src
	}

	return benchmark.Case{
		ID:       fmt.Sprintf("mmlu-pro-%d", r.QuestionID),
		Prompt:   b.String(),
		Expected: answer,
		Metadata: md,
	}, nil
}

// Compile-time check.
var _ benchmark.Loader = Loader{}
