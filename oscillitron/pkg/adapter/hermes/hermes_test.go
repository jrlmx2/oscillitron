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
// status overrides.
type fakeHermes struct {
	t        *testing.T
	server   *httptest.Server
	postRuns atomic.Int32
	getEvts  atomic.Int32

	mu          sync.Mutex
	runStatus   int // 0 → 202, otherwise the override
	runRespBody string
	eventsStatus int // 0 → 200
	events      []string // raw SSE payload lines (each becomes "data: <line>\n\n")

	// lastBody captures the most recent POST body for body-shape assertions.
	lastBody map[string]any
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

func envFor(bf session.BrainFunction, id session.ID, content string) session.Envelope {
	return session.Envelope{
		SchemaVersion: session.SchemaVersion,
		ID:            id,
		BrainFunction: bf,
		Input:         session.Input{Type: "prompt", Content: content},
		OutputSchema:  "free-text",
		Budget:        session.Budget{DepthRemaining: 1},
	}
}

func TestSingleEndpointBindsAllBrainFunctions(t *testing.T) {
	cfg := SingleEndpoint("http://x", "test-model")
	for _, bf := range []session.BrainFunction{
		session.BrainPerception, session.BrainRetrieval, session.BrainPlanning,
		session.BrainReasoning, session.BrainCritic, session.BrainComposition,
	} {
		if _, ok := cfg.Endpoints[bf]; !ok {
			t.Errorf("SingleEndpoint missing brain function %q", bf)
		}
	}
}

func TestNewRejectsEmptyEndpoints(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with no endpoints should error")
	}
}

func TestNewRejectsBlankBaseURL(t *testing.T) {
	cfg := Config{Endpoints: map[session.BrainFunction]Endpoint{
		session.BrainReasoning: {BaseURL: "   "},
	}}
	if _, err := New(cfg); err == nil {
		t.Fatal("blank BaseURL should error")
	}
}

func TestCallHappyPath(t *testing.T) {
	f := newFake(t)
	// Structured envelope inside run.completed — the documented v0
	// output contract. Adapter parses it into the rich Output fields.
	structured := `{"content":"Hello world.","classification":"ok","confidence":0.9,"signals":["s1"],"contradictions":[],"open_questions":[],"sub_aps":[]}`
	completed := fmt.Sprintf(`{"event":"run.completed","run_id":"run_fake","output":%q,"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}`, structured)
	f.setEvents(
		`{"event":"message.delta","run_id":"run_fake","delta":"Hello "}`,
		`{"event":"message.delta","run_id":"run_fake","delta":"world."}`,
		completed,
	)

	tracker := cost.New(cost.Pricing{InputUSDPerMTok: 10, OutputUSDPerMTok: 30})
	tracker.Register("test-model", cost.Pricing{InputUSDPerMTok: 1, OutputUSDPerMTok: 3})

	a, err := New(Config{
		Endpoints: map[session.BrainFunction]Endpoint{
			session.BrainReasoning: {BaseURL: f.server.URL, Model: "test-model"},
		},
		Cost: tracker,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := a.Call(context.Background(), envFor(session.BrainReasoning, "sess-1", "say hi"))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Content != "Hello world." {
		t.Errorf("Content = %q, want %q", out.Content, "Hello world.")
	}
	if out.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want done", out.ExitReason)
	}
	if out.Confidence != 0.9 {
		t.Errorf("Confidence = %v, want 0.9", out.Confidence)
	}
	if out.Classification != "ok" {
		t.Errorf("Classification = %q, want ok", out.Classification)
	}
	if len(out.Signals) != 1 || out.Signals[0] != "s1" {
		t.Errorf("Signals = %v, want [s1]", out.Signals)
	}
	if got := f.postRuns.Load(); got != 1 {
		t.Errorf("POST /v1/runs called %d times, want 1", got)
	}
	if got := f.getEvts.Load(); got != 1 {
		t.Errorf("GET events called %d times, want 1", got)
	}
	// Body shape: session_id == envelope ID, instructions == OutputSchema, model == ep.Model.
	f.mu.Lock()
	body := f.lastBody
	f.mu.Unlock()
	if body["session_id"] != "sess-1" {
		t.Errorf("session_id = %v, want sess-1", body["session_id"])
	}
	// Instructions get the structured preamble prepended; the
	// envelope's OutputSchema is the trailing schema directive.
	instr, _ := body["instructions"].(string)
	if !strings.Contains(instr, "JSON object") || !strings.Contains(instr, "free-text") {
		t.Errorf("instructions missing preamble or schema: %q", instr)
	}
	if body["model"] != "test-model" {
		t.Errorf("model = %v, want test-model", body["model"])
	}
	if body["input"] != "say hi" {
		t.Errorf("input = %v, want 'say hi'", body["input"])
	}

	// Cost ledger captured the usage under the endpoint's model.
	summary := tracker.Summary()
	if len(summary.Entries) != 1 {
		t.Fatalf("expected 1 cost entry, got %d", len(summary.Entries))
	}
	if summary.Entries[0].TokensInput != 12 || summary.Entries[0].TokensOutput != 3 {
		t.Errorf("cost entry tokens = (%d,%d), want (12,3)",
			summary.Entries[0].TokensInput, summary.Entries[0].TokensOutput)
	}
}

func TestCallRunFailedSurfacesError(t *testing.T) {
	f := newFake(t)
	f.setEvents(
		`{"event":"run.failed","run_id":"run_fake","error":"model 401"}`,
	)
	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainReasoning, "sess-2", "fail"))
	if err == nil || !strings.Contains(err.Error(), "model 401") {
		t.Fatalf("expected run.failed error, got %v", err)
	}
}

