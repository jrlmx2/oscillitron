// CLAUDE GENERATED
package recomposer

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

func anthropicMockServer(t *testing.T, replyText string) (*httptest.Server, *string) {
	t.Helper()
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_x", "model": "claude-sonnet-4-6", "role": "assistant", "type": "message",
			"content": []map[string]any{{"type": "text", "text": replyText}},
			"usage":   map[string]int{"input_tokens": 100, "output_tokens": 80},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody
}

func TestAnthropicSynthesizer_ParsesContentAndConfidence(t *testing.T) {
	srv, _ := anthropicMockServer(t,
		`{"content": "integrated paragraph combining left and right", "confidence": 0.85}`)
	s, err := NewAnthropic(AnthropicConfig{APIKey: "sk-test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	resp, err := s.Synthesize(context.Background(), SynthesizeRequest{
		Left:          session.ReturnResultPayload{Result: session.Payload{Content: "L"}, Confidence: 0.9},
		Right:         session.ReturnResultPayload{Result: session.Payload{Content: "R"}, Confidence: 0.7},
		RecomposeSpec: session.RecomposeSequential,
		StepIndex:     0,
	})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if resp.Content != "integrated paragraph combining left and right" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", resp.Confidence)
	}
	if resp.TokensUsed != 180 {
		t.Errorf("TokensUsed = %d, want 180", resp.TokensUsed)
	}
}

func TestAnthropicSynthesizer_ClampsConfidence(t *testing.T) {
	// Model returns 2.0 (out of range) — clamp to 1.0.
	srv, _ := anthropicMockServer(t, `{"content":"x","confidence":2.0}`)
	s, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	resp, _ := s.Synthesize(context.Background(), SynthesizeRequest{
		Left:  session.ReturnResultPayload{Result: session.Payload{Content: "L"}},
		Right: session.ReturnResultPayload{Result: session.Payload{Content: "R"}},
	})
	if resp.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want clamped to 1.0", resp.Confidence)
	}
	// And negative → 0.
	srv2, _ := anthropicMockServer(t, `{"content":"x","confidence":-0.5}`)
	s2, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv2.URL})
	resp2, _ := s2.Synthesize(context.Background(), SynthesizeRequest{
		Left:  session.ReturnResultPayload{Result: session.Payload{Content: "L"}},
		Right: session.ReturnResultPayload{Result: session.Payload{Content: "R"}},
	})
	if resp2.Confidence != 0 {
		t.Errorf("Confidence = %v, want clamped to 0", resp2.Confidence)
	}
}

func TestAnthropicSynthesizer_ErrorsOnEmptyContent(t *testing.T) {
	srv, _ := anthropicMockServer(t, `{"content":"","confidence":0.9}`)
	s, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	_, err := s.Synthesize(context.Background(), SynthesizeRequest{
		Left: session.ReturnResultPayload{Result: session.Payload{Content: "L"}},
		Right: session.ReturnResultPayload{Result: session.Payload{Content: "R"}},
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestAnthropicSynthesizer_SendsBothPayloadsInUserMessage(t *testing.T) {
	srv, lastBody := anthropicMockServer(t, `{"content":"combined","confidence":0.8}`)
	s, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	_, _ = s.Synthesize(context.Background(), SynthesizeRequest{
		Left:          session.ReturnResultPayload{Result: session.Payload{Content: "alpha-content"}},
		Right:         session.ReturnResultPayload{Result: session.Payload{Content: "beta-content"}},
		RecomposeSpec: session.RecomposePairwise,
		StepIndex:     2,
	})
	for _, want := range []string{"alpha-content", "beta-content", "pairwise", "step 2"} {
		if !strings.Contains(*lastBody, want) {
			t.Errorf("user message missing %q", want)
		}
	}
}

func TestAnthropicSynthesizer_PluggableIntoSynthRecomposer(t *testing.T) {
	// The Anthropic synthesizer is meant to plug into Synth alongside
	// any other Synthesizer implementation. Verify end-to-end through
	// pkg/recomposer.Synth using a sequential fold.
	srv, _ := anthropicMockServer(t, `{"content":"merged","confidence":0.9}`)
	s, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	rec := Synth{Synthesizer: s}
	got, err := rec.Recompose(context.Background(), session.RecomposeSequential,
		[]session.ReturnResultPayload{
			{Result: session.Payload{Content: "A"}, Confidence: 0.8},
			{Result: session.Payload{Content: "B"}, Confidence: 0.7},
			{Result: session.Payload{Content: "C"}, Confidence: 0.95},
		})
	if err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	// Last reducer's content wins through the fold; with the mock
	// always returning "merged", the final content is "merged".
	if got.Result.Content != "merged" {
		t.Errorf("content = %q, want merged", got.Result.Content)
	}
}
