// CLAUDE GENERATED
package recomposer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/anthropic"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// DefaultAnthropicModel is the default frontier model for the
// Synthesizer. Sonnet is the sweet spot for synthesis: stronger than
// Haiku at coherence, far cheaper than Opus. Operators can override.
const DefaultAnthropicModel = "claude-sonnet-4-6"

// DefaultMaxTokens caps the Synthesizer's reply length. Synthesis is
// the part that generates real content; this cap is much higher than
// the Judge's (verifier verdicts are tiny).
const DefaultMaxTokens = 4096

// AnthropicSynthesizer is a Synthesizer backed by the Anthropic
// Messages API. Given two payloads, it asks the frontier model to
// produce a coherent merger and parses {content, confidence} from the
// response.
type AnthropicSynthesizer struct {
	client       *anthropic.Client
	model        string
	maxTokens    int
	systemPrompt string
}

// AnthropicConfig configures a new AnthropicSynthesizer.
type AnthropicConfig struct {
	APIKey       string
	BaseURL      string       // optional; tests pass an httptest server URL
	Model        string       // optional; defaults to DefaultAnthropicModel
	MaxTokens    int          // optional; defaults to DefaultMaxTokens
	SystemPrompt string       // optional; overrides the built-in synthesis preamble
	HTTPClient   *http.Client // optional; forwarded to anthropic.Client
}

// NewAnthropic constructs an AnthropicSynthesizer. APIKey is required.
func NewAnthropic(cfg AnthropicConfig) (*AnthropicSynthesizer, error) {
	clientCfg := anthropic.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		HTTPClient: cfg.HTTPClient,
	}
	client, err := anthropic.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("recomposer.NewAnthropic: %w", err)
	}
	model := cfg.Model
	if model == "" {
		model = DefaultAnthropicModel
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultSynthesizerSystemPrompt
	}
	return &AnthropicSynthesizer{
		client:       client,
		model:        model,
		maxTokens:    maxTokens,
		systemPrompt: systemPrompt,
	}, nil
}

const defaultSynthesizerSystemPrompt = `You are a frontier synthesizer. ` +
	`You receive two pieces of work output and must combine them into a single coherent result. ` +
	`Do not concatenate; integrate. Drop redundancy, resolve mild contradictions in favor of consistency, ` +
	`and produce output the reader would want as the final answer if there was only one to read. ` +
	`Return your output as a single JSON object with this exact shape — no prose, no markdown:` + "\n" +
	`{"content": "<combined content>", "confidence": <0.0..1.0>}` + "\n" +
	`Set confidence to your judgment of how well the integration succeeded — 1.0 if the pieces were clean and combined seamlessly, lower if they conflicted or were sparse.`

// Name implements Synthesizer.
func (s *AnthropicSynthesizer) Name() string { return "anthropic" }

// Synthesize implements Synthesizer.
func (s *AnthropicSynthesizer) Synthesize(ctx context.Context, req SynthesizeRequest) (SynthesizeResponse, error) {
	userMsg := renderSynthesizerUserMessage(req)
	resp, err := s.client.Messages(ctx, anthropic.MessagesRequest{
		Model:     s.model,
		MaxTokens: s.maxTokens,
		System:    s.systemPrompt,
		Messages:  []anthropic.Message{{Role: "user", Content: userMsg}},
	})
	if err != nil {
		return SynthesizeResponse{}, fmt.Errorf("synthesizer: anthropic call: %w", err)
	}
	text := resp.FirstText()
	content, confidence, perr := parseSynthesizerResponse(text)
	if perr != nil {
		return SynthesizeResponse{}, fmt.Errorf("synthesizer: parse response: %w (raw=%q)", perr, text)
	}
	return SynthesizeResponse{
		Content:    content,
		Confidence: confidence,
		TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}, nil
}

// renderSynthesizerUserMessage builds the user message for synthesis.
// Step index + recompose spec ride in case the model wants context.
func renderSynthesizerUserMessage(req SynthesizeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Reduction step %d (spec=%s).\n\n", req.StepIndex, req.RecomposeSpec)
	b.WriteString("Left:\n")
	b.WriteString(req.Left.Result.Content)
	b.WriteString("\n\nRight:\n")
	b.WriteString(req.Right.Result.Content)
	b.WriteString("\n\nIntegrate Left and Right. Reply with the JSON object only.")
	return b.String()
}

type synthResponseShape struct {
	Content    string  `json:"content"`
	Confidence float64 `json:"confidence"`
}

func parseSynthesizerResponse(raw string) (string, float64, error) {
	body := extractFirstJSONObject(raw)
	if body == "" {
		return "", 0, fmt.Errorf("no JSON object in response")
	}
	var parsed synthResponseShape
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", 0, err
	}
	if parsed.Content == "" {
		return "", 0, fmt.Errorf("response content field is empty")
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	return parsed.Content, parsed.Confidence, nil
}

// extractFirstJSONObject is the same brace-balancing scanner used by
// pkg/judge — kept inline here to avoid a cross-package internal
// helper. If a third consumer arrives, factor it out.
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
var _ Synthesizer = (*AnthropicSynthesizer)(nil)

// session import kept to support future SessionEstimate-flavored
// extensions (e.g., model-context-aware budget) without breaking the
// import. Remove if it stays unused for >1 turn.
var _ = session.RecomposeNone
