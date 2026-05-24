// Package categorize classifies why each OrchestratorResult failed.
// "Pass/fail" alone hides important detail — the empirical work
// today showed phi4-mini's 13% pass rate on GPQA was 71% format
// leakage (model reasoned correctly but didn't end with a parseable
// letter), not reasoning failure. Categorization surfaces this
// distinction so operators can pick the right intervention:
// recipes for format issues, substrate change for reasoning, etc.
//
// The classifier is heuristic-only (no LLM calls — free, fast,
// deterministic). Buckets are coarse on purpose; finer
// classification (e.g., LLM-as-judge on the failure mode) is a
// separate concern that can layer on top.
//
// Output is intended for stdout reports and aggregated telemetry.
// The package does not modify the underlying Report or
// OrchestratorResult — it derives a parallel Summary structure.
package categorize

import (
	"regexp"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// Category is the classification of one failure (or pass).
// Pass cases are categorized as CategoryPass — operators can
// filter to fails-only by ignoring that one.
type Category string

const (
	// CategoryPass is the case where the verdict was Pass=true.
	// Not really a "failure mode" but it's useful for the summary
	// to surface the pass count alongside the failure buckets.
	CategoryPass Category = "pass"

	// CategoryFormatNoLetter is the most actionable failure mode:
	// the model produced a response but `Answer.Extracted` was
	// empty. Usually means the model reasoned but didn't end with
	// a parseable letter. Fixable with a prompt-suffix format
	// recipe (cmd/bench --prompt-suffix).
	CategoryFormatNoLetter Category = "format_no_letter"

	// CategoryWrongLetter is the second-most actionable: the
	// model committed to a single letter, but it was wrong.
	// Substrate is reasoning, just incorrectly. Lift comes from
	// better substrate, recipes for specific question types, or
	// ensemble orchestration (when substrate is good enough).
	CategoryWrongLetter Category = "wrong_letter"

	// CategoryRefusal is when the substrate refused to answer
	// ("I cannot…", "I won't…"). Distinct from format/reasoning
	// failure — needs jailbreak-style prompt engineering or a
	// less safety-tuned substrate.
	CategoryRefusal Category = "refusal"

	// CategoryEmptyResponse is when Answer.Raw is empty or
	// whitespace-only. Substrate didn't even try. Likely an
	// adapter-side issue, model hung up, or context truncation.
	CategoryEmptyResponse Category = "empty_response"

	// CategoryAdapterError is when OrchestratorResult.Err was set.
	// The adapter or governor failed before the model could
	// produce output. Operational concern, not a substrate
	// quality concern.
	CategoryAdapterError Category = "adapter_error"

	// CategoryOther catches everything that doesn't fit the
	// above. Should be rare; investigate if it isn't.
	CategoryOther Category = "other"
)

// AllCategories returns the canonical ordering used by the summary
// printer. CategoryPass first, then failure modes ordered roughly
// by actionability (format fixes first, then reasoning, then
// operational).
func AllCategories() []Category {
	return []Category{
		CategoryPass,
		CategoryFormatNoLetter,
		CategoryWrongLetter,
		CategoryRefusal,
		CategoryEmptyResponse,
		CategoryAdapterError,
		CategoryOther,
	}
}

// refusalPattern matches common substrate refusals. Conservative
// (avoids false positives on responses that mention refusal
// phrases as part of reasoning). Operators can extend.
//
// Handles both "I am unable to" and "I'm unable to" contractions.
var refusalPattern = regexp.MustCompile(
	`(?i)(I cannot|I can't|I won't|I will not|I am unable to|I am not able to|I'm unable to|I'm not able to)`,
)

// Classify returns the Category for one OrchestratorResult given
// the originating Case. Returns CategoryPass when Verdict.Pass is
// true; otherwise inspects the failure shape.
//
// Ordering is intentional: error first (overrides everything),
// then format (cheapest fix), then refusal, then wrong-letter,
// then empty, with CategoryOther as the last-resort bucket.
func Classify(c benchmark.Case, r benchmark.OrchestratorResult) Category {
	if r.Err != nil {
		return CategoryAdapterError
	}
	if r.Verdict.Pass {
		return CategoryPass
	}
	raw := r.Answer.Raw
	extracted := strings.TrimSpace(r.Answer.Extracted)

	// Empty raw — substrate didn't produce output at all.
	if strings.TrimSpace(raw) == "" {
		return CategoryEmptyResponse
	}

	// Refusal detection on the raw response. Run BEFORE
	// no-letter check because a refusal often produces no letter
	// AND a refusal sentence; the refusal is the more
	// informative bucket.
	if refusalPattern.MatchString(raw) {
		return CategoryRefusal
	}

	// No letter extracted — most actionable failure mode.
	if extracted == "" {
		return CategoryFormatNoLetter
	}

	// A letter was extracted; it must be wrong (Verdict.Pass is
	// false). Confirm shape before committing — single letter
	// in A-Z.
	if isSingleLetter(extracted) {
		return CategoryWrongLetter
	}

	return CategoryOther
}

// isSingleLetter reports whether s is exactly one ASCII letter.
// Multichoice extracts could include accidental punctuation if
// the extractor regex changes; this guard keeps the bucket
// honest.
func isSingleLetter(s string) bool {
	if len(s) != 1 {
		return false
	}
	c := s[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// --- Aggregation ---

// OrchestratorSummary is one orchestrator's per-category breakdown.
type OrchestratorSummary struct {
	OrchestratorName string
	// Counts is the per-category count. Categories with zero
	// occurrences are still present (= 0) so consumers can show
	// a stable table layout.
	Counts map[Category]int
	// Total is the sum of all counts (= number of cases scored
	// for this orchestrator).
	Total int
}

// Summary is the full per-orchestrator breakdown across a Report.
type Summary struct {
	// Orchestrators is one entry per orchestrator, in the order
	// they appear in the Report.
	Orchestrators []OrchestratorSummary
}

// Aggregate classifies every OrchestratorResult in the Report and
// returns the per-orchestrator counts.
func Aggregate(report benchmark.Report) Summary {
	if len(report.Aggregates) == 0 {
		return Summary{}
	}
	// Build per-orchestrator counters keyed by orchestrator name
	// (and index, since the Report stores parallel slices).
	summaries := make([]OrchestratorSummary, len(report.Aggregates))
	for i, a := range report.Aggregates {
		summaries[i] = OrchestratorSummary{
			OrchestratorName: a.OrchestratorName,
			Counts:           initialCounts(),
		}
	}
	for _, cr := range report.Cases {
		for i, r := range cr.Results {
			if i >= len(summaries) {
				continue
			}
			cat := Classify(cr.Case, r)
			summaries[i].Counts[cat]++
			summaries[i].Total++
		}
	}
	return Summary{Orchestrators: summaries}
}

// initialCounts returns a counts map pre-populated with every
// known category at zero, so consumers don't have to nil-check.
func initialCounts() map[Category]int {
	out := make(map[Category]int, len(AllCategories()))
	for _, c := range AllCategories() {
		out[c] = 0
	}
	return out
}

// --- Pretty-printing ---

// FormatTable renders a Summary as a stdout-friendly table.
// One row per category, one column per orchestrator. Returns the
// formatted string ready to write to an io.Writer.
//
// Example output:
//
//	Failure categorization (per orchestrator)
//	  category              frontier  vote-3
//	  pass                       8       10
//	  format_no_letter          42       26
//	  wrong_letter               9       24
//	  refusal                    0        0
//	  empty_response             0        0
//	  adapter_error              1        0
//	  other                      0        0
//	  ────────────────────────────────────
//	  total                     60       60
func FormatTable(s Summary) string {
	if len(s.Orchestrators) == 0 {
		return "(no orchestrators in summary)\n"
	}
	var b strings.Builder
	b.WriteString("Failure categorization (per orchestrator)\n")

	// Header.
	b.WriteString("  ")
	b.WriteString(padRight("category", 24))
	for _, o := range s.Orchestrators {
		b.WriteString(padRight(o.OrchestratorName, 32))
	}
	b.WriteString("\n")

	// Body — one row per category in canonical order.
	for _, cat := range AllCategories() {
		b.WriteString("  ")
		b.WriteString(padRight(string(cat), 24))
		for _, o := range s.Orchestrators {
			b.WriteString(padRight(formatInt(o.Counts[cat]), 32))
		}
		b.WriteString("\n")
	}

	// Footer — totals.
	b.WriteString("  " + strings.Repeat("─", 24+32*len(s.Orchestrators)) + "\n")
	b.WriteString("  " + padRight("total", 24))
	for _, o := range s.Orchestrators {
		b.WriteString(padRight(formatInt(o.Total), 32))
	}
	b.WriteString("\n")
	return b.String()
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}

func formatInt(n int) string {
	// strconv.Itoa is fine but using a tiny manual buffer keeps
	// FormatTable allocation-free per call.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
