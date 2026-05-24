// Package grader implements benchmark.Grader strategies.
//
//   - Multichoice: exact-match on extracted A/B/C/D letter (or A-Z
//     for benchmarks with more options). No LLM judge needed.
//   - Dual: wraps Primary + Secondary graders, runs them in parallel,
//     surfaces Primary as canonical and Secondary verdicts as a
//     sanity-check signal. Useful when bootstrapping a local LLM
//     judge against a frontier judge to track agreement before
//     trusting it solo.
//
// Each grader package may be wired with a *vram.Governor where LLM
// calls are involved (Dual itself isn't substrate-touching, but
// LLM-backed graders it wraps usually are).
package grader

import (
	"context"
	"regexp"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// MultichoiceLetters is the default option set: A through D.
const MultichoiceLetters = "ABCD"

// Multichoice grades MCQ benchmarks (GPQA, MMLU-Pro) by extracting
// the chosen letter from the model's response and comparing against
// Case.Expected. Pass/fail binary; Score is 0.0 or 1.0.
//
// Extraction order:
//
//  1. If Answer.Extracted is non-empty, use it directly (the
//     orchestrator already did extraction; e.g., Vote).
//  2. Otherwise extract from Answer.Raw using ExtractLetter.
//
// Case-insensitive comparison. Whitespace-trimmed.
type Multichoice struct {
	// NameStr appears in benchmark reports. Defaults to "multichoice".
	NameStr string
	// Letters is the option set; defaults to MultichoiceLetters
	// (ABCD). Override for benchmarks with more options (e.g.,
	// MMLU-Pro uses A-J).
	Letters string
}

// Name implements benchmark.Grader.
func (m Multichoice) Name() string {
	if m.NameStr != "" {
		return m.NameStr
	}
	return "multichoice"
}

// Grade implements benchmark.Grader.
func (m Multichoice) Grade(_ context.Context, c benchmark.Case, a benchmark.Answer) (benchmark.Verdict, error) {
	letters := m.Letters
	if letters == "" {
		letters = MultichoiceLetters
	}

	extracted := strings.TrimSpace(a.Extracted)
	if extracted == "" {
		extracted = ExtractLetter(a.Raw, letters)
	}
	expected := strings.TrimSpace(c.Expected)

	pass := strings.EqualFold(extracted, expected) && extracted != ""
	score := 0.0
	if pass {
		score = 1.0
	}
	notes := ""
	if !pass {
		if extracted == "" {
			notes = "no letter extracted"
		} else {
			notes = "extracted=" + extracted + " expected=" + expected
		}
	}
	return benchmark.Verdict{
		GraderName: m.Name(),
		Pass:       pass,
		Score:      score,
		Notes:      notes,
	}, nil
}

// ExtractLetter pulls a multiple-choice letter from a model response.
// Strategy: scan for letter tokens (word-boundary-delimited single
// chars in the configured set) and return the LAST match — models
// typically reason first and state the answer at the end ("…
// therefore the answer is C.").
//
// If no letter from `letters` is found, returns "".
//
// Special-case: a response that is exactly one letter (after trim)
// returns that letter immediately. Common for terse models.
func ExtractLetter(raw, letters string) string {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) == 1 && strings.ContainsAny(strings.ToUpper(trimmed), letters) {
		return strings.ToUpper(trimmed)
	}
	upper := strings.ToUpper(raw)
	// Word-boundary single letter from the option set.
	pattern := `\b[` + letters + `]\b`
	re := regexp.MustCompile(pattern)
	matches := re.FindAllString(upper, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// Compile-time check.
var _ benchmark.Grader = Multichoice{}
