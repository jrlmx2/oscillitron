package minimal

import (
	"regexp"
	"strconv"
	"strings"
)

// responseRE matches <response>...</response> with one level of inner
// tag nesting allowed (sufficient for cases where the inner content
// itself contains XML-flavored markup; deeper nesting is rare enough
// to defer). Go's RE2 doesn't support recursive matching, so deeper
// nests would need a state-machine extractor.
var responseRE = regexp.MustCompile(`(?s)<response>(.*?)</response>`)

// confidenceRE matches the literal `<confidence>X</confidence>` slot.
// The numeric content tolerates surrounding whitespace and a "0.0 to
// 1.0" placeholder pass-through (returned as "not found" rather than
// a bogus zero so the caller doesn't accidentally feed the placeholder
// into the cope dispatcher).
var confidenceTagRE = regexp.MustCompile(`(?s)<confidence>(.*?)</confidence>`)

// ExtractResponseTag pulls the last <response>...</response> content
// from raw text. Returns (content, true) on hit, ("", false) on miss.
// The content is trimmed of surrounding whitespace but otherwise
// preserved verbatim — the caller (typically the bench's letter or
// boxed extractor) decides what to do with it.
//
// Last-match-wins mirrors the dispatcher pattern used elsewhere
// (Multichoice grader, BoxedAnswer grader): the model may write
// intermediate <response> tags while reasoning out loud, then
// commit to a final one.
func ExtractResponseTag(raw string) (string, bool) {
	matches := responseRE.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return "", false
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return "", false
	}
	return strings.TrimSpace(last[1]), true
}

// ExtractConfidenceTag pulls the last <confidence>X</confidence>
// numeric value from raw text. Returns (value, true) when the inner
// content parses cleanly as a number, (0, false) otherwise.
//
// Tolerates a "0.0 to 1.0" placeholder (the literal instruction text
// the model echoes back instead of filling it in) by failing to
// parse — the caller treats it as missing rather than zero.
//
// Values are NOT clamped here; pkg/notice.NormalizeConfidence /
// EffectiveConfidence apply downstream clamping and signal
// adjustments. Last-match-wins.
func ExtractConfidenceTag(raw string) (float64, bool) {
	matches := confidenceTagRE.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0, false
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return 0, false
	}
	s := strings.TrimSpace(last[1])
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
