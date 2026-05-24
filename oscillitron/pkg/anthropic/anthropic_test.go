package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New with empty APIKey should error")
	}
}

func TestNew_DefaultsBaseURLAndVersion(t *testing.T) {
	c, err := New(Config{APIKey: "sk-x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.cfg.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.cfg.BaseURL, DefaultBaseURL)
	}
	if c.cfg.APIVersion != DefaultAPIVersion {
		t.Errorf("APIVersion = %q, want %q", c.cfg.APIVersion, DefaultAPIVersion)
	}
}

func TestMessages_RoundTrip(t *testing.T) {
	var seen struct {
		method     string
		path       string
		apiKey     string
		apiVersion string
		body       MessagesRequest
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method = r.Method
		seen.path = r.URL.Path
		seen.apiKey = r.Header.Get("x-api-key")
		seen.apiVersion = r.Header.Get("anthropic-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_abc",
			"model": "claude-haiku-4-5-20251001",
			"role": "assistant",
			"type": "message",
			"content": [{"type": "text", "text": "hello world"}],
			"usage": {"input_tokens": 12, "output_tokens": 3}
		}`))
	}))
	defer srv.Close()

	c, _ := New(Config{APIKey: "sk-test", BaseURL: srv.URL})
	resp, err := c.Messages(context.Background(), MessagesRequest{
		Model:     "claude-haiku-4-5-20251001",
		MaxTokens: 256,
		System:    "be brief",
		Messages: []Message{
			{Role: "user", Content: "say hello"},
		},
	})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if seen.method != http.MethodPost || seen.path != "/v1/messages" {
		t.Errorf("method/path = %s %s, want POST /v1/messages", seen.method, seen.path)
	}
	if seen.apiKey != "sk-test" {
		t.Errorf("x-api-key = %q, want sk-test", seen.apiKey)
	}
	if seen.apiVersion != DefaultAPIVersion {
		t.Errorf("anthropic-version = %q, want %q", seen.apiVersion, DefaultAPIVersion)
	}
	if seen.body.System != "be brief" {
		t.Errorf("system = %q, want %q", seen.body.System, "be brief")
	}
	if resp.FirstText() != "hello world" {
		t.Errorf("FirstText() = %q, want %q", resp.FirstText(), "hello world")
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want input=12 output=3", resp.Usage)
	}
}

func TestMessages_NonSuccessReturnsErrorWithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer srv.Close()
	c, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	_, err := c.Messages(context.Background(), MessagesRequest{
		Model: "m", MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("error %q should include response body", err.Error())
	}
}

func TestMessages_ValidatesRequiredFields(t *testing.T) {
	c, _ := New(Config{APIKey: "sk-x"})
	cases := []MessagesRequest{
		{MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}}}, // no Model
		{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}},    // no MaxTokens
		{Model: "m", MaxTokens: 10},                                        // no Messages
	}
	for i, req := range cases {
		if _, err := c.Messages(context.Background(), req); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestMessages_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := New(Config{APIKey: "sk-x", BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Messages(ctx, MessagesRequest{Model: "m", MaxTokens: 10, Messages: []Message{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
