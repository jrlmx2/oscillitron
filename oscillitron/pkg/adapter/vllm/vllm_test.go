package vllm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// fakeVLLM is a minimal httptest server that mimics vLLM's
// OpenAI-compatible /v1/chat/completions endpoint. The script lets
// each test queue a sequence of responses; the server pops one per
// request in arrival order.
type fakeVLLM struct {
	server *httptest.Server
	mu     sync.Mutex
	script []scriptedResponse
	calls  []capturedCall
}

type scriptedResponse struct {
	status       int
	content      string
	finishReason string
	tokensIn     int
	tokensOut    int
}

type capturedCall struct {
	model      string
	systemMsg  string
	userMsg    string
	rawRequest map[string]any
}

func newFakeVLLM() *fakeVLLM {
	f := &fakeVLLM{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		var sys, usr string
		if msgs, ok := parsed["messages"].([]any); ok {
			for _, m := range msgs {
				if mm, ok := m.(map[string]any); ok {
					switch mm["role"] {
					case "system":
						sys, _ = mm["content"].(string)
					case "user":
						usr, _ = mm["content"].(string)
					}
				}
			}
		}
		f.mu.Lock()
		mod, _ := parsed["model"].(string)
		f.calls = append(f.calls, capturedCall{model: mod, systemMsg: sys, userMsg: usr, rawRequest: parsed})
		if len(f.script) == 0 {
			f.mu.Unlock()
			http.Error(w, "fakeVLLM: no scripted response left", http.StatusInternalServerError)
			return
		}
		next := f.script[0]
		f.script = f.script[1:]
		f.mu.Unlock()
		w.WriteHeader(next.status)
		if next.status != http.StatusOK {
			w.Write([]byte(next.content))
			return
		}
		resp := map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": next.content},
				"finish_reason": next.finishReason,
			}},
			"usage": map[string]any{
				"prompt_tokens":     next.tokensIn,
				"completion_tokens": next.tokensOut,
				"total_tokens":      next.tokensIn + next.tokensOut,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return f
}

func (f *fakeVLLM) queue(r scriptedResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.script = append(f.script, r)
}

func (f *fakeVLLM) snapshotCalls() []capturedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capturedCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeVLLM) close() { f.server.Close() }

func newAdapter(t *testing.T, url string, opts ...func(*Config)) *Adapter {
	t.Helper()
	cfg := SingleEndpoint(url, "meta-llama/Llama-3.3-70B-Instruct")
	for _, o := range opts {
		o(&cfg)
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// --- Config validation ---

func TestNew_RejectsEmptyBaseURL(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error for missing EvaluateEndpoint.BaseURL")
	}
}

func TestNew_RejectsEmptyModel(t *testing.T) {
	_, err := New(Config{EvaluateEndpoint: Endpoint{BaseURL: "http://x"}, ExecuteEndpoints: map[session.Playbook]Endpoint{session.PlaybookProcess: {BaseURL: "http://x", Model: "m"}}})
	if err == nil {
		t.Fatal("expected error for missing EvaluateEndpoint.Model")
	}
}

func TestNew_RejectsEmptyExecuteEndpoints(t *testing.T) {
	_, err := New(Config{EvaluateEndpoint: Endpoint{BaseURL: "http://x", Model: "m"}})
	if err == nil {
		t.Fatal("expected error for missing ExecuteEndpoints")
	}
}

func TestSingleEndpoint_WiresEveryPlaybook(t *testing.T) {
	cfg := SingleEndpoint("http://x", "m")
	for _, pb := range AllPlaybooks {
		if _, ok := cfg.ExecuteEndpoints[pb]; !ok {
			t.Errorf("SingleEndpoint missing playbook %q", pb)
		}
	}
}

func TestMultiEndpoint_ErrorsOnMissingPlaybook(t *testing.T) {
	_, err := MultiEndpoint(Endpoint{BaseURL: "http://x", Model: "m"}, map[session.Playbook]Endpoint{
		session.PlaybookProcess: {BaseURL: "http://x", Model: "m"},
	})
	if err == nil {
		t.Fatal("expected error for missing playbook endpoints")
	}
}

// --- Evaluate ---

func TestEvaluate_HappyPath(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      `{"playbook":"process","rationale":"single q","confidence":0.92}`,
		finishReason: "stop",
		tokensIn:     120,
		tokensOut:    24,
	})
	a := newAdapter(t, f.server.URL)
	env := session.Envelope{ID: "ap-1", Input: session.Payload{Kind: "task", Content: "what is 2+2?"}}
	got, err := a.Evaluate(context.Background(), env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.Evaluate == nil {
		t.Fatal("env.Evaluate not populated")
	}
	if got.Evaluate.Playbook != session.PlaybookProcess {
		t.Errorf("Playbook = %q, want process", got.Evaluate.Playbook)
	}
	if got.Evaluate.Confidence < 0.9 {
		t.Errorf("Confidence = %v, want >= 0.9", got.Evaluate.Confidence)
	}
	if got.Evaluate.TokensUsed != 144 {
		t.Errorf("TokensUsed = %d, want 144", got.Evaluate.TokensUsed)
	}

	// Inspect the request actually sent.
	calls := f.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].userMsg != "what is 2+2?" {
		t.Errorf("user message not propagated: %q", calls[0].userMsg)
	}
	if !strings.Contains(calls[0].systemMsg, "routing classifier") {
		t.Errorf("system message missing evaluate preamble: %q", calls[0].systemMsg)
	}
	if calls[0].model != "meta-llama/Llama-3.3-70B-Instruct" {
		t.Errorf("model not propagated: %q", calls[0].model)
	}
}

