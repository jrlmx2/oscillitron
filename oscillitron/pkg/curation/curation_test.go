// CLAUDE GENERATED
package curation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// --- helpers: stub adapter, mem store, JSONL fixture ---

// scriptedAdapter returns a pre-recorded response per Execute call
// (in order). Records the env it was called with so tests can
// assert on prompt shape and session_id continuity.
type scriptedAdapter struct {
	mu        sync.Mutex
	responses []string
	errs      []error
	idx       int
	calls     []session.Envelope // captured envelopes, in call order
}

func (s *scriptedAdapter) Name() string { return "scripted" }

func (s *scriptedAdapter) Evaluate(_ context.Context, env session.Envelope) (session.Envelope, error) {
	return env, nil
}

func (s *scriptedAdapter) Execute(_ context.Context, env session.Envelope) (session.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, env)
	i := s.idx
	s.idx++
	if i >= len(s.responses) {
		return env, fmt.Errorf("scripted: out of responses (call %d)", i)
	}
	if i < len(s.errs) && s.errs[i] != nil {
		return env, s.errs[i]
	}
	env.Execute = &session.Execute{
		Category: session.CategoryReturnResult,
		ReturnResult: &session.ReturnResultPayload{
			Result: session.Payload{Kind: "result", Content: s.responses[i]},
		},
		TokensUsed: 100,
	}
	return env, nil
}

var _ adapter.Adapter = (*scriptedAdapter)(nil)

// memStore is an in-memory exemplar.Store for tests.
type memStore struct {
	mu    sync.Mutex
	added []exemplar.Exemplar
}

func (m *memStore) Add(_ context.Context, e exemplar.Exemplar) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.added = append(m.added, e)
	return nil
}
func (m *memStore) Retrieve(_ context.Context, _, _ string, _ int) ([]exemplar.Exemplar, error) {
	return nil, nil
}
func (m *memStore) GC(_ context.Context) (int, error) { return 0, nil }

var _ exemplar.Store = (*memStore)(nil)

// writeJSONLFixture writes lines to a tmp file and returns the path.
func writeJSONLFixture(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stream.jsonl")
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// caseJSONL builds one JSONL line representing a CaseResult with N
// orchestrator results.
func caseJSONL(t *testing.T, caseID, prompt, expected string, results []orchestratorResult) string {
	t.Helper()
	cr := caseResult{
		CaseID:  caseID,
		Case:    caseSpec{ID: caseID, Prompt: prompt, Expected: expected},
		Results: results,
	}
	b, err := json.Marshal(cr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// makeSelectionResponse builds a stub adapter response payload that
// the curation parser will accept.
func makeSelectionResponse(picks ...string) string {
	parts := make([]string, len(picks))
	for i, c := range picks {
		parts[i] = fmt.Sprintf(`{"case_id":"%s","reason":"good"}`, c)
	}
	return `{"exemplars":[` + strings.Join(parts, ",") + `]}`
}

// --- Config validation ---

func TestRun_RequiresAdapter(t *testing.T) {
	_, err := Run(context.Background(), Config{Store: &memStore{}, StreamPath: "/x"})
	if err == nil {
		t.Fatal("expected error with nil Adapter")
	}
}

func TestRun_RequiresStore(t *testing.T) {
	_, err := Run(context.Background(), Config{
		Adapter: &scriptedAdapter{}, StreamPath: "/x",
	})
	if err == nil {
		t.Fatal("expected error with nil Store")
	}
}

func TestRun_RequiresStreamPath(t *testing.T) {
	_, err := Run(context.Background(), Config{
		Adapter: &scriptedAdapter{}, Store: &memStore{},
	})
	if err == nil {
		t.Fatal("expected error with empty StreamPath")
	}
}

func TestRun_UnreadableStream_Errors(t *testing.T) {
	_, err := Run(context.Background(), Config{
		Adapter: &scriptedAdapter{}, Store: &memStore{},
		StreamPath: "/nonexistent/curation-fixture.jsonl",
	})
	if err == nil {
		t.Fatal("expected error reading missing stream")
	}
}

// --- Filter logic ---

func TestRun_FiltersOnlyPassingResults(t *testing.T) {
	lines := []string{
		caseJSONL(t, "c1", "q1", "A", []orchestratorResult{
			{OrchestratorName: "single", Answer: answer{Raw: "A is correct", Calls: 1, TokensUsed: 50},
				Verdict: verdict{Pass: true, Score: 1.0}},
		}),
		caseJSONL(t, "c2", "q2", "B", []orchestratorResult{
			{OrchestratorName: "single", Answer: answer{Raw: "wrong reasoning", Calls: 1, TokensUsed: 50},
				Verdict: verdict{Pass: false, Score: 0.0}}, // skipped: failed
		}),
		caseJSONL(t, "c3", "q3", "C", []orchestratorResult{
			{OrchestratorName: "errored", Error: "substrate down"}, // skipped: errored
		}),
	}
	path := writeJSONLFixture(t, lines)

	adapter := &scriptedAdapter{responses: []string{makeSelectionResponse("c1")}}
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.CasesScanned != 3 {
		t.Errorf("CasesScanned = %d, want 3", res.CasesScanned)
	}
	if res.CandidatesFiltered != 1 {
		t.Errorf("CandidatesFiltered = %d, want 1 (only c1 passed and was non-errored)", res.CandidatesFiltered)
	}
	if res.ExemplarsAdded != 1 {
		t.Errorf("ExemplarsAdded = %d, want 1", res.ExemplarsAdded)
	}
}

func TestRun_RespectsMinScore(t *testing.T) {
	lines := []string{
		caseJSONL(t, "c1", "q1", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out1", Calls: 1},
				Verdict: verdict{Pass: true, Score: 0.5}},
		}),
		caseJSONL(t, "c2", "q2", "B", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out2", Calls: 1},
				Verdict: verdict{Pass: true, Score: 1.0}},
		}),
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{responses: []string{makeSelectionResponse("c2")}}
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
		MinScore: 0.9, // c1's 0.5 excluded
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.CandidatesFiltered != 1 {
		t.Errorf("CandidatesFiltered = %d, want 1", res.CandidatesFiltered)
	}
	if res.ExemplarsAdded != 1 || store.added[0].SourceCase != "c2:s" {
		t.Errorf("wrong exemplar added: %+v", store.added)
	}
}

