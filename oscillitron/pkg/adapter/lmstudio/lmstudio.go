// Package lmstudio is a direct-to-LM-Studio adapter that bypasses
// Hermes.
//
// Why this exists: LM Studio (https://lmstudio.ai) is the easiest
// local-LLM server for non-technical operators — one-click model
// download from its built-in HuggingFace browser, a GUI for picking
// the loaded model and tuning runtime knobs, and an OpenAI-compatible
// /v1 surface served on 127.0.0.1:1234 by default. For Oscillitron
// users who run on a desktop dev machine, LM Studio is often the
// loaded gun already pointed at their GPU — easier to set up than
// Ollama if they already use the GUI.
//
// The direct adapter lets those operators point bench at their
// existing LM Studio instance without standing up Hermes. Same
// two-step contract (Evaluate / Execute), same JSON payload
// protocol as the Hermes and Ollama adapters — drop-in substrate
// swap.
//
// Response shape: LM Studio implements the OpenAI /v1/chat/completions
// surface, including finish_reason (stop|length|content_filter|
// tool_calls). The adapter surfaces it in trace events so the bench
// categorize layer can distinguish "model finished naturally" from
// "we cut it off" — something Hermes's /v1/runs surface does not
// expose.
package lmstudio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/notice"
	"github.com/jrlmx2/oscillitron/pkg/semanticpool"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/thinking"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

const (
	adapterName = "lmstudio-direct"

	// DefaultRunTimeout bounds wall-clock for one /v1/chat/completions
	// call. Generous — small CPU-only setups can take a while on a
	// cold model load.
	DefaultRunTimeout = 10 * time.Minute

	// DefaultBaseURL is LM Studio's default Local Server listen
	// address (the "Local Server" tab's default port; configurable
	// in the GUI).
	DefaultBaseURL = "http://127.0.0.1:1234"
)

// Endpoint binds either the evaluate step or a playbook's execute
// step to one LM Studio server + model.
type Endpoint struct {
	// BaseURL of the LM Studio server (no trailing slash; the
	// adapter appends "/v1/chat/completions" etc.).
	BaseURL string

	// Model is the LM Studio model identifier as exposed by its
	// UI — typically the HuggingFace repo path of the loaded model
	// (e.g. "lmstudio-community/Qwen2.5-7B-Instruct-GGUF/qwen2.5-7b-instruct-q4_k_m.gguf")
	// or a short alias the operator configured in the "Local Server"
	// tab. Required — LM Studio does not have a server-side default.
	Model string

	// Options is the optional pass-through to LM Studio's "options"
	// map for engine-specific knobs (temperature, top_p, num_ctx,
	// etc.). Keys are model-specific.
	Options map[string]any
}

// Config wires the adapter. Parity-shaped with pkg/adapter/hermes.Config
// and pkg/adapter/ollama.Config so callers (bench, runner) can swap
// them with minimal ceremony.
type Config struct {
	// EvaluateEndpoint is the LM Studio instance for the evaluate
	// step. Required.
	EvaluateEndpoint Endpoint

	// ExecuteEndpoints maps a playbook to its execute LM Studio
	// instance. At least one entry required; an Execute call against
	// a playbook missing from this map errors out — no silent fallback
	// to the evaluate endpoint.
	ExecuteEndpoints map[session.Playbook]Endpoint

	// HTTPClient overrides the default. Optional.
	HTTPClient *http.Client

	// RunTimeout bounds the wall-clock for one chat completion call.
	// Defaults to DefaultRunTimeout. Only applied if the caller's
	// context has no deadline of its own.
	RunTimeout time.Duration

	// Tracer is the fat learning-loop sink (per the lean-AP /
	// fat-trace split). nil-safe.
	Tracer trace.Tracer

	// Cost is the optional cost tracker. When set, completion usage
	// records into the ledger.
	Cost *cost.Tracker

	// RequireStructured controls how strictly the adapter enforces
	// the structured-output envelope. When true, any output that
	// cannot be parsed as the documented JSON shape is surfaced as
	// an error. When false (the default), parse failure falls back
	// to a low-confidence return_result placeholder — useful for
	// small models that don't always honor the format.
	RequireStructured bool

	// Inspector enables effective-confidence recovery on the
	// unstructured (process/compose) path: the substrate answers in
	// natural text with a trailing "confidence: X.X" line, the adapter
	// recovers that number and (when set) adjusts it by the v3.2
	// response signals before stamping. Optional; nil recovers the raw
	// annotated value unadjusted. Mirrors pkg/adapter/ollama.
	Inspector *notice.Inspector

	// SemanticPool is the optional shared-knowledge store. When set,
	// the adapter prepends the pool's rendered preamble to every
	// call's instructions — a stable, cache-friendly addition.
	SemanticPool semanticpool.Pool

	// RawEvaluateInstructions overrides the adapter's default evaluate
	// preamble. Most callers should leave this empty.
	RawEvaluateInstructions string

	// RawExecuteInstructions overrides the adapter's default execute
	// preamble per playbook. Most callers should leave this empty.
	RawExecuteInstructions map[session.Playbook]string

	// ResponseFormat is the OpenAI-compat `response_format`
	// parameter (v3.5). When set, LM Studio constrains the model
	// to emit JSON matching this schema. See pkg/adapter/minimal
	// for the standard wrap. Optional; nil = no constraint.
	ResponseFormat map[string]any

	// ExecuteResponseFormats provides per-playbook response_format
	// schemas for Execute calls. When set, Execute looks up the
	// playbook's schema here first; if missing, falls back to
	// ResponseFormat. Evaluate always passes nil (prompt-only).
	ExecuteResponseFormats map[session.Playbook]map[string]any

	// Thinking decides whether reasoning/thinking-mode should be
	// enabled per Execute call. See pkg/adapter/ollama.Config.Thinking
	// for the architectural framing. LM Studio's chat-completions
	// surface honors the `"think"` flag for substrates that support
	// it; others silently ignore.
	Thinking thinking.Policy
}

