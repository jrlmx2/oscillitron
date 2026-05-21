// CLAUDE GENERATED
package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// fakeHermes stands in for a Hermes /v1/runs surface. Each test
// configures the SSE event sequence it wants emitted, plus optional
// status overrides. The same instance serves both Evaluate and
// Execute calls; tests reconfigure events between calls when needed.
type fakeHermes struct {
	t        *testing.T
	server   *httptest.Server
	postRuns atomic.Int32
	getEvts  atomic.Int32

	mu           sync.Mutex
	runStatus    int    // 0 → 202, otherwise the override
	runRespBody  string
	eventsStatus int      // 0 → 200
	events       []string // raw SSE payload lines (each becomes "data: <line>\n\n")
	lastBody     map[string]any
}

func newFake(t *testing.T) *fakeHermes {
	f := &fakeHermes{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f.postRuns.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.lastBody = body
		status := f.runStatus
		respBody := f.runRespBody
		f.mu.Unlock()
		if status != 0 {
			http.Error(w, respBody, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"run_id":"run_fake","status":"started"}`))
	})
	mux.HandleFunc("/v1/runs/run_fake/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		f.getEvts.Add(1)
		f.mu.Lock()
		status := f.eventsStatus
		events := append([]string(nil), f.events...)
		f.mu.Unlock()
		if status != 0 {
			http.Error(w, "events error", status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, line := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte(": stream closed\n\n"))
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeHermes) setEvents(events ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = events
}

// completedEvent produces a run.completed SSE payload carrying the
// supplied JSON-as-a-string output and a fixed token usage.
func completedEvent(output string) string {
	return fmt.Sprintf(`{"event":"run.completed","run_id":"run_fake","output":%q,"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}`, output)
}

func envFor(id session.ID, content, schema string) session.Envelope {
	return session.Envelope{
		SchemaVersion: session.SchemaVersion,
		ID:            id,
		RootID:        id,
		Path:          []session.ID{id},
		Input:         session.Payload{Kind: "task", Content: content},
		OutputSchema:  schema,
		Budget:        session.Budget{DepthRemaining: 3},
	}
}

func envWithEvaluate(id session.ID, content string, pb session.Playbook) session.Envelope {
	e := envFor(id, content, "free-text")
	e.Evaluate = &session.Evaluate{Playbook: pb, Confidence: 0.9}
	return e
}

func TestSingleEndpointBindsEvaluateAndAllPlaybooks(t *testing.T) {
	cfg := SingleEndpoint("http://x", "test-model")
	if cfg.EvaluateEndpoint.BaseURL != "http://x" {
		t.Errorf("EvaluateEndpoint missing or wrong: %+v", cfg.EvaluateEndpoint)
	}
	for _, pb := range []session.Playbook{
		session.PlaybookPlan, session.PlaybookProcess,
		session.PlaybookCritique, session.PlaybookVerifyGrounded,
		session.PlaybookCompose,
	} {
		if _, ok := cfg.ExecuteEndpoints[pb]; !ok {
			t.Errorf("SingleEndpoint missing playbook %q", pb)
		}
	}
}

func TestNewRejectsMissingEvaluateEndpoint(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with no EvaluateEndpoint should error")
	}
	if _, err := New(Config{
		EvaluateEndpoint: Endpoint{BaseURL: "http://x"},
	}); err == nil {
		t.Fatal("New with no ExecuteEndpoints should error")
	}
}

func TestNewRejectsBlankBaseURL(t *testing.T) {
	cfg := Config{
		EvaluateEndpoint: Endpoint{BaseURL: "http://x"},
		ExecuteEndpoints: map[session.Playbook]Endpoint{
			session.PlaybookProcess: {BaseURL: "   "},
		},
	}
	if _, err := New(cfg); err == nil {
		t.Fatal("blank BaseURL should error")
	}
}

func TestEvaluateHappyPath(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent(`{"playbook":"process","rationale":"single task","confidence":0.85}`))

	a, err := New(SingleEndpoint(f.server.URL, "test-model"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	env, err := a.Evaluate(context.Background(), envFor("ap-1", "say hi", "{answer}"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if env.Evaluate == nil {
		t.Fatal("Evaluate field not populated")
	}
	if env.Evaluate.Playbook != session.PlaybookProcess {
		t.Errorf("Playbook = %q, want process", env.Evaluate.Playbook)
	}
	if env.Evaluate.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", env.Evaluate.Confidence)
	}
	if env.Evaluate.Rationale != "single task" {
		t.Errorf("Rationale = %q", env.Evaluate.Rationale)
	}
	// Session ID should be "<envID>:evaluate".
	f.mu.Lock()
	body := f.lastBody
	f.mu.Unlock()
	if body["session_id"] != "ap-1:evaluate" {
		t.Errorf("session_id = %v, want ap-1:evaluate", body["session_id"])
	}
}

func TestEvaluateRejectsUnknownPlaybook(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent(`{"playbook":"foo","confidence":0.5}`))
	a, _ := New(SingleEndpoint(f.server.URL, ""))
	if _, err := a.Evaluate(context.Background(), envFor("ap-x", "hello", "")); err == nil {
		t.Fatal("expected error for unknown playbook")
	}
}

func TestEvaluateUnstructuredFallback(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent("this is not JSON at all"))

	cfg := SingleEndpoint(f.server.URL, "")
	a, _ := New(cfg)
	env, err := a.Evaluate(context.Background(), envFor("ap-1", "hello", ""))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Fallback: PlaybookProcess at very low confidence.
	if env.Evaluate.Playbook != session.PlaybookProcess {
		t.Errorf("fallback playbook = %q, want process", env.Evaluate.Playbook)
	}
	if env.Evaluate.Confidence != 0.1 {
		t.Errorf("fallback confidence = %v, want 0.1", env.Evaluate.Confidence)
	}
}

func TestEvaluateRequireStructuredRejectsUnstructured(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent("definitely not JSON"))
	cfg := SingleEndpoint(f.server.URL, "")
	cfg.RequireStructured = true
	a, _ := New(cfg)
	if _, err := a.Evaluate(context.Background(), envFor("ap-1", "hello", "")); err == nil {
		t.Fatal("expected error with RequireStructured=true")
	}
}

func TestExecutePlan(t *testing.T) {
	f := newFake(t)
	planJSON := `{"sub_aps":[{"input_kind":"task","input":"step 1","output_schema":"{r}","classification":"","needs_verification":false},{"input_kind":"task","input":"step 2","output_schema":"{r}","classification":"","needs_verification":true}],"recompose":"sequential"}`
	f.setEvents(completedEvent(planJSON))

	a, _ := New(SingleEndpoint(f.server.URL, ""))
	env, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "decompose", session.PlaybookPlan))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if env.Execute == nil || env.Execute.Category != session.CategoryEmitSubtree {
		t.Fatalf("Execute = %+v", env.Execute)
	}
	if env.Execute.EmitSubtree == nil || len(env.Execute.EmitSubtree.SubAPs) != 2 {
		t.Fatalf("SubAPs not parsed: %+v", env.Execute.EmitSubtree)
	}
	if env.Execute.EmitSubtree.Recompose != session.RecomposeSequential {
		t.Errorf("Recompose = %q, want sequential", env.Execute.EmitSubtree.Recompose)
	}
	if !env.Execute.EmitSubtree.SubAPs[1].NeedsVerification {
		t.Errorf("NeedsVerification on second seed should be true")
	}
	if env.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want done", env.ExitReason)
	}
}

