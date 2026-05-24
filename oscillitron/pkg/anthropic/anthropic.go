// Package anthropic is a stdlib-only HTTP client for the Anthropic
// Messages API. Used by frontier-backed Judge (pkg/judge) and
// Synthesizer (pkg/recomposer) implementations.
//
// No SDK dependency — matches the project's "stdlib only" skeleton
// convention. Speaks the documented public API surface:
//
//	POST https://api.anthropic.com/v1/messages
//	x-api-key:        <key>
//	anthropic-version: 2023-06-01
//	content-type:     application/json
//
// What this package provides:
//
//   - Client with auth + base URL + HTTP client
//   - MessagesRequest / MessagesResponse types matching the public shape
//   - Single Messages(ctx, req) method
//
// What this package deliberately does NOT provide:
//
//   - Streaming. The Judge and Synthesizer want a single complete
//     response; SSE-style streaming would complicate the consumer
//     interfaces (judge.Response, recomposer.SynthesizeResponse) for
//     no win in our use case.
//   - Tool use. Out of scope for v0 of frontier-backed audit.
//   - Multi-turn conversations. Each call is a fresh request; the
//     consumer is responsible for any conversation state.
//   - Retries / backoff. Callers (or a wrapping cost-tracker) own
//     resilience. The runner already treats Judge errors as non-fatal.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the public Anthropic API root.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultAPIVersion is the anthropic-version header value Anthropic
// stable APIs use.
const DefaultAPIVersion = "2023-06-01"

// DefaultTimeout bounds a single Messages call.
const DefaultTimeout = 60 * time.Second

// Config configures the client.
type Config struct {
	// APIKey is sent as x-api-key. Required.
	APIKey string
	// BaseURL overrides the public endpoint. Useful for tests against
	// httptest servers or proxies. Empty = DefaultBaseURL.
	BaseURL string
	// APIVersion overrides the anthropic-version header. Empty =
	// DefaultAPIVersion.
	APIVersion string
	// HTTPClient lets callers inject a pre-configured client (timeouts,
	// proxies, etc.). nil = default http.Client with DefaultTimeout.
	HTTPClient *http.Client
}

// Client is the Anthropic Messages API client.
type Client struct {
	cfg Config
}

// New constructs a Client. Validates that APIKey is non-empty.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("anthropic: APIKey required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAPIVersion
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{cfg: cfg}, nil
}

// MessagesRequest mirrors the public Messages API request shape. Only
// the fields Judge and Synthesizer actually use are exposed; extend on
// real demand rather than mirroring the whole schema upfront.
type MessagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	System      string    `json:"system,omitempty"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// Message is one conversational turn.
type Message struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"` // plain text
}

// MessagesResponse mirrors the relevant fields of the Messages API
// response. The full schema has more — we read only what consumers
// need.
type MessagesResponse struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Role    string          `json:"role"`
	Content []ContentBlock  `json:"content"`
	Usage   Usage           `json:"usage"`
	Type    string          `json:"type"`
	Raw     json.RawMessage `json:"-"` // populated for debug / forward-compat
}

// ContentBlock is one item in the content array.
type ContentBlock struct {
	Type string `json:"type"` // typically "text"
	Text string `json:"text,omitempty"`
}

// Usage is the input/output token count from a response.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// FirstText returns the concatenated text of all text-type content
// blocks in the response. The Messages API may return multiple text
// blocks; consumers wanting structured JSON expect the model produced
// a single text block containing the JSON.
func (r MessagesResponse) FirstText() string {
	var b strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// Messages POSTs the request to /v1/messages and returns the parsed
// response. Errors include both transport failures and non-2xx status
// codes (with response body included for debug).
func (c *Client) Messages(ctx context.Context, req MessagesRequest) (MessagesResponse, error) {
	if req.Model == "" {
		return MessagesResponse{}, fmt.Errorf("anthropic: Model required")
	}
	if req.MaxTokens <= 0 {
		return MessagesResponse{}, fmt.Errorf("anthropic: MaxTokens must be > 0")
	}
	if len(req.Messages) == 0 {
		return MessagesResponse{}, fmt.Errorf("anthropic: at least one message required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	url := c.cfg.BaseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", c.cfg.APIVersion)

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("anthropic: POST /v1/messages: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22)) // 4 MiB safety cap
	if err != nil {
		return MessagesResponse{}, fmt.Errorf("anthropic: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MessagesResponse{}, fmt.Errorf("anthropic: status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}
	var parsed MessagesResponse
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		return MessagesResponse{}, fmt.Errorf("anthropic: decode response: %w (body=%q)", err, string(rawBody))
	}
	parsed.Raw = rawBody
	return parsed, nil
}