func TestRun_RespectsAllowlist(t *testing.T) {
	lines := []string{
		caseJSONL(t, "c1", "q", "A", []orchestratorResult{
			{OrchestratorName: "single", Answer: answer{Raw: "out", Calls: 1},
				Verdict: verdict{Pass: true, Score: 1.0}},
			{OrchestratorName: "vote-3", Answer: answer{Raw: "voted", Calls: 3},
				Verdict: verdict{Pass: true, Score: 1.0}},
		}),
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{responses: []string{makeSelectionResponse("c1")}}
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
		OrchestratorAllowlist: []string{"single"}, // skip vote-3
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.CandidatesFiltered != 1 {
		t.Errorf("CandidatesFiltered = %d, want 1", res.CandidatesFiltered)
	}
	if len(store.added) != 1 || store.added[0].SourceCase != "c1:single" {
		t.Errorf("expected only 'single' exemplar; got %+v", store.added)
	}
}

func TestRun_NoCandidates_NoBatches(t *testing.T) {
	lines := []string{
		caseJSONL(t, "c1", "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Verdict: verdict{Pass: false}},
		}),
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{} // no responses needed
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.BatchesProcessed != 0 || res.ExemplarsAdded != 0 {
		t.Errorf("expected no-op result; got %+v", res)
	}
	if len(adapter.calls) != 0 {
		t.Errorf("adapter should not be called when no candidates; got %d calls", len(adapter.calls))
	}
}

// --- Batching ---

func TestRun_BatchesByBatchSize(t *testing.T) {
	// 25 candidates, BatchSize=10 → 3 batches (10, 10, 5).
	var lines []string
	for i := 0; i < 25; i++ {
		lines = append(lines, caseJSONL(t, fmt.Sprintf("c%02d", i), "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out", Calls: 1}, Verdict: verdict{Pass: true, Score: 1.0}},
		}))
	}
	path := writeJSONLFixture(t, lines)

	// Each batch picks one candidate (the first).
	adapter := &scriptedAdapter{
		responses: []string{
			makeSelectionResponse("c00"),
			makeSelectionResponse("c10"),
			makeSelectionResponse("c20"),
		},
	}
	store := &memStore{}
	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path, BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.BatchesProcessed != 3 {
		t.Errorf("BatchesProcessed = %d, want 3", res.BatchesProcessed)
	}
	if len(adapter.calls) != 3 {
		t.Errorf("adapter calls = %d, want 3", len(adapter.calls))
	}
	if res.ExemplarsAdded != 3 {
		t.Errorf("ExemplarsAdded = %d, want 3", res.ExemplarsAdded)
	}
}

