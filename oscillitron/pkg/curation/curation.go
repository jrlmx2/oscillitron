// Package curation is the cold-path side of the three-tier
// self-improvement architecture (warm path / cold path / substrate).
//
// What it does:
//
//   - Reads a bench's `--stream-out` JSONL — one CaseResult per line,
//     written live during the bench run by the OnCase hook
//     (pkg/benchmark.JSONLStreamer).
//   - Filters cases that passed the grader at or above a quality
//     floor (MinScore) — no point asking the cold-path session to
//     reason about obvious failures.
//   - Batches the candidates and dispatches each batch to a
//     cold-path session (`Config.Adapter` — typically Hermes with a
//     stable session_id so its native memory accumulates across
//     batches of the same action).
//   - Parses the cold-path session's structured response listing
//     the case_ids worth preserving as few-shot exemplars.
//   - Writes the selected (prompt, output) pairs to the per-action
//     exemplar.Store. The warm-path pkg/adapter/curated then
//     retrieves them on subsequent bench runs.
//
// This package never touches the warm path. The cold-path session
// and the warm-path orchestrators communicate ONLY through the
// substrate (exemplar.Store). That decoupling preserves the
// invocation-isolation lock — warm-path APs see only retrieved
// exemplars, never the cold-path session's accumulated state.
//
// What this package deliberately does NOT do (v0):
//
//   - Recipes / anti-patterns (richer cold-path outputs that need
//     their own storage and consumption logic) — deferred to v2.1.
//   - Incremental / streaming JSONL processing — bench corpora are
//     small (~200 cases); load-all-then-batch is fine.
//   - Per-orchestrator action mapping — every run targets a single
//     Action key. Operators run curation multiple times with
//     different Action values for multi-action substrates.
package curation

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Defaults for Config.
const (
	DefaultAction          = "process"
	DefaultBatchSize       = 20
	DefaultSessionIDPrefix = "oscillitron:curation:"
)

// Config configures a curation run. Required fields: Adapter, Store,
// StreamPath. Everything else has sensible defaults.
type Config struct {
	// Adapter is the cold-path session substrate. Required. Typically
	// Hermes (which keeps memory across batches when SessionIDPrefix
	// gives a stable session_id); any adapter.Adapter works.
	Adapter adapter.Adapter
	// Store is the per-action exemplar substrate to write into.
	// Required.
	Store exemplar.Store
	// StreamPath is the bench's --stream-out JSONL path. Each line
	// is one benchmark.CaseResult. Required.
	StreamPath string
	// Action keys every exemplar this run produces. Default "process".
	// Run curation multiple times with different Action values to
	// populate separate per-action store partitions.
	Action string
	// OrchestratorAllowlist restricts which OrchestratorResults
	// contribute exemplar candidates. Empty = all orchestrators
	// eligible. Operators typically allow single-call orchestrators
	// (e.g., "frontier-*") and skip vote-style ones whose Raw output
	// is a concatenation of N attempts.
	OrchestratorAllowlist []string
	// BatchSize is the number of cases per cold-path call. Default
	// DefaultBatchSize (20). Smaller = faster + more calls; larger =
	// fewer calls but bigger prompts (may hit substrate context limit).
	BatchSize int
	// MinScore is the cutoff for considering a case as a candidate.
	// Zero (default) includes every case where Verdict.Pass=true,
	// regardless of Score. Set higher to require higher-quality
	// candidates on benchmarks with continuous scoring (e.g., 0.9
	// for HLE-style partial credit). For MCQ benchmarks where Score
	// is binary (0.0 / 1.0), the default is fine.
	MinScore float64
	// SessionIDPrefix is prepended to each batch's env.ID before
	// calling Adapter. Default DefaultSessionIDPrefix. The resulting
	// session_id (e.g., "oscillitron:curation:process:batch-3")
	// gives Hermes adapters memory continuity across batches of the
	// same Action.
	SessionIDPrefix string
	// PromptTemplate overrides the default cold-path prompt builder.
	// Nil = DefaultPromptTemplate. Custom templates must produce
	// output the model will respond to with the structured JSON
	// shape SelectionResponse expects.
	PromptTemplate func(action string, candidates []Candidate) string
	// Tracer emits per-batch + per-exemplar observability events.
	// Defaults to trace.Discard{}.
	Tracer trace.Tracer
}