func TestExecutePlanRejectsUnknownRecompose(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent(`{"sub_aps":[],"recompose":"bogus"}`))
	a, _ := New(SingleEndpoint(f.server.URL, ""))
	if _, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", session.PlaybookPlan)); err == nil {
		t.Fatal("expected error for unknown recompose spec")
	}
}

func TestExecuteProcess(t *testing.T) {
	f := newFake(t)
	groundedTrue := true
	procJSON := `{"content":"42","confidence":0.92,"grounded_pass":true,"contradictions":[],"open_questions":["why 42?"]}`
	f.setEvents(completedEvent(procJSON))

	a, _ := New(SingleEndpoint(f.server.URL, ""))
	env, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "compute", session.PlaybookProcess))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if env.Execute.Category != session.CategoryReturnResult {
		t.Fatalf("Category = %q, want return_result", env.Execute.Category)
	}
	rr := env.Execute.ReturnResult
	if rr.Result.Content != "42" {
		t.Errorf("Content = %q", rr.Result.Content)
	}
	if rr.Confidence != 0.92 {
		t.Errorf("Confidence = %v", rr.Confidence)
	}
	if rr.Signals.GroundedPass == nil || *rr.Signals.GroundedPass != groundedTrue {
		t.Errorf("GroundedPass not parsed: %+v", rr.Signals.GroundedPass)
	}
	if len(rr.Signals.OpenQuestions) != 1 {
		t.Errorf("OpenQuestions = %v", rr.Signals.OpenQuestions)
	}
}

