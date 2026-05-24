// Package ollama is a direct-to-Ollama adapter that bypasses Hermes.
//
// Why this exists: Hermes is an agentic substrate built for tool-calling
// (its docs recommend models ≥30B for reliable tool-call behavior).
// Small models (≤7B) like phi4-mini get crushed by Hermes's baseline
// agentic envelope — empirically the wrapped call ships ~4k tokens of
// boilerplate per request, and small substrates degenerate into
// truncated gibberish before they reach the user's question.
//
// This adapter sends only the playbook instructions and the AP input
// to Ollama's OpenAI-compatible /v1/chat/completions endpoint. No
// agentic envelope, no tool-call scaffolding. The result is that small
// models actually get to reason about the input.
//
// Hermes stays the right substrate for capable agentic models; this
// adapter is the right substrate for small models, frozen-weight
// reasoning, and any case where the bench wants to measure the raw
// model rather than the model-plus-agent-framework.
//
// Response shape: the OpenAI-compat surface exposes finish_reason
// (stop|length|content_filter|tool_calls), which the Hermes /v1/runs
// surface does not. The adapter surfaces it in trace events so the
// bench categorize layer can distinguish "model finished naturally"
// from "we cut it off" — something Hermes can't tell us today.
package ollama

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
	adapterName = "ollama-direct"

	// DefaultRunTimeout bounds wall-clock for one /v1/chat/completions
	// call. Generous — small CPU-only setups can take a while on a
	// cold model load.
	DefaultRunTimeout = 10 * time.Minute

	// DefaultBaseURL is Ollama's default listen address.
	DefaultBaseURL = "http://127.0.0.1:11434"
)

// Endpoint binds either the evaluate step or a playbook's execute
// step to one Ollama server + model.
type Endpoint struct {
	// BaseURL of the Ollama server (no trailing slash; the adapter
	// appends "/v1/chat/completions" etc.).
	BaseURL string

	// Model is the Ollama model tag (e.g. "phi4-mini:latest").
	// Required — Ollama does not have a server-side default.
	Model string

	// Options is the optional pass-through to Ollama's "options" map
	// (num_ctx, temperature, top_p, etc.). Keys are model-specific;
	// see Ollama docs.
	Options map[string]any
}

// Config wires the adapter. Parity-shaped with pkg/adapter/hermes.Config
// so callers (bench, runner) can swap them with minimal ceremony.
type Config struct {
	// EvaluateEndpoint is the Ollama instance for the evaluate step.
	// Required.
	EvaluateEndpoint Endpoint

	// ExecuteEndpoints maps a playbook to its execute Ollama instance.
	// At least one entry required; an Execute call against a playbook
	// missing from this map errors out — no silent fallback to the
	// evaluate endpoint.
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

	// Inspector enables v3.1 prompt-side notice inspection on every
	// call. Optional — nil disables. When set, the adapter runs
	// Inspector.Inspect(systemPrompt, userContent) just before
	// posting and emits an `ollama.notice_assessment` trace event
	// when any detector fires. See pkg/notice for the signal
	// catalog (input overflows context, persona-heavy, etc.).
	Inspector *notice.Inspector

	// ResponseFormat is the OpenAI-compat `response_format`
	// parameter — when set, the chat-completions engine constrains
	// the model to emit JSON matching this schema. Used to force
	// format compliance without the legacy 4k-token envelope (see
	// pkg/adapter/minimal.AsResponseFormat for the standard wrap).
	//
	// Optional. nil = no constraint; model emits free-form text
	// and the adapter's unstructuredFallback parses what it can.
	//
	// Ollama, vLLM, and LM Studio honor this parameter via their
	// OpenAI-compat surface. Hermes does not (it speaks /v1/runs,
	// not /v1/chat/completions) — for Hermes, format enforcement
	// would happen via soul.md, which we deliberately exited.
	ResponseFormat map[string]any

	// Thinking decides whether reasoning/thinking-mode should be
	// enabled for each Execute call. nil = substrate default
	// (which on Qwen3.x, DeepSeek-R1, Magistral etc. means
	// thinking-on; on non-reasoning substrates it's a no-op).
	//
	// Wired as `"think": <bool>` at the top level of the
	// chat-completions request body. The Ollama /v1/chat/completions
	// surface honors this flag; substrates without reasoning mode
	// silently ignore it.
	//
	// See pkg/thinking for stock policies (AlwaysOn / AlwaysOff /
	// ByStakes / ByPlaybook / Composite) and references/
	// reasoning-model-setup.md for the architectural framing.
	Thinking thinking.Policy
}

// Adapter is an adapter.Adapter targeting one Ollama instance per
// playbook for execute and a separate Ollama for evaluate.
type Adapter struct {
	cfg Config
}

