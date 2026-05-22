// CLAUDE GENERATED
// Package gpqa loads GPQA Diamond cases from disk.
//
// Dataset: https://huggingface.co/datasets/Idavidrein/gpqa
//
// Gated on HuggingFace; the operator downloads the snapshot, converts
// to the simple JSON shape this loader expects, and commits it under
// cmd/bench/cases/ (not auto-fetched at runtime — bench runs must be
// reproducible and offline-capable).
//
// Expected JSON shape:
//
//	[
//	  {
//	    "id": "gpqa-diamond-001",
//	    "question": "What is …?",
//	    "correct_answer": "…",
//	    "incorrect_answers": ["…", "…", "…"],
//	    "subdomain": "physics"
//	  },
//	  …
//	]
//
// The loader deterministically places the correct answer into one of
// A/B/C/D using a hash of the case ID — same ID always lands the
// correct answer at the same letter across runs, so benchmark results
// are reproducible.
package gpqa

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// rawCase is the per-case JSON schema. Operators run the conversion
// script (cmd/bench/cases/README.md) to produce a file of these.
type rawCase struct {
	ID               string   `json:"id"`
	Question         string   `json:"question"`
	CorrectAnswer    string   `json:"correct_answer"`
	IncorrectAnswers []string `json:"incorrect_answers"`
	Subdomain        string   `json:"subdomain,omitempty"`
}

// Loader reads cases from a JSON file. Implements benchmark.Loader.
type Loader struct {
	// Path is the JSON file path. Required.
	Path string
	// NameStr appears in benchmark reports. Defaults to "gpqa-diamond".
	NameStr string
	// Limit optionally caps the number of cases loaded (0 = all).
	// Useful for fast smoke runs without re-running the full corpus.
	Limit int
}

// Name implements benchmark.Loader.
func (l Loader) Name() string {
	if l.NameStr != "" {
		return l.NameStr
	}
	return "gpqa-diamond"
}

// Load implements benchmark.Loader.
func (l Loader) Load(_ context.Context) ([]benchmark.Case, error) {
	if l.Path == "" {
		return nil, fmt.Errorf("gpqa: Path is required")
	}
	data, err := os.ReadFile(l.Path)
	if err != nil {
		return nil, fmt.Errorf("gpqa: read %s: %w", l.Path, err)
	}
	var raws []rawCase
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("gpqa: parse %s: %w", l.Path, err)
	}
	if len(raws) == 0 {
		return nil, fmt.Errorf("gpqa: %s contains zero cases", l.Path)
	}

	// Stable order: sort by ID so loader.Load is deterministic across
	// runs even if the source JSON's array order shifts.
	sort.Slice(raws, func(i, j int) bool { return raws[i].ID < raws[j].ID })

	cases := make([]benchmark.Case, 0, len(raws))
	for _, r := range raws {
		if l.Limit > 0 && len(cases) >= l.Limit {
			break
		}
		c, err := buildCase(r)
		if err != nil {
			return nil, fmt.Errorf("gpqa: case %q: %w", r.ID, err)
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// buildCase converts one rawCase into a benchmark.Case. The correct
// answer is placed at a deterministic letter based on the case ID
// hash so reruns produce identical prompts.
func buildCase(r rawCase) (benchmark.Case, error) {
	if r.ID == "" {
		return benchmark.Case{}, fmt.Errorf("missing id")
	}
	if r.Question == "" {
		return benchmark.Case{}, fmt.Errorf("missing question")
	}
	if r.CorrectAnswer == "" {
		return benchmark.Case{}, fmt.Errorf("missing correct_answer")
	}
	if len(r.IncorrectAnswers) != 3 {
		return benchmark.Case{}, fmt.Errorf("expected 3 incorrect answers, got %d", len(r.IncorrectAnswers))
	}

	// Place the correct answer at one of A/B/C/D deterministically.
	correctIdx := deterministicLetterIndex(r.ID, 4)
	options := make([]string, 4)
	options[correctIdx] = r.CorrectAnswer
	wrong := r.IncorrectAnswers
	w := 0
	for i := 0; i < 4; i++ {
		if i == correctIdx {
			continue
		}
		options[i] = wrong[w]
		w++
	}

	correctLetter := string(rune('A' + correctIdx))

	var b strings.Builder
	b.WriteString("Question: ")
	b.WriteString(r.Question)
	b.WriteString("\n\n")
	for i, opt := range options {
		fmt.Fprintf(&b, "%s) %s\n", string(rune('A'+i)), opt)
	}
	b.WriteString("\nAnswer with a single letter: A, B, C, or D.")

	md := map[string]string{}
	if r.Subdomain != "" {
		md["subdomain"] = r.Subdomain
	}

	return benchmark.Case{
		ID:       r.ID,
		Prompt:   b.String(),
		Expected: correctLetter,
		Metadata: md,
	}, nil
}

// deterministicLetterIndex returns an integer in [0, n) derived from a
// SHA-256 hash of id. Same id always returns the same index — keeps
// benchmark prompts byte-stable across runs.
func deterministicLetterIndex(id string, n int) int {
	sum := sha256.Sum256([]byte(id))
	v := binary.BigEndian.Uint64(sum[:8])
	return int(v % uint64(n))
}

// Compile-time check.
var _ benchmark.Loader = Loader{}