func TestExecuteCritique(t *testing.T) {
	f := newFake(t)
	critJSON := `{"verdict":"issues","issues":[{"severity":"warning","where":"line 12","what":"loop bound suspicious"}]}`
	f.setEvents(completedEvent(critJSON))

	a, _ := New(SingleEndpoint(f.server.URL, ""))
	env, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "check", session.PlaybookCritique))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if env.Execute.Category != session.CategoryVerifierSignal {
		t.Fatalf("Category = %q, want verifier_signal", env.Execute.Category)
	}
	vs := env.Execute.VerifierSignal
	if vs.Verdict != session.VerdictIssues {
		t.Errorf("Verdict = %q", vs.Verdict)
	}
	if len(vs.Issues) != 1 || vs.Issues[0].Severity != session.SeverityWarning {
		t.Errorf("Issues not parsed: %+v", vs.Issues)
	}
}

func TestExecuteVerifyGrounded(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent(`{"verdict":"pass","issues":[]}`))

	a, _ := New(SingleEndpoint(f.server.URL, ""))
	env := envWithEvaluate("ap-1", "ground check", session.PlaybookVerifyGrounded)
	env.VerifySpec = &session.VerifySpec{Kind: "compile", Spec: "go build ./..."}
	env, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if env.Execute.VerifierSignal.Verdict != session.VerdictPass {
		t.Errorf("Verdict = %q, want pass", env.Execute.VerifierSignal.Verdict)
	}
}

func TestExecuteCalledBeforeEvaluate(t *testing.T) {
	f := newFake(t)
	a, _ := New(SingleEndpoint(f.server.URL, ""))
	if _, err := a.Execute(context.Background(), envFor("ap-1", "x", "")); err == nil {
		t.Fatal("expected error when Execute called before Evaluate")
	}
}

func TestExecuteUnknownPlaybookEndpoint(t *testing.T) {
	f := newFake(t)
	// Configure only PlaybookProcess; evaluate "plan" → should error.
	cfg := Config{
		EvaluateEndpoint: Endpoint{BaseURL: f.server.URL},
		ExecuteEndpoints: map[session.Playbook]Endpoint{
			session.PlaybookProcess: {BaseURL: f.server.URL},
		},
	}
	a, _ := New(cfg)
	if _, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", session.PlaybookPlan)); err == nil {
		t.Fatal("expected error: no endpoint for PlaybookPlan")
	}
}

func TestExecuteUnstructuredFallbackByCategory(t *testing.T) {
	cases := []struct {
		name         string
		pb           session.Playbook
		wantCategory session.Category
	}{
		{"plan → empty emit_subtree", session.PlaybookPlan, session.CategoryEmitSubtree},
		{"process → low-conf return_result", session.PlaybookProcess, session.CategoryReturnResult},
		{"critique → 'issues' verifier_signal", session.PlaybookCritique, session.CategoryVerifierSignal},
		{"verify_grounded → 'issues' verifier_signal", session.PlaybookVerifyGrounded, session.CategoryVerifierSignal},
		{"compose → low-conf return_result", session.PlaybookCompose, session.CategoryReturnResult},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.setEvents(completedEvent("not JSON at all, just words"))
			a, _ := New(SingleEndpoint(f.server.URL, ""))
			env, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", c.pb))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if env.Execute.Category != c.wantCategory {
				t.Errorf("Category = %q, want %q", env.Execute.Category, c.wantCategory)
			}
		})
	}
}

func TestExecuteRequireStructuredRejectsUnstructured(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent("not JSON"))
	cfg := SingleEndpoint(f.server.URL, "")
	cfg.RequireStructured = true
	a, _ := New(cfg)
	if _, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", session.PlaybookProcess)); err == nil {
		t.Fatal("expected error with RequireStructured=true")
	}
}