func TestRun_BatchFailureRecordedNotFatal(t *testing.T) {
	// 2 batches; first errors at adapter, second succeeds.
	var lines []string
	for i := 0; i < 4; i++ {
		lines = append(lines, caseJSONL(t, fmt.Sprintf("c%d", i), "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out", Calls: 1}, Verdict: verdict{Pass: true, Score: 1.0}},
		}))
	}
	path := writeJSONLFixture(t, lines)

	adapter := &scriptedAdapter{
		responses: []string{"", makeSelectionResponse("c2")}, // first response unused due to err
		errs:      []error{errors.New("substrate hiccup"), nil},
	}
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path, BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Run: %v (batch failure should not be fatal)", err)
	}
	if res.BatchesFailed != 1 || res.BatchesProcessed != 1 {
		t.Errorf("expected 1 failed + 1 processed; got %+v", res)
	}
	if res.ExemplarsAdded != 1 {
		t.Errorf("expected 1 exemplar added (second batch); got %d", res.ExemplarsAdded)
	}
}

// --- Parse + selection ---

func TestRun_ToleratesMarkdownFencedJSON(t *testing.T) {
	lines := []string{
		caseJSONL(t, "c1", "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out", Calls: 1}, Verdict: verdict{Pass: true, Score: 1.0}},
		}),
	}
	path := writeJSONLFixture(t, lines)
	fenced := "Sure! Here are the picks:\n```json\n" + makeSelectionResponse("c1") + "\n```\n"
	adapter := &scriptedAdapter{responses: []string{fenced}}
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExemplarsAdded != 1 {
		t.Errorf("expected 1 exemplar from fenced response; got %d", res.ExemplarsAdded)
	}
}

func TestRun_GarbageResponseIsBatchFailure(t *testing.T) {
	lines := []string{
		caseJSONL(t, "c1", "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out", Calls: 1}, Verdict: verdict{Pass: true, Score: 1.0}},
		}),
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{responses: []string{"not json at all, just words"}}
	store := &memStore{}

	res, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
	})
	if err != nil {
		t.Fatalf("Run: %v (parse failure should not be fatal)", err)
	}
	if res.BatchesFailed != 1 {
		t.Errorf("expected 1 failed batch; got %+v", res)
	}
	if res.ExemplarsAdded != 0 {
		t.Errorf("ExemplarsAdded = %d, want 0", res.ExemplarsAdded)
	}
}

func TestRun_UnknownSelectionIsSkipped(t *testing.T) {
	// Model hallucinates a case_id not in the batch — should be
	// skipped, not added.
	lines := []string{
		caseJSONL(t, "c1", "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out", Calls: 1}, Verdict: verdict{Pass: true, Score: 1.0}},
		}),
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{
		responses: []string{makeSelectionResponse("c1", "c999-hallucinated")},
	}
	store := &memStore{}

	res, _ := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
	})
	if res.ExemplarsSelected != 2 {
		t.Errorf("ExemplarsSelected = %d, want 2 (model returned 2)", res.ExemplarsSelected)
	}
	if res.ExemplarsAdded != 1 {
		t.Errorf("ExemplarsAdded = %d, want 1 (hallucinated id skipped)", res.ExemplarsAdded)
	}
}

// --- Session ID continuity ---

func TestRun_SessionIDPrefixUsedPerBatch(t *testing.T) {
	var lines []string
	for i := 0; i < 4; i++ {
		lines = append(lines, caseJSONL(t, fmt.Sprintf("c%d", i), "q", "A", []orchestratorResult{
			{OrchestratorName: "s", Answer: answer{Raw: "out", Calls: 1}, Verdict: verdict{Pass: true, Score: 1.0}},
		}))
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{
		responses: []string{makeSelectionResponse("c0"), makeSelectionResponse("c2")},
	}
	store := &memStore{}

	_, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path,
		Action: "process", BatchSize: 2,
		SessionIDPrefix: "oscillitron:curation:",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(adapter.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(adapter.calls))
	}
	want0 := session.ID("oscillitron:curation:process:batch-0")
	want1 := session.ID("oscillitron:curation:process:batch-1")
	if adapter.calls[0].ID != want0 {
		t.Errorf("batch 0 session id = %q, want %q", adapter.calls[0].ID, want0)
	}
	if adapter.calls[1].ID != want1 {
		t.Errorf("batch 1 session id = %q, want %q", adapter.calls[1].ID, want1)
	}
}

// --- Exemplar shape ---

