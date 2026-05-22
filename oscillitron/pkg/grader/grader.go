// CLAUDE GENERATED
// Package grader implements LLM-as-judge quality grading for Phase 1
// kill-or-proceed measurement. Given the original task input and a
// candidate output, an AnthropicGrader scores the candidate against a
// fixed rubric (relevance, tone, completeness, professionalism) plus
// free-form notes.
//
// The grader is the *measurement* layer — it is not part of the
// runtime call tree. Phase 1 grades both the orchestrator's output
// and the frontier baseline's output with the same grader so the
// quality numbers are directly comparable.
//
// Defaults to Sonnet (claude-sonnet-4-6) so the judge is stronger than
// either candidate. Override via Config.Model if you want to grade
// against a different model — e.g. Opus for the highest-fidelity
// judgment at the cost of slower / more expensive grading.
package grader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/anthropic"
)

// DefaultModel is the model the grader uses by default. Sonnet is the
// sweet spot — strong enough to judge reliably, cheap enough to grade
// dozens of candidates per dollar.
const DefaultModel = "claude-sonnet-4-6"

// DefaultMaxTokens caps the grader's reply. The rubric output is small;
// 1024 is plenty.
const DefaultMaxTokens = 1024

// Score is one rubric dimension. 1-5 scale; 1 = unacceptable, 3 = OK,
// 5 = excellent. Aggregated into Total downstream.
type Score struct {
	Relevance       int    `json:"relevance"`
	Tone            int    `json:"tone"`
	Completeness    int    `json:"completeness"`
	Professionalism int    `json:"professionalism"`
	Notes           string `json:"notes,omitempty"`
}

// Total returns the sum of the four rubric dimensions. Max = 20.
func (s Score) Total() int {
	return s.Relevance + s.Tone + s.Completeness + s.Professionalism
}

// Result is a graded candidate with its scoring metadata.
type Result struct {
	Score      Score
	TokensUsed int    // grader-side cost (not the candidate's cost)
	Model      string // grader model id, for trace records
	Raw        string // the model's raw reply (debug)
}

// Request is one grading request.
type Request struct {
	// Task is the original input the candidate was generated from.
	// For email drafting, this includes the original email + the
	// user's intent.
	Task string
	// Candidate is the output to grade.
	Candidate string
	// Notes is optional extra context for the grader (e.g. workload
	// metadata, "case-id=001"). Riding on the request rather than as
	// a separate trace channel keeps grading reproducible.
	Notes string
}

// Grader is the interface. Implementations are expected to be safe
// for concurrent use; Phase 1's measurement driver runs grading in
// parallel across cases.
type Grader interface {
	Name() string
	Grade(ctx context.Context, req Request) (Result, error)
}

// --- Anthropic-backed implementation ---

// AnthropicGrader uses pkg/anthropic to call Claude with a rubric
// prompt. The candidate is judged via a structured-output JSON shape;
// markdown-fenced JSON is tolerated.
type AnthropicGrader struct {
	client       *anthropic.Client
	model        string
	maxTokens    int
	systemPrompt string
}

// AnthropicConfig configures a new AnthropicGrader.
type AnthropicConfig struct {
	APIKey       string
	BaseURL      string       // optional; tests pass an httptest server URL
	Model        string       // optional; defaults to DefaultModel
	MaxTokens    int          // optional; defaults to DefaultMaxTokens
	SystemPrompt string       // optional; overrides the built-in rubric preamble
	HTTPClient   *http.Client // optional; forwarded to anthropic.Client
}

// NewAnthropic constructs an AnthropicGrader. APIKey is required.
func NewAnthropic(cfg AnthropicConfig) (*AnthropicGrader, error) {
	client, err := anthropic.New(anthropic.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("grader: %w", err)
	}
	model := cfg.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}
	return &AnthropicGrader{
		client:       client,
		model:        model,
		maxTokens:    maxTokens,
		systemPrompt: systemPrompt,
	}, nil
}

// Name implements Grader.
func (g *AnthropicGrader) Name() string { return "anthropic-" + g.model }

const defaultSystemPrompt = `You are a strict quality grader for email drafts. ` +
	`You receive (a) the original email thread + the user's intent, and (b) a candidate draft response. ` +
	`Score the candidate on a 1-5 scale across four dimensions:` + "\n" +
	`- relevance: does the draft actually address what the user asked for?` + "\n" +
	`- tone: does it match the requested tone (formal/casual/firm/polite/etc.)?` + "\n" +
	`- completeness: does it include greeting, body, close, and any required elements?` + "\n" +
	`- professionalism: free of errors, hedging, over-apology, generic filler.` + "\n" +
	`1 = unacceptable, 3 = OK, 5 = excellent. Be calibrated — most "OK" drafts should land at 3-4. ` +
	`Reply with a single JSON object — no prose, no markdown:` + "\n" +
	`{"relevance": <1-5>, "tone": <1-5>, "completeness": <1-5>, "professionalism": <1-5>, "notes": "<one-sentence justification>"}`

// Grade implements Grader.
func (g *AnthropicGrader) Grade(ctx context.Context, req Request) (Result, error) {
	user := renderUserMessage(req)
	resp, err := g.client.Messages(ctx, anthropic.MessagesRequest{
		Model:     g.model,
		MaxTokens: g.maxTokens,
		System:    g.systemPrompt,
		Messages:  []anthropic.Message{{Role: "user", Content: user}},
	})
	if err != nil {
		return Result{}, fmt.Errorf("grader: anthropic call: %w", err)
	}
	text := resp.FirstText()
	score, perr := parseScore(text)
	if perr != nil {
		return Result{}, fmt.Errorf("grader: parse response: %w (raw=%q)", perr, text)
	}
	return Result{
		Score:      score,
		TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
		Model:      g.model,
		Raw:        text,
	}, nil
}

func renderUserMessage(req Request) string {
	var b strings.Builder
	b.WriteString("=== Task ===\n")
	b.WriteString(req.Task)
	b.WriteString("\n\n=== Candidate draft ===\n")
	b.WriteString(req.Candidate)
	if req.Notes != "" {
		b.WriteString("\n\n=== Context ===\n")
		b.WriteString(req.Notes)
	}
	b.WriteString("\n\nReply with the JSON object only.")
	return b.String()
}

func parseScore(raw string) (Score, error) {
	body := extractFirstJSONObject(raw)
	if body == "" {
		return Score{}, fmt.Errorf("no JSON object found")
	}
	var s Score
	if err := json.Unmarshal([]byte(body), &s); err != nil {
		return Score{}, err
	}
	for _, val := range []int{s.Relevance, s.Tone, s.Completeness, s.Professionalism} {
		if val < 1 || val > 5 {
			return Score{}, fmt.Errorf("score out of range (1-5): %d", val)
		}
	}
	return s, nil
}

// extractFirstJSONObject — same brace-balancing scanner as pkg/judge,
// pkg/recomposer, pkg/adapter/anthropic. Tolerates markdown fences.
func extractFirstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			switch ch {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// Compile-time check.
var _ Grader = (*AnthropicGrader)(nil)