func TestEvaluate_RejectsUnknownPlaybook(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      `{"playbook":"nonsense","confidence":0.5}`,
		finishReason: "stop",
	})
	a := newAdapter(t, f.server.URL)
	_, err := a.Evaluate(context.Background(), session.Envelope{ID: "ap-1"})
	if err == nil || !strings.Contains(err.Error(), "unknown playbook") {
		t.Fatalf("expected unknown-playbook error, got %v", err)
	}
}

func TestEvaluate_UnstructuredFallback(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      "the answer is to call process",
		finishReason: "stop",
	})
	a := newAdapter(t, f.server.URL)
	got, err := a.Evaluate(context.Background(), session.Envelope{ID: "ap-1"})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.Evaluate.Playbook != session.PlaybookProcess {
		t.Errorf("fallback should pick process; got %q", got.Evaluate.Playbook)
	}
	if got.Evaluate.Confidence > 0.2 {
		t.Errorf("fallback Confidence should be low; got %v", got.Evaluate.Confidence)
	}
}

func TestEvaluate_RequireStructuredRejectsUnstructured(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      "no json here",
		finishReason: "length",
	})
	a := newAdapter(t, f.server.URL, func(c *Config) { c.RequireStructured = true })
	_, err := a.Evaluate(context.Background(), session.Envelope{ID: "ap-1"})
	if err == nil || !strings.Contains(err.Error(), "no structured envelope") {
		t.Fatalf("expected structured-required error, got %v", err)
	}
}

func TestEvaluate_PropagatesHTTPError(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{status: http.StatusBadGateway, content: "upstream down"})
	a := newAdapter(t, f.server.URL)
	_, err := a.Evaluate(context.Background(), session.Envelope{ID: "ap-1"})
	if err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("expected status 502 error, got %v", err)
	}
}

// --- Execute ---

func TestExecute_ProcessHappyPath(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      `{"content":"4","confidence":0.98}`,
		finishReason: "stop",
		tokensIn:     200,
		tokensOut:    8,
	})
	a := newAdapter(t, f.server.URL)
	env := session.Envelope{
		ID:       "ap-1",
		Input:    session.Payload{Kind: "task", Content: "what is 2+2?"},
		Evaluate: &session.Evaluate{Playbook: session.PlaybookProcess},
	}
	got, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Execute == nil || got.Execute.ReturnResult == nil {
		t.Fatal("env.Execute.ReturnResult not populated")
	}
	if got.Execute.ReturnResult.Result.Content != "4" {
		t.Errorf("Result.Content = %q, want '4'", got.Execute.ReturnResult.Result.Content)
	}
	if got.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want done", got.ExitReason)
	}
}