// Candidate is one (case, orchestrator) tuple that passed the
// MinScore filter and is eligible to become an exemplar. The cold-
// path session sees these as input; the operator's prompt template
// formats them for the model.
type Candidate struct {
	CaseID           string
	OrchestratorName string
	Prompt           string
	Output           string
	Score            float64
	Expected         string
}

// SelectionResponse is the structured JSON shape the cold-path
// session is expected to reply with. The default prompt asks for
// this shape; custom prompt templates must do the same.
type SelectionResponse struct {
	Exemplars []SelectedExemplar `json:"exemplars"`
}

// SelectedExemplar identifies one candidate the cold-path session
// judged worth preserving. CaseID + OrchestratorName uniquely
// resolve back to a Candidate; Reason is free-form (logged but not
// stored).
type SelectedExemplar struct {
	CaseID           string `json:"case_id"`
	OrchestratorName string `json:"orchestrator_name,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// Result reports what a curation run produced.
type Result struct {
	CasesScanned       int // total CaseResults read from JSONL
	CandidatesFiltered int // passed Allowlist + MinScore
	BatchesProcessed   int // batches that completed cleanly
	BatchesFailed      int // batches where the adapter call or parse failed
	ExemplarsSelected  int // the cold-path session picked these
	ExemplarsAdded     int // successfully written to Store
	AdapterTokens      int // sum of TokensUsed across batches
	Elapsed            time.Duration
}

// Run is the cold-path curation driver. Reads StreamPath, filters
// candidates, batches and dispatches to Adapter, writes selected
// exemplars to Store. Returns the aggregate Result and any fatal
// error (per-batch failures are recorded in Result, not surfaced).
//
// Fatal errors: nil Adapter, nil Store, unreadable StreamPath, or
// ctx cancellation. Everything else is best-effort; a batch failure
// increments BatchesFailed and the loop continues.
func Run(ctx context.Context, cfg Config) (Result, error) {
	if cfg.Adapter == nil {
		return Result{}, errors.New("curation: Adapter is required")
	}
	if cfg.Store == nil {
		return Result{}, errors.New("curation: Store is required")
	}
	if cfg.StreamPath == "" {
		return Result{}, errors.New("curation: StreamPath is required")
	}
	if cfg.Action == "" {
		cfg.Action = DefaultAction
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.MinScore < 0 {
		cfg.MinScore = 0
	}
	if cfg.SessionIDPrefix == "" {
		cfg.SessionIDPrefix = DefaultSessionIDPrefix
	}
	if cfg.PromptTemplate == nil {
		cfg.PromptTemplate = DefaultPromptTemplate
	}
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}
	allowed := buildAllowedSet(cfg.OrchestratorAllowlist)

	started := time.Now()
	var result Result

	cases, err := readJSONL(cfg.StreamPath)
	if err != nil {
		return result, fmt.Errorf("curation: read stream: %w", err)
	}
	result.CasesScanned = len(cases)

	trace.Info(tracer, ctx, "curation.start",
		slog.String("stream", cfg.StreamPath),
		slog.String("action", cfg.Action),
		slog.Int("cases_total", result.CasesScanned),
		slog.Float64("min_score", cfg.MinScore),
		slog.Int("batch_size", cfg.BatchSize),
	)

	candidates := filterCandidates(cases, allowed, cfg.MinScore)
	result.CandidatesFiltered = len(candidates)

	if len(candidates) == 0 {
		trace.Info(tracer, ctx, "curation.no_candidates",
			slog.Int("cases_scanned", result.CasesScanned),
			slog.Float64("min_score", cfg.MinScore),
		)
		result.Elapsed = time.Since(started)
		return result, nil
	}

	// Batch + dispatch.
	for batchIdx, batch := range chunkCandidates(candidates, cfg.BatchSize) {
		if err := ctx.Err(); err != nil {
			result.Elapsed = time.Since(started)
			return result, err
		}
		batchResult := runBatch(ctx, cfg, tracer, batchIdx, batch)
		if batchResult.failed {
			result.BatchesFailed++
			continue
		}
		result.BatchesProcessed++
		result.AdapterTokens += batchResult.tokensUsed
		result.ExemplarsSelected += len(batchResult.selected)

		// Write selected exemplars to the store.
		for _, sel := range batchResult.selected {
			cand, ok := findCandidate(batch, sel)
			if !ok {
				trace.Error(tracer, ctx, "curation.unknown_selection",
					slog.String("case_id", sel.CaseID),
					slog.String("orchestrator", sel.OrchestratorName),
				)
				continue
			}
			ex := exemplar.Exemplar{
				Action:     cfg.Action,
				Prompt:     cand.Prompt,
				Output:     cand.Output,
				Score:      cand.Score,
				SourceCase: cand.CaseID + ":" + cand.OrchestratorName,
			}
			if err := cfg.Store.Add(ctx, ex); err != nil {
				trace.Error(tracer, ctx, "curation.exemplar_add_failed",
					slog.String("source_case", ex.SourceCase),
					slog.String("err", err.Error()),
				)
				continue
			}
			result.ExemplarsAdded++
			trace.Info(tracer, ctx, "curation.exemplar_added",
				slog.String("action", cfg.Action),
				slog.String("source_case", ex.SourceCase),
				slog.Float64("score", ex.Score),
				slog.String("reason", sel.Reason),
			)
		}
	}

	result.Elapsed = time.Since(started)
	trace.Info(tracer, ctx, "curation.done",
		slog.Int("cases_scanned", result.CasesScanned),
		slog.Int("candidates", result.CandidatesFiltered),
		slog.Int("batches_processed", result.BatchesProcessed),
		slog.Int("batches_failed", result.BatchesFailed),
		slog.Int("exemplars_selected", result.ExemplarsSelected),
		slog.Int("exemplars_added", result.ExemplarsAdded),
		slog.Int("adapter_tokens", result.AdapterTokens),
		slog.Duration("elapsed", result.Elapsed),
	)
	return result, nil
}

// batchResult bundles one batch's outcome.
type batchResult struct {
	failed     bool
	tokensUsed int
	selected   []SelectedExemplar
}

// runBatch builds the prompt, calls the cold-path adapter, parses
// the response, and returns the batch outcome. A failure to call or
// parse marks the batch as failed but does not surface an error —
// the run continues with the next batch.
func runBatch(ctx context.Context, cfg Config, tracer trace.Tracer, idx int, batch []Candidate) batchResult {
	trace.Info(tracer, ctx, "curation.batch_start",
		slog.Int("batch_idx", idx),
		slog.Int("batch_size", len(batch)),
		slog.String("action", cfg.Action),
	)

	prompt := cfg.PromptTemplate(cfg.Action, batch)
	envID := session.ID(fmt.Sprintf("%s%s:batch-%d", cfg.SessionIDPrefix, cfg.Action, idx))
	env := session.NewRoot(
		envID,
		prompt,
		`{"exemplars": [{"case_id": "...", "reason": "..."}]}`,
		classification.Internal,
		session.Budget{TokensRemaining: 32_000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{Playbook: session.PlaybookProcess, Confidence: 1.0}

	out, err := cfg.Adapter.Execute(ctx, env)
	if err != nil {
		trace.Error(tracer, ctx, "curation.batch_adapter_error",
			slog.Int("batch_idx", idx),
			slog.String("err", err.Error()),
		)
		return batchResult{failed: true}
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		trace.Error(tracer, ctx, "curation.batch_no_return_result",
			slog.Int("batch_idx", idx),
		)
		return batchResult{failed: true}
	}

	raw := out.Execute.ReturnResult.Result.Content
	selections, perr := parseSelectionResponse(raw)
	if perr != nil {
		trace.Error(tracer, ctx, "curation.batch_parse_error",
			slog.Int("batch_idx", idx),
			slog.String("err", perr.Error()),
			slog.String("raw_excerpt", excerpt(raw, 200)),
		)
		return batchResult{failed: true}
	}

	trace.Info(tracer, ctx, "curation.batch_completed",
		slog.Int("batch_idx", idx),
		slog.Int("selected", len(selections)),
		slog.Int("tokens", out.Execute.TokensUsed),
	)
	return batchResult{
		tokensUsed: out.Execute.TokensUsed,
		selected:   selections,
	}
}

// --- internals: stream read, filter, batch, parse ---

// readJSONL parses one CaseResult per line. Skips blank lines and
// logs (returns) the first parse error encountered — partial JSONL
// from a crashed bench is common, but a malformed line earlier than
// EOF likely indicates real corruption worth surfacing.
func readJSONL(path string) ([]caseResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []caseResult
	scanner := bufio.NewScanner(f)
	// Increase max line size — bench prompts can be large.
	buf := make([]byte, 0, 1<<20)
	scanner.Buffer(buf, 16<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var cr caseResult
		if err := json.Unmarshal([]byte(text), &cr); err != nil {
			return out, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, cr)
	}
	return out, scanner.Err()
}

// filterCandidates collects the (case, orchestrator) tuples that
// pass the allowlist + score floor. One CaseResult can produce
// multiple Candidates (one per qualifying orchestrator).
func filterCandidates(cases []caseResult, allowed map[string]bool, minScore float64) []Candidate {
	var out []Candidate
	for _, cr := range cases {
		for _, r := range cr.Results {
			if r.Error != "" {
				continue
			}
			if !r.Verdict.Pass {
				continue
			}
			if r.Verdict.Score < minScore {
				continue
			}
			if allowed != nil && !allowed[r.OrchestratorName] {
				continue
			}
			out = append(out, Candidate{
				CaseID:           cr.CaseID,
				OrchestratorName: r.OrchestratorName,
				Prompt:           cr.Case.Prompt,
				Output:           r.Answer.Raw,
				Score:            r.Verdict.Score,
				Expected:         cr.Case.Expected,
			})
		}
	}
	return out
}

// chunkCandidates splits the candidate list into chunks of at most
// size. Final chunk may be smaller.
func chunkCandidates(cands []Candidate, size int) [][]Candidate {
	if size <= 0 || len(cands) == 0 {
		return nil
	}
	var out [][]Candidate
	for i := 0; i < len(cands); i += size {
		end := i + size
		if end > len(cands) {
			end = len(cands)
		}
		out = append(out, cands[i:end])
	}
	return out
}

// findCandidate resolves a SelectedExemplar back to its Candidate
// in a batch. Matches on CaseID + OrchestratorName when the model
// supplied the orchestrator; falls back to CaseID-only when it
// didn't (older prompt templates may not).
func findCandidate(batch []Candidate, sel SelectedExemplar) (Candidate, bool) {
	for _, c := range batch {
		if c.CaseID != sel.CaseID {
			continue
		}
		if sel.OrchestratorName == "" || sel.OrchestratorName == c.OrchestratorName {
			return c, true
		}
	}
	return Candidate{}, false
}

// buildAllowedSet turns the allowlist into a map for O(1) lookup.
// Nil if the operator passed no allowlist (= all orchestrators
// eligible).
func buildAllowedSet(list []string) map[string]bool {
	if len(list) == 0 {
		return nil
	}
	m := make(map[string]bool, len(list))
	for _, name := range list {
		m[name] = true
	}
	return m
}

// parseSelectionResponse extracts the structured JSON the cold-path
// session was asked to return. Tolerates markdown-fenced JSON and
// leading prose ("Here are the selected exemplars: …") via the
// same brace-balancing scanner pkg/grader and pkg/judge use.
func parseSelectionResponse(raw string) ([]SelectedExemplar, error) {
	body := extractFirstJSONObject(raw)
	if body == "" {
		return nil, fmt.Errorf("no JSON object found")
	}
	var resp SelectionResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	return resp.Exemplars, nil
}

// excerpt truncates s to max bytes (rune-safe).
func excerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// --- caseResult: local schema matching pkg/benchmark's JSONL output ---
//
// We mirror the on-disk JSON shape (snake_case) here rather than
// importing benchmark.CaseResult and its serialization quirks. This
// is also the right place to add backward-compat shims if the JSON
// schema evolves.

type caseResult struct {
	CaseID  string               `json:"case_id"`
	Case    caseSpec             `json:"case"`
	Results []orchestratorResult `json:"results"`
}

type caseSpec struct {
	ID       string            `json:"id"`
	Prompt   string            `json:"prompt"`
	Expected string            `json:"expected"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type orchestratorResult struct {
	OrchestratorName string  `json:"orchestrator_name"`
	Answer           answer  `json:"answer"`
	Verdict          verdict `json:"verdict"`
	Error            string  `json:"error,omitempty"`
}

type answer struct {
	Raw        string `json:"raw"`
	Extracted  string `json:"extracted"`
	Calls      int    `json:"calls"`
	TokensUsed int    `json:"tokens_used"`
}

type verdict struct {
	GraderName string  `json:"grader_name"`
	Pass       bool    `json:"pass"`
	Score      float64 `json:"score"`
	Notes      string  `json:"notes,omitempty"`
	TokensUsed int     `json:"tokens_used"`
}