// Adapter is an adapter.Adapter targeting one LM Studio instance per
// playbook for execute and a separate LM Studio for evaluate.
type Adapter struct {
	cfg Config
}

// New constructs an adapter. Returns an error for invalid config —
// adapter-construction failures should surface before any runner gets
// a chance to dispatch.
func New(cfg Config) (*Adapter, error) {
	if cfg.EvaluateEndpoint.BaseURL == "" {
		return nil, errors.New("lmstudio: EvaluateEndpoint.BaseURL required")
	}
	if cfg.EvaluateEndpoint.Model == "" {
		return nil, errors.New("lmstudio: EvaluateEndpoint.Model required (no server-side default)")
	}
	if len(cfg.ExecuteEndpoints) == 0 {
		return nil, errors.New("lmstudio: at least one ExecuteEndpoints entry required")
	}
	cfg.EvaluateEndpoint.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.EvaluateEndpoint.BaseURL), "/")
	normalized := make(map[session.Playbook]Endpoint, len(cfg.ExecuteEndpoints))
	for pb, ep := range cfg.ExecuteEndpoints {
		trimmed := strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/")
		if trimmed == "" {
			return nil, fmt.Errorf("lmstudio: ExecuteEndpoints[%q].BaseURL is empty", pb)
		}
		if ep.Model == "" {
			return nil, fmt.Errorf("lmstudio: ExecuteEndpoints[%q].Model is empty", pb)
		}
		ep.BaseURL = trimmed
		normalized[pb] = ep
	}
	cfg.ExecuteEndpoints = normalized
	if cfg.HTTPClient == nil {
		// No per-request Timeout on the client — context governs
		// deadlines, mirroring the Hermes adapter's pattern.
		cfg.HTTPClient = &http.Client{}
	}
	if cfg.RunTimeout == 0 {
		cfg.RunTimeout = DefaultRunTimeout
	}
	if cfg.Tracer == nil {
		cfg.Tracer = trace.Discard{}
	}
	return &Adapter{cfg: cfg}, nil
}

// SingleEndpoint is a convenience that binds the evaluate step and
// every v0 playbook to one LM Studio instance. Mirrors
// hermes.SingleEndpoint and ollama.SingleEndpoint.
func SingleEndpoint(baseURL, model string) Config {
	ep := Endpoint{BaseURL: baseURL, Model: model}
	exec := make(map[session.Playbook]Endpoint, len(AllPlaybooks))
	for _, pb := range AllPlaybooks {
		exec[pb] = ep
	}
	return Config{
		EvaluateEndpoint: ep,
		ExecuteEndpoints: exec,
	}
}

// MultiEndpoint constructs a Config that routes each playbook to its
// own LM Studio instance — mirrors hermes.MultiEndpoint shape so callers
// can swap substrates without changing config-construction code.
func MultiEndpoint(evaluate Endpoint, byPlaybook map[session.Playbook]Endpoint) (Config, error) {
	missing := make([]string, 0, len(AllPlaybooks))
	exec := make(map[session.Playbook]Endpoint, len(AllPlaybooks))
	for _, pb := range AllPlaybooks {
		ep, ok := byPlaybook[pb]
		if !ok {
			missing = append(missing, string(pb))
			continue
		}
		exec[pb] = ep
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("lmstudio: MultiEndpoint missing endpoints for playbooks: %s", strings.Join(missing, ", "))
	}
	return Config{
		EvaluateEndpoint: evaluate,
		ExecuteEndpoints: exec,
	}, nil
}