// New constructs an adapter. Returns an error for invalid config —
// adapter-construction failures should surface before any runner gets
// a chance to dispatch.
func New(cfg Config) (*Adapter, error) {
	if cfg.EvaluateEndpoint.BaseURL == "" {
		return nil, errors.New("ollama: EvaluateEndpoint.BaseURL required")
	}
	if cfg.EvaluateEndpoint.Model == "" {
		return nil, errors.New("ollama: EvaluateEndpoint.Model required (no server-side default)")
	}
	if len(cfg.ExecuteEndpoints) == 0 {
		return nil, errors.New("ollama: at least one ExecuteEndpoints entry required")
	}
	cfg.EvaluateEndpoint.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.EvaluateEndpoint.BaseURL), "/")
	normalized := make(map[session.Playbook]Endpoint, len(cfg.ExecuteEndpoints))
	for pb, ep := range cfg.ExecuteEndpoints {
		trimmed := strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/")
		if trimmed == "" {
			return nil, fmt.Errorf("ollama: ExecuteEndpoints[%q].BaseURL is empty", pb)
		}
		if ep.Model == "" {
			return nil, fmt.Errorf("ollama: ExecuteEndpoints[%q].Model is empty", pb)
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
// every v0 playbook to one Ollama instance. Mirrors hermes.SingleEndpoint.
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
// own Ollama instance — mirrors hermes.MultiEndpoint shape so callers
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
		return Config{}, fmt.Errorf("ollama: MultiEndpoint missing endpoints for playbooks: %s", strings.Join(missing, ", "))
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
	raw, _, usage, finish, err := a.oneCall(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate")
	if err != nil {
		return env, err
	}
	a.recordCost(a.cfg.EvaluateEndpoint, usage)
	parsed, structured, err := parseEvaluateResponse(raw)
	if err != nil {
		return env, err
	}
	if !structured && a.cfg.RequireStructured {
		return env, fmt.Errorf("ollama: evaluate produced no structured envelope (raw length %d, finish_reason %q)", len(raw), finish)
	}
	if !structured {
		// Conservative fallback: default to PlaybookProcess with low
		// confidence. Same fallback shape as hermes.
		parsed.Playbook = string(session.PlaybookProcess)
		parsed.Confidence = 0.1
		parsed.Rationale = "unstructured evaluate response; fallback to process"
		trace.Info(a.cfg.Tracer, ctx, "ollama.evaluate_unstructured_fallback",
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
		return env, errors.New("ollama: Execute called before Evaluate (env.Evaluate is nil)")
	}
	pb := env.Evaluate.Playbook
	ep, ok := a.cfg.ExecuteEndpoints[pb]
	if !ok {
		return env, fmt.Errorf("ollama: no execute endpoint registered for playbook %q", pb)
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
	raw, reasoning, usage, _, err := a.oneCall(ctx, ep, env, instructions, "execute")
	if err != nil {
		return env, err
	}
	a.recordCost(ep, usage)

	execute, err := parseExecuteResponse(pb, raw, a.cfg.RequireStructured)
	if err != nil {
		return env, err
	}
	execute.TokensUsed = usage.input + usage.output
	// Stamp the model's reasoning trace (when present) onto the
	// return_result so downstream consumers (cope dispatcher,
	// recomposer, curation) can read it. Empty when the substrate
	// didn't think or thinking-mode was disabled by policy.
	if reasoning != "" && execute.ReturnResult != nil {
		execute.ReturnResult.Reasoning = reasoning
	}
	// v3.3: stamp effective confidence onto the return_result so the
	// orchestrator surfaces it in benchmark.Answer. For
	// minimal-output responses where the JSON envelope is absent,
	// parseExecuteResponse's unstructuredFallback sets Confidence to
	// the 0.1 stub. Recover the real number from the raw text via
	// notice.ExtractConfidence, then apply the v3.2 signal
	// adjustments.
	applyEffectiveConfidence(execute, raw, a.cfg.Inspector)
	env.Execute = execute
	env.ExitReason = session.ExitDone
	return env, nil
}

// applyEffectiveConfidence recovers + adjusts confidence when the
// minimal-output path produced an unstructured-fallback Execute
// (Confidence == 0 = "not reported"). JSON-parsed confidences
// (Confidence > 0 from parseReturnResultJSON) are left alone —
// those are already the model's self-report.
//
// Pure stamping logic lives in notice.EffectiveConfidenceFromRaw
// (adapter-agnostic, tested there). This function is the
// adapter-specific gate that decides WHEN to stamp.
//
// When the substrate didn't emit a `confidence: X.X` line AND the
// JSON envelope was absent, Confidence stays 0 — which cope.Decide
// reads as "ship_with_caveat" (NOT "escalate"). Escalating on
// missing data is expensive and wrong; the safe default is to
// flag uncertainty without paying for the frontier call.
func applyEffectiveConfidence(exec *session.Execute, raw string, inspector *notice.Inspector) {
	if exec == nil || exec.ReturnResult == nil {
		return
	}
	// JSON-parsed confidence is authoritative; only touch the
	// unstructured-fallback case (Confidence stays zero from
	// structured.go's unstructuredFallback).
	if exec.ReturnResult.Confidence > 0 {
		return
	}
	if conf, ok := notice.EffectiveConfidenceFromRaw(raw, inspector); ok {
		exec.ReturnResult.Confidence = conf
	}
}

// boundContext applies the configured RunTimeout if the caller's
// context has no deadline of its own. Mirrors hermes.
func (a *Adapter) boundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, nil
	}
	return context.WithTimeout(ctx, a.cfg.RunTimeout)
}

// tokenUsage is the per-call token accounting from Ollama's
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
	// Think is the per-call thinking-mode flag honored by Ollama's
	// /v1/chat/completions surface (and the underlying engine's
	// support for the Qwen3-style reasoning toggle). Pointer so
	// nil → omit (substrate default) vs explicitly true / false.
	Think *bool `json:"think,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Reasoning carries the substrate's hidden chain-of-thought
	// trace when the model produced one (Qwen3.x, DeepSeek-R1,
	// Magistral, etc., when thinking-mode is on). Empty on
	// non-reasoning substrates or when thinking was disabled.
	// Surfaced separately from Content by Ollama's /v1 wrapper so
	// the JSON parser doesn't have to deal with embedded reasoning.
	Reasoning string `json:"reasoning,omitempty"`
}

// chatResponse matches the OpenAI /v1/chat/completions response.
// Ollama implements the surface but emits extra fields; we ignore the
// extras with default JSON behavior.
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
// text, the model's hidden reasoning trace (empty when none), token
// usage, and the OpenAI-style finish_reason. The finish_reason is
// written to the trace so categorize can read it.
func (a *Adapter) oneCall(ctx context.Context, ep Endpoint, env session.Envelope, instructions, phase string) (string, string, tokenUsage, string, error) {
	// v3.1: pre-call notice inspection. When the operator wired an
	// Inspector, run the prompt-side detectors and emit a trace
	// event if any fired. The call itself is unchanged — notice is
	// observability, not enforcement (the act layer in v3.4 reads
	// these signals to drive coping decisions).
	a.inspectPreCall(ctx, env, instructions, phase)

	body := chatRequest{
		Model: ep.Model,
		Messages: []chatMessage{
			{Role: "system", Content: instructions},
			{Role: "user", Content: env.Input.Content},
		},
		Stream:         false,
		Options:        ep.Options,
		ResponseFormat: a.cfg.ResponseFormat,
	}
	// Thinking-mode: ask the policy. nil-safe — when the operator
	// hasn't wired a policy, leave the field unset and the substrate
	// uses its own default.
	if a.cfg.Thinking != nil {
		think := a.cfg.Thinking.ShouldThink(env)
		body.Think = &think
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("ollama: marshal chat request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.BaseURL+"/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("ollama: build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("ollama: POST /v1/chat/completions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", tokenUsage{}, "", fmt.Errorf("ollama: POST /v1/chat/completions: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", "", tokenUsage{}, "", fmt.Errorf("ollama: decode chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", "", tokenUsage{}, "", errors.New("ollama: chat response had no choices")
	}
	choice := parsed.Choices[0]
	usage := tokenUsage{
		input:  parsed.Usage.PromptTokens,
		output: parsed.Usage.CompletionTokens,
	}
	trace.Info(a.cfg.Tracer, ctx, "ollama.call_completed",
		slog.String("ap_id", string(env.ID)),
		slog.String("phase", phase),
		slog.String("model", ep.Model),
		slog.String("finish_reason", choice.FinishReason),
		slog.Int("tokens_in", usage.input),
		slog.Int("tokens_out", usage.output),
		slog.Duration("elapsed", time.Since(start)),
	)

	// v3.2: response-side notice inspection. Runs after the
	// substrate returns; complements the pre-call inspection that
	// fired before posting. Extracts self-reported confidence,
	// applies signal-driven adjustments, and emits a trace event
	// when anything fires.
	a.inspectPostCall(ctx, env, choice.Message.Content, phase)

	// Surface the reasoning trace as a separate trace event when
	// the model emitted one. Operators reading `-v` logs see the
	// full (truncated) reasoning content; downstream consumers can
	// grep `msg=ollama.thinking_emitted` to find every reasoning
	// trace produced across a run.
	if choice.Message.Reasoning != "" {
		trace.Info(a.cfg.Tracer, ctx, "ollama.thinking_emitted",
			slog.String("ap_id", string(env.ID)),
			slog.String("phase", phase),
			slog.String("model", ep.Model),
			slog.Int("reasoning_chars", len(choice.Message.Reasoning)),
			slog.String("reasoning", truncateForTrace(choice.Message.Reasoning, maxTraceReasoningChars)),
		)
	}

	return choice.Message.Content, choice.Message.Reasoning, usage, choice.FinishReason, nil
}

// maxTraceReasoningChars caps the per-call reasoning trace emitted to
// the tracer. Reasoning traces can be thousands of tokens; logging
// them in full would bloat the trace stream. The cap is generous
// enough that you can usually read the model's chain of thought
// in `-v` output without scrolling through pages.
const maxTraceReasoningChars = 4000

// truncateForTrace shortens s to at most n characters, appending a
// "…[truncated, N more chars]" suffix when truncated. Used for
// trace-event payloads where the full content lives elsewhere (or in
// the model's own JSONL output) and the trace just needs a readable
// preview.
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
		trace.Error(a.cfg.Tracer, ctx, "ollama.semantic_pool_read_failed",
			slog.String("err", err.Error()),
		)
		return instructions
	}
	if snap.IsOverBudget() {
		trace.Info(a.cfg.Tracer, ctx, "ollama.semantic_pool_over_budget",
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

// inspectPreCall runs the v3.1 notice layer (when Inspector is
// wired) and emits an `ollama.notice_assessment` trace event if any
// detector fired. No-op when no Inspector or no detections. Never
// alters the call — notice is observability, not enforcement; the
// act layer (v3.4) reads these signals to drive coping decisions.
func (a *Adapter) inspectPreCall(ctx context.Context, env session.Envelope, instructions, phase string) {
	if a.cfg.Inspector == nil {
		return
	}
	assess := a.cfg.Inspector.Inspect(instructions, env.Input.Content)
	if len(assess.Detections) == 0 {
		return
	}
	// Aggregate detections into a compact log line — operators want
	// "what fired, how bad" at a glance, not the full list per call.
	signals := make([]string, 0, len(assess.Detections))
	topSeverity := notice.Info
	for _, d := range assess.Detections {
		signals = append(signals, string(d.Signal))
		if severityRank(d.Severity) > severityRank(topSeverity) {
			topSeverity = d.Severity
		}
	}
	trace.Info(a.cfg.Tracer, ctx, "ollama.notice_assessment",
		slog.String("ap_id", string(env.ID)),
		slog.String("phase", phase),
		slog.Float64("score", assess.Score),
		slog.Int("detection_count", len(assess.Detections)),
		slog.String("top_severity", string(topSeverity)),
		slog.String("signals", strings.Join(signals, ",")),
	)
}

// inspectPostCall runs the v3.2 response-side notice layer. Extracts
// self-reported confidence (when present in the raw text per the
// minimal-output convention) and emits `ollama.notice_response_assessment`
// when any detector fires OR when an extracted-confidence value is
// available (operators want to see the per-call confidence even on
// clean calls). The event includes both raw and effective confidence
// so downstream can correlate signal-driven downgrades.
//
// No-op when Inspector is nil. Never alters the call — observability
// only.
func (a *Adapter) inspectPostCall(ctx context.Context, env session.Envelope, response, phase string) {
	if a.cfg.Inspector == nil {
		return
	}
	assess := a.cfg.Inspector.InspectResponse(response)
	rawConf, hasConf := notice.ExtractConfidence(response)

	// Don't emit if there's nothing useful to say.
	if len(assess.Detections) == 0 && !hasConf {
		return
	}

	effConf := rawConf
	if hasConf {
		effConf = notice.EffectiveConfidence(rawConf, assess)
	}

	signals := make([]string, 0, len(assess.Detections))
	topSeverity := notice.Info
	for _, d := range assess.Detections {
		signals = append(signals, string(d.Signal))
		if severityRank(d.Severity) > severityRank(topSeverity) {
			topSeverity = d.Severity
		}
	}

	attrs := []slog.Attr{
		slog.String("ap_id", string(env.ID)),
		slog.String("phase", phase),
		slog.Float64("score", assess.Score),
		slog.Int("detection_count", len(assess.Detections)),
		slog.String("top_severity", string(topSeverity)),
		slog.String("signals", strings.Join(signals, ",")),
	}
	if hasConf {
		attrs = append(attrs,
			slog.Float64("raw_confidence", rawConf),
			slog.Float64("effective_confidence", effConf),
		)
	}
	trace.Info(a.cfg.Tracer, ctx, "ollama.notice_response_assessment", attrs...)
}

// severityRank gives a comparable ordering: Info < Warning < Error.
func severityRank(s notice.Severity) int {
	switch s {
	case notice.Error:
		return 3
	case notice.Warning:
		return 2
	case notice.Info:
		return 1
	default:
		return 0
	}
}
