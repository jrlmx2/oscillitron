package grader

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mockGrader(t *testing.T, reply string) (*httptest.Server, *string) {
	t.Helper()
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_g", "model": "claude-sonnet-4-6", "role": "assistant", "type": "message",
			"content": []map[string]any{{"type": "text", "text": reply}},
			"usage":   map[string]int{"input_tokens": 200, "output_tokens": 50},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &lastBody
}

func TestAnthropicGrader_ParsesAllFourDimensions(t *testing.T) {
	srv, _ := mockGrader(t,
		`{"relevance":4,"tone":5,"completeness":3,"professionalism":4,"notes":"clear and polite"}`)
	g, err := NewAnthropic(AnthropicConfig{APIKey: "sk-test", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewAnthropic: %v", err)
	}
	res, err := g.Grade(context.Background(), Request{
		Task:      "decline politely",
		Candidate: "Thanks but no thanks.",
	})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if res.Score.Relevance != 4 || res.Score.Tone != 5 || res.Score.Completeness != 3 || res.Score.Professionalism != 4 {
		t.Errorf("scores wrong: %+v", res.Score)
	}
	if res.Score.Notes != "clear and polite" {
		t.Errorf("notes = %q", res.Score.Notes)
	}
	if res.Score.Total() != 16 {
		t.Errorf("Total() = %d, want 16", res.Score.Total())
	}
	if res.TokensUsed != 250 {
		t.Errorf("TokensUsed = %d, want 250", res.TokensUsed)
	}
}

func TestAnthropicGrader_RejectsOutOfRangeScores(t *testing.T) {
	srv, _ := mockGrader(t,
		`{"relevance":4,"tone":7,"completeness":3,"professionalism":4}`) // 7 invalid
	g, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	_, err := g.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err == nil {
		t.Fatal("expected error on out-of-range score")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error %q should mention range", err.Error())
	}
}

func TestAnthropicGrader_TolerantsMarkdownFences(t *testing.T) {
	srv, _ := mockGrader(t,
		"```json\n{\"relevance\":3,\"tone\":3,\"completeness\":3,\"professionalism\":3}\n```")
	g, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	res, err := g.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if res.Score.Total() != 12 {
		t.Errorf("Total = %d, want 12", res.Score.Total())
	}
}

func TestAnthropicGrader_SendsBothTaskAndCandidate(t *testing.T) {
	srv, lastBody := mockGrader(t, `{"relevance":3,"tone":3,"completeness":3,"professionalism":3}`)
	g, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	_, err := g.Grade(context.Background(), Request{
		Task:      "task-content-here",
		Candidate: "candidate-content-here",
		Notes:     "case-id=test",
	})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	for _, want := range []string{"task-content-here", "candidate-content-here", "case-id=test"} {
		if !strings.Contains(*lastBody, want) {
			t.Errorf("request body missing %q", want)
		}
	}
}

func TestAnthropicGrader_ErrorOnUnparseableResponse(t *testing.T) {
	srv, _ := mockGrader(t, "this is not JSON at all")
	g, _ := NewAnthropic(AnthropicConfig{APIKey: "sk-x", BaseURL: srv.URL})
	_, err := g.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err == nil {
		t.Fatal("expected error on unparseable response")
	}
}

func TestScore_TotalSums(t *testing.T) {
	s := Score{Relevance: 5, Tone: 4, Completeness: 3, Professionalism: 2}
	if s.Total() != 14 {
		t.Errorf("Total = %d, want 14", s.Total())
	}
}