// AllPlaybooks lists every v0 playbook (mirrors hermes.AllPlaybooks).
var AllPlaybooks = []session.Playbook{
	session.PlaybookPlan,
	session.PlaybookProcess,
	session.PlaybookCritique,
	session.PlaybookVerifyGrounded,
	session.PlaybookCompose,
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return adapterName }

// Evaluate implements adapter.Adapter. Posts the AP envelope to the
// configured EvaluateEndpoint, parses the JSON response, and stitches
// env.Evaluate.
func (a *Adapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	ctx, cancel := a.boundContext(ctx)
	if cancel != nil {
		defer cancel()
	}
	instructions := a.cfg.RawEvaluateInstructions
	if instructions == "" {
		instructions = renderEvaluateInstructions(env)
	}
	instructions = a.withPoolPreamble(ctx, instructions)
	raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate", nil)
	if err != nil {
		return env, err
	}
	a.recordCost(a.cfg.EvaluateEndpoint, usage)
	parsed, structured, err := parseEvaluateResponse(raw)
	if err != nil {
		return env, err
	}
	if !structured && a.cfg.RequireStructured {
		return env, fmt.Errorf("lmstudio: evaluate produced no structured envelope (raw length %d, finish_reason %q)", len(raw), finish)
	}
	if !structured {
		// Conservative fallback: default to PlaybookProcess with low
		// confidence. Same fallback shape as hermes / ollama.
		parsed.Playbook = string(session.PlaybookProcess)
		parsed.Confidence = 0.1
		parsed.Rationale = "unstructured evaluate response; fallback to process"
		trace.Info(a.cfg.Tracer, ctx, "lmstudio.evaluate_unstructured_fallback",
			slog.String("ap_id", string(env.ID)),
			slog.String("finish_reason", finish),
		)
	}
	pb, err := parsePlaybook(parsed.Playbook)
	if err != nil {
		return env, err
	}
	env.Evaluate = &session.Evaluate{
		Playbook:   pb,
		Rationale:  parsed.Rationale,
		Confidence: parsed.Confidence,
		TokensUsed: usage.input + usage.output,
	}
	return env, nil
}

// Execute implements adapter.Adapter. Looks up the endpoint for the
// playbook chosen by Evaluate, posts the AP envelope, parses the
// playbook-specific JSON response, and stitches env.Execute.
func (a *Adapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if env.Evaluate == nil {
		return env, errors.New("lmstudio: Execute called before Evaluate (env.Evaluate is nil)")
	}
	pb := env.Evaluate.Playbook
	ep, ok := a.cfg.ExecuteEndpoints[pb]
	if !ok {
		return env, fmt.Errorf("lmstudio: no execute endpoint registered for playbook %q", pb)
	}

	ctx, cancel := a.boundContext(ctx)
	if cancel != nil {
		defer cancel()
	}

	instructions := a.cfg.RawExecuteInstructions[pb]
	if instructions == "" {
		instructions = renderExecuteInstructions(pb, env)
	}
	instructions = a.withPoolPreamble(ctx, instructions)
	raw, reasoning, usage, _, err := a.oneCall(ctx, ep, env, instructions, "execute", a.executeResponseFormat(pb))
	if err != nil {
		return env, err
	}
	a.recordCost(ep, usage)

	execute, err := parseExecuteResponse(pb, raw, a.cfg.RequireStructured)
	if err != nil {
		return env, err
	}
	execute.TokensUsed = usage.input + usage.output
	if reasoning != "" && execute.ReturnResult != nil {
		execute.ReturnResult.Reasoning = reasoning
	}
	// Recover the model's self-reported confidence from the natural-text
	// (process/compose) path and strip the annotation line from content.
	applyEffectiveConfidence(execute, raw, a.cfg.Inspector)
	env.Execute = execute
	env.ExitReason = session.ExitDone
	return env, nil
}

// executeResponseFormat returns the per-playbook response_format if
// configured, falling back to the adapter-wide ResponseFormat.
func (a *Adapter) executeResponseFormat(pb session.Playbook) map[string]any {
	if rf, ok := a.cfg.ExecuteResponseFormats[pb]; ok {
		return rf
	}
	return a.cfg.ResponseFormat
}

// boundContext applies the configured RunTimeout if the caller's
// context has no deadline of its own. Mirrors hermes.
func (a *Adapter) boundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, nil
	}
	return context.WithTimeout(ctx, a.cfg.RunTimeout)
}

// tokenUsage is the per-call token accounting from LM Studio's
// OpenAI-compat response.
type tokenUsage struct {
	input  int
	output int
}