func TestCallApprovalRequestRejected(t *testing.T) {
	f := newFake(t)
	f.setEvents(
		`{"event":"approval.request","run_id":"run_fake","tool":"shell"}`,
	)
	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainReasoning, "sess-3", "rm -rf /"))
	if err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("expected approval-required error, got %v", err)
	}
}

func TestCallStreamEndsWithoutTerminalEvent(t *testing.T) {
	f := newFake(t)
	// Only deltas, no run.completed — server closes cleanly without a
	// terminal. Adapter must treat this as failure rather than
	// returning a confidently-empty Output.
	f.setEvents(
		`{"event":"message.delta","run_id":"run_fake","delta":"partial"}`,
	)
	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainReasoning, "sess-4", "x"))
	if err == nil || !strings.Contains(err.Error(), "terminal event") {
		t.Fatalf("expected terminal-event error, got %v", err)
	}
}

func TestCallUnknownBrainFunctionErrors(t *testing.T) {
	f := newFake(t)
	a, err := New(Config{
		Endpoints: map[session.BrainFunction]Endpoint{
			session.BrainReasoning: {BaseURL: f.server.URL},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainCritic, "sess-5", "x"))
	if err == nil || !strings.Contains(err.Error(), "no endpoint registered") {
		t.Fatalf("expected unregistered-brain-function error, got %v", err)
	}
}

func TestCallPostRunsServerErrorPropagates(t *testing.T) {
	f := newFake(t)
	f.mu.Lock()
	f.runStatus = http.StatusInternalServerError
	f.runRespBody = "boom"
	f.mu.Unlock()
	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainReasoning, "sess-6", "x"))
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 to surface, got %v", err)
	}
}

func TestContextCancellationStopsRun(t *testing.T) {
	f := newFake(t)
	// No events queued — the SSE handler will write the sentinel close
	// after no events, so the stream closes quickly. Cancel before
	// dispatch to exercise the cancellation path.
	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = a.Call(ctx, envFor(session.BrainReasoning, "sess-7", "x"))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestStructuredFromFencedBlock(t *testing.T) {
	f := newFake(t)
	// Hermes returns prose + a fenced JSON block; adapter pulls the
	// block out and falls the prose into Content (since the JSON's
	// Content is empty).
	raw := "Sure, here's the answer:\n\n```json\n{\"content\":\"\",\"confidence\":0.7,\"sub_aps\":[{\"brain_function\":\"critic\",\"input\":\"check it\",\"output_schema\":\"ok\"}]}\n```\n\nLet me know if you want more.\n"
	completed := fmt.Sprintf(`{"event":"run.completed","run_id":"run_fake","output":%q,"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, raw)
	f.setEvents(completed)

	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := a.Call(context.Background(), envFor(session.BrainReasoning, "sess-fenced", "x"))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7", out.Confidence)
	}
	if len(out.SubAPs) != 1 || out.SubAPs[0].BrainFunction != session.BrainCritic {
		t.Errorf("SubAPs = %+v, want one critic", out.SubAPs)
	}
	if !strings.Contains(out.Content, "Sure, here's the answer") {
		t.Errorf("Content should fall back to surrounding prose: %q", out.Content)
	}
}

func TestUnstructuredFallback(t *testing.T) {
	f := newFake(t)
	completed := `{"event":"run.completed","run_id":"run_fake","output":"just text","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`
	f.setEvents(completed)

	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out, err := a.Call(context.Background(), envFor(session.BrainReasoning, "sess-unstructured", "x"))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Content != "just text" {
		t.Errorf("Content = %q, want raw fallback", out.Content)
	}
	if out.Confidence != 0 {
		t.Errorf("Confidence = %v, want zero for unstructured", out.Confidence)
	}
}

func TestRequireStructuredErrorsOnFallback(t *testing.T) {
	f := newFake(t)
	completed := `{"event":"run.completed","run_id":"run_fake","output":"plain prose, no envelope","usage":{}}`
	f.setEvents(completed)

	cfg := SingleEndpoint(f.server.URL, "")
	cfg.RequireStructured = true
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainReasoning, "sess-strict", "x"))
	if err == nil || !strings.Contains(err.Error(), "no structured envelope") {
		t.Fatalf("expected structured-required error, got %v", err)
	}
}

func TestMalformedJSONBlockSurfacesParseError(t *testing.T) {
	f := newFake(t)
	raw := "```json\n{not valid json}\n```"
	completed := fmt.Sprintf(`{"event":"run.completed","run_id":"run_fake","output":%q,"usage":{}}`, raw)
	f.setEvents(completed)

	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Call(context.Background(), envFor(session.BrainReasoning, "sess-bad-json", "x"))
	if err == nil || !strings.Contains(err.Error(), "parse fenced JSON") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// Smoke check: cost tracker stays untouched when not configured.
func TestCallNilCostTrackerNoOp(t *testing.T) {
	f := newFake(t)
	f.setEvents(
		`{"event":"run.completed","run_id":"run_fake","output":"ok","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
	)
	a, err := New(SingleEndpoint(f.server.URL, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.Call(context.Background(), envFor(session.BrainReasoning, "sess-8", "x")); err != nil {
		t.Fatalf("Call: %v", err)
	}
}

