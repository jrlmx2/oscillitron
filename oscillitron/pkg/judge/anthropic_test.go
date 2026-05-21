// CLAUDE GENERATED
package judge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// anthropicMockServer returns an httptest server that responds with
// the supplied text wrapped in a Messages API response envelope.
func anthropicMockServer(t *testing.T, replyText string) (*httptest.Server, *string) {
	t.Helper()
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"id":      "msg_test",
			"model":   "claude-haiku-4-5-20251001",
			"role":    "assistant",
			"type":    "message",
			"content": []map[string]any{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": 50, "output_tokens": 20},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody
}

func TestAnthropicJudge_ParsesStructuredVerdict(t *testing.T) {
	srv, _ := anthropicMockServer(t, `{"verdict":"pass","issues":[]}`)
	j, err := NewAnthropic(AnthropicConfig{APIKey: "sk-test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	target := session.Envelope{
		ID:    "ap-target",
		Input: session.Payload{Content: "what is 2+2"},
		Execute: &session.Execute{
			Category:     session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{Result: session.Payload{Content: "4"}},
		},
	}
	resp, err := j.Judge(context.Background(), Request{
		Target:       target,
		LocalVerdict: session.VerdictPass,
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if resp.Verdict != session.VerdictPass {
		t.Errorf("Verdict = %q, want pass", resp.Verdict)
	}
	if resp.TokensUsed != 70 {
		t.Errorf("TokensUsed = %d, want 70 (50+20)", resp.TokensUsed)
	}
}

func TestAnthropicJudge_ParsesIssuesArray(t *testing.T) {
	srv, _ := anthropicMockServer(t,
		`{"verdict":"issues","issues":[{"severity":"warning","what":"computation looks off"},{"severity":"error","what":"wrong"}]}`)
	j, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	resp, err := j.Judge(context.Background(), Request{Target: session.Envelope{}, LocalVerdict: session.VerdictPass})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if resp.Verdict != session.VerdictIssues {
		t.Errorf("Verdict = %q, want issues", resp.Verdict)
	}
	if len(resp.Issues) != 2 {
		t.Fatalf("Issues = %d, want 2", len(resp.Issues))
	}
	if resp.Issues[0].Severity != session.SeverityWarning {
		t.Errorf("issue[0].Severity = %q, want warning", resp.Issues[0].Severity)
	}
	if resp.Issues[1].Severity != session.SeverityError {
		t.Errorf("issue[1].Severity = %q, want error", resp.Issues[1].Severity)
	}
}

func TestAnthropicJudge_TolerantsMarkdownFences(t *testing.T) {
	// Model wraps JSON in markdown fences. extractFirstJSONObject
	// should still find the object.
	srv, _ := anthropicMockServer(t,
		"Sure, here is my verdict:\n```json\n{\"verdict\": \"fail\", \"issues\": []}\n```")
	j, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	resp, err := j.Judge(context.Background(), Request{Target: session.Envelope{}, LocalVerdict: session.VerdictPass})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if resp.Verdict != session.VerdictFail {
		t.Errorf("Verdict = %q, want fail", resp.Verdict)
	}
}

func TestAnthropicJudge_ErrorsOnUnparseableResponse(t *testing.T) {
	srv, _ := anthropicMockServer(t, "totally not JSON at all, just prose")
	j, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	_, err := j.Judge(context.Background(), Request{Target: session.Envelope{}, LocalVerdict: session.VerdictPass})
	if err == nil {
		t.Fatal("expected error on unparseable response")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error %q should mention parse", err.Error())
	}
}

func TestAnthropicJudge_SendsLocalContextInUserMessage(t *testing.T) {
	srv, lastBody := anthropicMockServer(t, `{"verdict":"pass"}`)
	j, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	target := session.Envelope{
		Input: session.Payload{Content: "user-task-content"},
		Execute: &session.Execute{
			Category:     session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{Result: session.Payload{Content: "ap-result-content"}},
		},
	}
	_, err := j.Judge(context.Background(), Request{
		Target:       target,
		LocalVerdict: session.VerdictIssues,
		LocalIssues:  []session.Issue{{Severity: session.SeverityWarning, What: "looks fishy"}},
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	for _, want := range []string{"user-task-content", "ap-result-content", "issues", "looks fishy"} {
		if !strings.Contains(*lastBody, want) {
			t.Errorf("user message missing %q; body=%s", want, *lastBody)
		}
	}
}

func TestExtractFirstJSONObject(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"a":1}`},
		{"prose before {\"a\":1} prose after", `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{`{"nested": {"b": 2}, "c": 3}`, `{"nested": {"b": 2}, "c": 3}`},
		{`no braces here`, ""},
		{`{"unbalanced": `, ""},
		{`{"with-string": "}"}`, `{"with-string": "}"}`},
		{`{"escape": "\""}`, `{"escape": "\""}`},
	}
	for _, c := range cases {
		got := extractFirstJSONObject(c.in)
		if got != c.want {
			t.Errorf("input %q: got %q, want %q", c.in, got, c.want)
		}
	}
}
