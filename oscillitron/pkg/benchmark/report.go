package benchmark

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/stakes"
)

// WriteJSON serializes a Report to w as indented JSON. Designed so the
// output is round-trippable for downstream analysis (notebooks,
// plotters, CI gates) — the `error` fields in OrchestratorResult are
// stringified rather than emitted as Go's `{}` default.
//
// Use cmd/bench's --report-out flag to write to disk; programmatic
// callers can write to any io.Writer.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toReportJSON(r))
}

// WriteJSONFile is a convenience wrapper that writes the report to
// path, creating the file (or truncating it if it exists).
func WriteJSONFile(path string, r Report) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("benchmark.WriteJSONFile: %w", err)
	}
	defer f.Close()
	if err := WriteJSON(f, r); err != nil {
		return fmt.Errorf("benchmark.WriteJSONFile: %w", err)
	}
	return nil
}

// AppendCaseJSONL appends one CaseResult as a single JSON line
// (no leading/trailing whitespace, trailing newline) to w. Use as
// the body of a RunnerConfig.OnCase callback to stream per-case
// results to a file as the bench progresses — crash safety on
// long runs.
//
// Errors stringify the same way as in WriteJSON. The JSON shape
// for one line matches one entry in Report.Cases under the
// top-level "cases" key.
func AppendCaseJSONL(w io.Writer, cr CaseResult) error {
	// json.Encoder writes a trailing newline by default — perfect
	// for JSONL — but it also pretty-prints if SetIndent is called.
	// Default Encoder behavior (no SetIndent) is one-line-per-Encode.
	enc := json.NewEncoder(w)
	return enc.Encode(toCaseResultJSON(cr))
}

// JSONLStreamer wraps an io.Writer to flush after each AppendCase
// call so a tail -f on the output file sees per-case progress
// immediately. Use with os.File.Sync via Flusher for stronger
// crash safety.
type JSONLStreamer struct {
	W io.Writer
	// Flusher is called after every AppendCase. Typically *os.File's
	// Sync method. Nil = no flush (the OS may buffer writes).
	Flusher func() error
}

// AppendCase writes one CaseResult to the underlying writer and
// invokes Flusher (if set).
func (s *JSONLStreamer) AppendCase(cr CaseResult) error {
	if err := AppendCaseJSONL(s.W, cr); err != nil {
		return err
	}
	if s.Flusher != nil {
		return s.Flusher()
	}
	return nil
}

// toCaseResultJSON exposes the internal conversion so AppendCaseJSONL
// can share the same error-stringification + secondary-verdict logic
// as the full Report writer. Kept in report.go so the schema stays
// in one place.
func toCaseResultJSON(cr CaseResult) caseResultJSON {
	results := make([]orchestratorResultJSON, len(cr.Results))
	for j, or := range cr.Results {
		errStr := ""
		if or.Err != nil {
			errStr = or.Err.Error()
		}
		results[j] = orchestratorResultJSON{
			OrchestratorName: or.OrchestratorName,
			Answer:           answerJSON(or.Answer),
			Verdict:          toVerdictJSON(or.Verdict),
			Error:            errStr,
		}
	}
	return caseResultJSON{
		CaseID:  cr.CaseID,
		Case:    caseJSON(cr.Case),
		Results: results,
	}
}

// --- internal serialization shapes ---
//
// We use private mirror types rather than putting json tags on the
// public Report types because (a) the error fields need stringification
// and (b) we want the JSON shape to evolve independently of the
// in-process Go types if a future schema-versioning concern hits.

type reportJSON struct {
	BenchmarkName string               `json:"benchmark_name"`
	StartedAt     time.Time            `json:"started_at"`
	EndedAt       time.Time            `json:"ended_at"`
	ElapsedMS     int64                `json:"elapsed_ms"`
	Aggregates    []aggregateStatsJSON `json:"aggregates"`
	Windows       []windowStatsJSON    `json:"windows,omitempty"`
	Cases         []caseResultJSON     `json:"cases"`
}