// chatRequest matches the OpenAI /v1/chat/completions request body.
// We send stream=false because we want one shot per call; the bench's
// observability layer reads completions, not streamed deltas.
type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Stream         bool           `json:"stream"`
	Options        map[string]any `json:"options,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	// Think — see pkg/adapter/ollama.chatRequest.Think.
	Think *bool `json:"think,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Reasoning — see pkg/adapter/ollama.chatMessage.Reasoning.
	Reasoning string `json:"reasoning,omitempty"`
}

// chatResponse matches the OpenAI /v1/chat/completions response.
// LM Studio implements the surface but may emit extra fields; we
// ignore the extras with default JSON behavior.
type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// oneCall posts one chat-completion request and returns the response
// text, reasoning trace (empty when none), token usage, and the
// OpenAI-style finish_reason.
func (a *Adapter) oneCall(ctx context.Context, ep Endpoint, env session.Envelope, instructions, phase string, responseFormat map[string]any) (string, string, tokenUsage, string, error) {
	body := chatRequest{
		Model: ep.Model,
		Messages: []chatMessage{
			{Role: "system", Content: instructions},
			{Role: "user", Content: env.Input.Content},
		},
		Stream:         false,
		Options:        ep.Options,
		ResponseFormat: responseFormat,
	}
	if a.cfg.Thinking != nil {
		think := a.cfg.Thinking.ShouldThink(env)
		body.Think = &think
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("lmstudio: marshal chat request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.BaseURL+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("lmstudio: build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("lmstudio: POST /v1/chat/completions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", tokenUsage{}, "", fmt.Errorf("lmstudio: POST /v1/chat/completions: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("lmstudio: decode chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", "", tokenUsage{}, "", errors.New("lmstudio: chat response had no choices")
	}
	choice := parsed.Choices[0]
	usage := tokenUsage{
		input:  parsed.Usage.PromptTokens,
		output: parsed.Usage.CompletionTokens,
	}
	trace.Info(a.cfg.Tracer, ctx, "lmstudio.call_completed",
		slog.String("ap_id", string(env.ID)),
		slog.String("phase", phase),
		slog.String("model", ep.Model),
		slog.String("finish_reason", choice.FinishReason),
		slog.Int("tokens_in", usage.input),
		slog.Int("tokens_out", usage.output),
		slog.Duration("elapsed", time.Since(start)),
	)
	if choice.Message.Reasoning != "" {
		trace.Info(a.cfg.Tracer, ctx, "lmstudio.thinking_emitted",
			slog.String("ap_id", string(env.ID)),
			slog.String("phase", phase),
			slog.String("model", ep.Model),
			slog.Int("reasoning_chars", len(choice.Message.Reasoning)),
			slog.String("reasoning", truncateForTrace(choice.Message.Reasoning, maxTraceReasoningChars)),
		)
	}
	return choice.Message.Content, choice.Message.Reasoning, usage, choice.FinishReason, nil
}

// maxTraceReasoningChars caps per-call reasoning trace size for trace
// events. See pkg/adapter/ollama for rationale.
const maxTraceReasoningChars = 4000

func truncateForTrace(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("…[truncated, %d more chars]", len(s)-n)
}

// recordCost records the phase's usage into the tracker (when wired).
func (a *Adapter) recordCost(ep Endpoint, usage tokenUsage) {
	if a.cfg.Cost == nil || (usage.input == 0 && usage.output == 0) {
		return
	}
	model := ep.Model
	if model == "" {
		model = adapterName
	}
	a.cfg.Cost.Record(model, usage.input, usage.output)
}

// withPoolPreamble prepends the semantic-pool rendered preamble to
// instructions. Same shape as hermes.withPoolPreamble — keeps the
// shared-pool surface symmetric across substrates.
func (a *Adapter) withPoolPreamble(ctx context.Context, instructions string) string {
	if a.cfg.SemanticPool == nil {
		return instructions
	}
	snap, err := a.cfg.SemanticPool.All(ctx)
	if err != nil {
		trace.Error(a.cfg.Tracer, ctx, "lmstudio.semantic_pool_read_failed",
			slog.String("err", err.Error()),
		)
		return instructions
	}
	if snap.IsOverBudget() {
		trace.Info(a.cfg.Tracer, ctx, "lmstudio.semantic_pool_over_budget",
			slog.Int("bytes", snap.TotalBytes()),
			slog.Int("budget_bytes", semanticpool.SoftByteBudget),
		)
	}
	preamble := semanticpool.RenderPreamble(snap)
	if preamble == "" {
		return instructions
	}
	return preamble + "\n" + instructions
}