func TestApprovalRejected(t *testing.T) {
	f := newFake(t)
	f.setEvents(`{"event":"approval.request","run_id":"run_fake","tool":"shell"}`)
	a, _ := New(SingleEndpoint(f.server.URL, ""))
	_, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", session.PlaybookProcess))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Errorf("expected approval error, got %v", err)
	}
}

func TestRunFailed(t *testing.T) {
	f := newFake(t)
	f.setEvents(`{"event":"run.failed","run_id":"run_fake","error":"model rate-limited"}`)
	a, _ := New(SingleEndpoint(f.server.URL, ""))
	_, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", session.PlaybookProcess))
	if err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Errorf("expected failure error, got %v", err)
	}
}

func TestStreamEndedWithoutTerminal(t *testing.T) {
	f := newFake(t)
	f.setEvents(`{"event":"message.delta","run_id":"run_fake","delta":"partial"}`)
	a, _ := New(SingleEndpoint(f.server.URL, ""))
	_, err := a.Execute(context.Background(), envWithEvaluate("ap-1", "x", session.PlaybookProcess))
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Errorf("expected terminal-event error, got %v", err)
	}
}

func TestCostTrackingAcrossBothPhases(t *testing.T) {
	f := newFake(t)
	tracker := cost.New(cost.Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 30})
	tracker.Register("test-model", cost.Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 3})

	cfg := SingleEndpoint(f.server.URL, "test-model")
	cfg.Cost = tracker
	a, _ := New(cfg)

	// Evaluate
	f.setEvents(completedEvent(`{"playbook":"process","confidence":0.9}`))
	env, err := a.Evaluate(context.Background(), envFor("ap-1", "x", ""))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Execute
	f.setEvents(completedEvent(`{"content":"done","confidence":0.9}`))
	env, err = a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if env.Evaluate.TokensUsed != 15 || env.Execute.TokensUsed != 15 {
		t.Errorf("token usage not stamped: eval=%d exec=%d", env.Evaluate.TokensUsed, env.Execute.TokensUsed)
	}
	// Cost ledger should have 2 entries total — one per phase.
	sum := tracker.Summary()
	if len(sum.Entries) != 2 {
		t.Errorf("cost entries: got %d, want 2", len(sum.Entries))
	}
	for _, e := range sum.Entries {
		if e.Model != "test-model" {
			t.Errorf("cost entry model = %q, want test-model", e.Model)
		}
	}
	// Envelope Trace should carry the sum of both phases' actual +
	// frontier counterfactual costs.
	wantActual := sum.TotalActualUSD
	wantFrontier := sum.TotalFrontierUSD
	if env.Trace.CostUSD != wantActual {
		t.Errorf("env.Trace.CostUSD = %v, want %v (sum of both phases)",
			env.Trace.CostUSD, wantActual)
	}
	if env.Trace.CostVsFrontierBaselineUSD != wantFrontier {
		t.Errorf("env.Trace.CostVsFrontierBaselineUSD = %v, want %v",
			env.Trace.CostVsFrontierBaselineUSD, wantFrontier)
	}
}

func TestPostRunsServerError(t *testing.T) {
	f := newFake(t)
	f.mu.Lock()
	f.runStatus = http.StatusInternalServerError
	f.runRespBody = "boom"
	f.mu.Unlock()

	a, _ := New(SingleEndpoint(f.server.URL, ""))
	if _, err := a.Evaluate(context.Background(), envFor("ap-1", "x", "")); err == nil {
		t.Fatal("expected POST /v1/runs error to surface")
	}
}

func TestRawInstructionsOverride(t *testing.T) {
	f := newFake(t)
	f.setEvents(completedEvent(`{"playbook":"process","confidence":0.5}`))

	cfg := SingleEndpoint(f.server.URL, "")
	cfg.RawEvaluateInstructions = "use my custom evaluate prompt"
	a, _ := New(cfg)

	_, err := a.Evaluate(context.Background(), envFor("ap-1", "x", ""))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	f.mu.Lock()
	got := f.lastBody["instructions"]
	f.mu.Unlock()
	if got != "use my custom evaluate prompt" {
		t.Errorf("instructions = %q, want override", got)
	}
}