type aggregateStatsJSON struct {
	OrchestratorName  string  `json:"orchestrator_name"`
	Cases             int     `json:"cases"`
	Successes         int     `json:"successes"`
	Failures          int     `json:"failures"`
	Errors            int     `json:"errors"`
	PassRate          float64 `json:"pass_rate"`
	AvgScore          float64 `json:"avg_score"`
	TotalCalls        int     `json:"total_calls"`
	TotalTokens       int     `json:"total_tokens"`
	TotalGraderTokens int     `json:"total_grader_tokens"`
	TotalActualUSD    float64 `json:"total_actual_usd"`
	TotalFrontierUSD  float64 `json:"total_frontier_usd"`
	SavingsRatio      float64 `json:"savings_ratio"`
}

type windowStatsJSON struct {
	EndCase         int                   `json:"end_case"`
	Size            int                   `json:"size"`
	PerOrchestrator []windowOrchStatsJSON `json:"per_orchestrator"`
}

type windowOrchStatsJSON struct {
	OrchestratorName string  `json:"orchestrator_name"`
	PassRate         float64 `json:"pass_rate"`
	AvgScore         float64 `json:"avg_score"`
}

type caseResultJSON struct {
	CaseID  string                   `json:"case_id"`
	Case    caseJSON                 `json:"case"`
	Results []orchestratorResultJSON `json:"results"`
}

type caseJSON struct {
	ID       string            `json:"id"`
	Prompt   string            `json:"prompt"`
	Expected string            `json:"expected"`
	Stakes   stakes.Level      `json:"stakes,omitempty"`
	Goal     string            `json:"goal,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type orchestratorResultJSON struct {
	OrchestratorName string      `json:"orchestrator_name"`
	Answer           answerJSON  `json:"answer"`
	Verdict          verdictJSON `json:"verdict"`
	Error            string      `json:"error,omitempty"`
}

type answerJSON struct {
	Raw        string  `json:"raw"`
	Extracted  string  `json:"extracted"`
	Calls      int     `json:"calls"`
	TokensUsed int     `json:"tokens_used"`
	Confidence float64 `json:"confidence,omitempty"`
	CopeAction string  `json:"cope_action,omitempty"`
}

type verdictJSON struct {
	GraderName string        `json:"grader_name"`
	Pass       bool          `json:"pass"`
	Score      float64       `json:"score"`
	Notes      string        `json:"notes,omitempty"`
	TokensUsed int           `json:"tokens_used"`
	Secondary  []verdictJSON `json:"secondary,omitempty"`
}

// --- conversion ---

func toReportJSON(r Report) reportJSON {
	out := reportJSON{
		BenchmarkName: r.BenchmarkName,
		StartedAt:     r.StartedAt,
		EndedAt:       r.EndedAt,
		ElapsedMS:     r.EndedAt.Sub(r.StartedAt).Milliseconds(),
		Aggregates:    make([]aggregateStatsJSON, len(r.Aggregates)),
		Cases:         make([]caseResultJSON, len(r.Cases)),
	}
	for i, a := range r.Aggregates {
		out.Aggregates[i] = aggregateStatsJSON(a)
	}
	if len(r.Windows) > 0 {
		out.Windows = make([]windowStatsJSON, len(r.Windows))
		for i, w := range r.Windows {
			perOrch := make([]windowOrchStatsJSON, len(w.PerOrchestrator))
			for j, p := range w.PerOrchestrator {
				perOrch[j] = windowOrchStatsJSON(p)
			}
			out.Windows[i] = windowStatsJSON{
				EndCase:         w.EndCase,
				Size:            w.Size,
				PerOrchestrator: perOrch,
			}
		}
	}
	for i, cr := range r.Cases {
		out.Cases[i] = toCaseResultJSON(cr)
	}
	return out
}

func toVerdictJSON(v Verdict) verdictJSON {
	out := verdictJSON{
		GraderName: v.GraderName,
		Pass:       v.Pass,
		Score:      v.Score,
		Notes:      v.Notes,
		TokensUsed: v.TokensUsed,
	}
	if len(v.Secondary) > 0 {
		out.Secondary = make([]verdictJSON, len(v.Secondary))
		for i, s := range v.Secondary {
			out.Secondary[i] = toVerdictJSON(s)
		}
	}
	return out
}