func TestExecute_CalledBeforeEvaluate(t *testing.T) {
	a := newAdapter(t, "http://localhost") // server URL unused — adapter rejects before HTTP
	_, err := a.Execute(context.Background(), session.Envelope{ID: "ap-1"})
	if err == nil || !strings.Contains(err.Error(), "before Evaluate") {
		t.Fatalf("expected before-Evaluate error, got %v", err)
	}
}

func TestExecute_PlanReturnsEmitSubtree(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status: http.StatusOK,
		content: `{
		  "sub_aps": [{"input_kind":"task","input":"step 1","output_schema":"","classification":"","needs_verification":false}],
		  "recompose": "sequential"
		}`,
		finishReason: "stop",
	})
	a := newAdapter(t, f.server.URL)
	env := session.Envelope{
		ID:       "ap-2",
		Input:    session.Payload{Kind: "task", Content: "do the thing"},
		Evaluate: &session.Evaluate{Playbook: session.PlaybookPlan},
	}
	got, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Execute.Category != session.CategoryEmitSubtree {
		t.Errorf("Category = %q, want emit_subtree", got.Execute.Category)
	}
	if got.Execute.EmitSubtree == nil || len(got.Execute.EmitSubtree.SubAPs) != 1 {
		t.Errorf("expected 1 sub-AP; got %+v", got.Execute.EmitSubtree)
	}
	if got.Execute.EmitSubtree.Recompose != session.RecomposeSequential {
		t.Errorf("Recompose = %q, want sequential", got.Execute.EmitSubtree.Recompose)
	}
}

func TestExecute_CritiqueReturnsVerifierSignal(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      `{"verdict":"pass","issues":[]}`,
		finishReason: "stop",
	})
	a := newAdapter(t, f.server.URL)
	env := session.Envelope{
		ID:       "ap-3",
		Evaluate: &session.Evaluate{Playbook: session.PlaybookCritique},
	}
	got, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Execute.Category != session.CategoryVerifierSignal {
		t.Errorf("Category = %q, want verifier_signal", got.Execute.Category)
	}
	if got.Execute.VerifierSignal.Verdict != session.VerdictPass {
		t.Errorf("Verdict = %q, want pass", got.Execute.VerifierSignal.Verdict)
	}
}

func TestExecute_UnstructuredFallback(t *testing.T) {
	f := newFakeVLLM()
	defer f.close()
	f.queue(scriptedResponse{
		status:       http.StatusOK,
		content:      "the answer is 4",
		finishReason: "stop",
	})
	a := newAdapter(t, f.server.URL)
	env := session.Envelope{
		ID:       "ap-4",
		Evaluate: &session.Evaluate{Playbook: session.PlaybookProcess},
	}
	got, err := a.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Execute.ReturnResult == nil {
		t.Fatal("expected fallback ReturnResult")
	}
	if !strings.Contains(got.Execute.ReturnResult.Result.Content, "the answer is 4") {
		t.Errorf("fallback Result.Content = %q", got.Execute.ReturnResult.Result.Content)
	}
}

// --- Misc ---

func TestName(t *testing.T) {
	a := newAdapter(t, "http://x")
	if a.Name() != "vllm-direct" {
		t.Errorf("Name = %q, want vllm-direct", a.Name())
	}
}

// errClient lets us assert that HTTP errors propagate.
type errClient struct{ err error }

func (e errClient) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

func TestEvaluate_PropagatesTransportError(t *testing.T) {
	want := errors.New("network down")
	a := newAdapter(t, "http://x", func(c *Config) { c.HTTPClient = &http.Client{Transport: errClient{err: want}} })
	_, err := a.Evaluate(context.Background(), session.Envelope{ID: "ap-1"})
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected transport error to propagate, got %v", err)
	}
}