func TestRun_ExemplarFieldsCorrect(t *testing.T) {
	lines := []string{
		caseJSONL(t, "case-abc", "What is 2+2?", "B", []orchestratorResult{
			{
				OrchestratorName: "frontier-haiku",
				Answer:           answer{Raw: "Reasoning... therefore B.", Extracted: "B", Calls: 1, TokensUsed: 50},
				Verdict:          verdict{GraderName: "multichoice", Pass: true, Score: 1.0},
			},
		}),
	}
	path := writeJSONLFixture(t, lines)
	adapter := &scriptedAdapter{responses: []string{makeSelectionResponse("case-abc")}}
	store := &memStore{}

	_, err := Run(context.Background(), Config{
		Adapter: adapter, Store: store, StreamPath: path, Action: "process",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(store.added) != 1 {
		t.Fatalf("len(added) = %d, want 1", len(store.added))
	}
	e := store.added[0]
	if e.Action != "process" {
		t.Errorf("Action = %q, want process", e.Action)
	}
	if e.Prompt != "What is 2+2?" {
		t.Errorf("Prompt = %q", e.Prompt)
	}
	if e.Output != "Reasoning... therefore B." {
		t.Errorf("Output = %q (should be Raw, not Extracted)", e.Output)
	}
	if e.Score != 1.0 {
		t.Errorf("Score = %f", e.Score)
	}
	if e.SourceCase != "case-abc:frontier-haiku" {
		t.Errorf("SourceCase = %q, want 'case-abc:frontier-haiku'", e.SourceCase)
	}
}

// --- JSONL parsing edge cases ---

func TestReadJSONL_SkipsBlankLines(t *testing.T) {
	line := caseJSONL(t, "c1", "q", "A", []orchestratorResult{
		{OrchestratorName: "s", Answer: answer{Raw: "out"}, Verdict: verdict{Pass: true, Score: 1.0}},
	})
	path := filepath.Join(t.TempDir(), "stream.jsonl")
	// Blank line at start, middle, end.
	body := "\n" + line + "\n\n" + line + "\n\n"
	_ = os.WriteFile(path, []byte(body), 0o644)

	cases, err := readJSONL(path)
	if err != nil {
		t.Fatalf("readJSONL: %v", err)
	}
	if len(cases) != 2 {
		t.Errorf("got %d cases, want 2", len(cases))
	}
}

func TestReadJSONL_MalformedLineErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.jsonl")
	body := "this is not json\n"
	_ = os.WriteFile(path, []byte(body), 0o644)

	_, err := readJSONL(path)
	if err == nil {
		t.Fatal("expected parse error on malformed line")
	}
}

// --- Helper functions ---

func TestChunkCandidates(t *testing.T) {
	cands := make([]Candidate, 25)
	for i := range cands {
		cands[i] = Candidate{CaseID: fmt.Sprintf("c%d", i)}
	}
	chunks := chunkCandidates(cands, 10)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if len(chunks[0]) != 10 || len(chunks[1]) != 10 || len(chunks[2]) != 5 {
		t.Errorf("chunk sizes wrong: %d, %d, %d", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
}

func TestChunkCandidates_EmptyOrZeroSize(t *testing.T) {
	if chunkCandidates(nil, 10) != nil {
		t.Error("nil input should produce nil chunks")
	}
	if chunkCandidates([]Candidate{{}}, 0) != nil {
		t.Error("zero size should produce nil chunks")
	}
}

func TestExtractFirstJSONObject_HandlesNesting(t *testing.T) {
	in := `prefix text {"outer": {"inner": "x"}} suffix`
	got := extractFirstJSONObject(in)
	if got != `{"outer": {"inner": "x"}}` {
		t.Errorf("got %q", got)
	}
}

func TestExtractFirstJSONObject_NoObjectReturnsEmpty(t *testing.T) {
	if got := extractFirstJSONObject("just plain text"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractFirstJSONObject_BraceInString(t *testing.T) {
	// Brace inside a JSON string literal should NOT increment depth.
	in := `{"k": "value with } brace"}`
	got := extractFirstJSONObject(in)
	if got != in {
		t.Errorf("got %q, want full input", got)
	}
}

// --- Prompt template smoke ---

func TestDefaultPromptTemplate_IncludesAllFields(t *testing.T) {
	cands := []Candidate{
		{CaseID: "c1", OrchestratorName: "frontier", Prompt: "What is...", Output: "Answer.", Score: 1.0},
	}
	got := DefaultPromptTemplate("process", cands)

	for _, want := range []string{
		"process", "c1", "frontier", "What is", "Answer", `"exemplars"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected prompt to contain %q; got:\n%s", want, got)
		}
	}
}
