// CLAUDE GENERATED
// Package hermes implements adapter.Adapter against the
// OpenAI-compatible HTTP gateway shipped by Nous Research's
// hermes-agent (see gateway/platforms/api_server.py in that repo).
//
// Architecture (per parent CLAUDE.md, "Architecture" locks 2026-05-19
// onward):
//
//   - Specialization lives in the playbook substrate keyed by action,
//     not in distinct node types. Per the per-instance playbook
//     substrate lock, each playbook (plan / process / critique /
//     verify_grounded / compose) gets its own long-lived Hermes
//     process — the persistent learning substrate that grows over
//     time within that cognitive niche.
//   - Evaluate is cheap-local-first. A separate EvaluateEndpoint
//     points at a fast, cheap Hermes; the frontier is not freely
//     selectable by evaluate. Frontier model use is restricted to
//     the delegate runtime escalation gate (critic failed past retry
//     budget) and to sampled verify_judge audits.
//   - Each AP runs two HTTP rounds: Evaluate (picks the playbook),
//     then Execute (runs the playbook). Both rounds reuse the
//     envelope ID as session_id within their respective Hermes for
//     idempotent retry.
//
// The adapter speaks the /v1/runs surface (POST /v1/runs to start,
// SSE on /v1/runs/{id}/events to drain) rather than the stateless
// /v1/chat/completions surface, so Hermes' own self-improvement loop
// stays in play within each specialist.
//
// Approvals (Hermes pausing mid-run for tool approval) are rejected
// as inhibited in v0 — the orchestrator owns gating, not the
// substrate. /v1/runs/{id}/approval will be wired in a later PR.
package hermes

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

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/cost"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Compile-time check that *Adapter satisfies adapter.Adapter.
var _ adapter.Adapter = (*Adapter)(nil)

const (
	// DefaultRunTimeout bounds wall-clock for one /v1/runs + SSE drain.
	DefaultRunTimeout = 5 * time.Minute
	// adapterName identifies this adapter in logs and the cost tracker.
	adapterName = "hermes"
)

// Endpoint binds either the evaluate step or a playbook's execute
// step to a Hermes instance URL.
type Endpoint struct {
	// BaseURL is the gateway's root, e.g. "http://localhost:8642".
	// No trailing slash; the adapter appends "/v1/runs" etc.
	BaseURL string
	// Model is the optional model identifier sent in the request body.
	// Empty leaves the choice to the Hermes instance's own configuration.
	// Used as the cost-tracker key when non-empty; otherwise "hermes" is
	// used.
	Model string
}

// Config configures the adapter.
type Config struct {
	// EvaluateEndpoint is the Hermes used for the evaluate step. Per
	// the cheap-local-first lock this should point at a fast, cheap
	// Hermes. Required.
	EvaluateEndpoint Endpoint

	// ExecuteEndpoints maps a playbook to its execute Hermes. At
	// least one entry required; an Execute call against a playbook
	// missing from this map errors out — no silent fallback to the
	// evaluate endpoint.
	ExecuteEndpoints map[session.Playbook]Endpoint

	// HTTPClient overrides the default. Optional. SSE streams set
	// per-request timeouts via context; a request-level Timeout on the
	// client would kill the stream prematurely.
	HTTPClient *http.Client

	// AuthToken is sent as Bearer if non-empty. Optional — Hermes
	// authentication is configured via HERMES_API_KEY on the server
	// side.
	AuthToken string

	// RunTimeout bounds the wall-clock for one POST /v1/runs + SSE
	// drain. Defaults to DefaultRunTimeout. Only applied if the caller's
	// context has no deadline of its own.
	RunTimeout time.Duration

	// Tracer is the fat learning-loop sink (per the lean-AP /
	// fat-trace split). Every SSE event also gets pushed through here
	// so the trace record gets the real run timeline. nil-safe.
	Tracer trace.Tracer

	// Cost is the optional cost tracker. When set, run.completed token
	// usage records into the actual + frontier-counterfactual ledgers.
	Cost *cost.Tracker

	// RequireStructured controls how strictly the adapter enforces the
	// structured-output envelope. When true, any run.completed output
	// that cannot be parsed as the documented JSON shape is surfaced
	// as an error (caller wraps it in ExitInhibited). When false (the
	// default), parse failure falls back to a low-confidence
	// return_result placeholder with the raw text in content — useful
	// for dev against weak models or quick smoke tests.
	RequireStructured bool

	// RawEvaluateInstructions overrides the adapter's default evaluate
	// preamble. Most callers should leave this empty.
	RawEvaluateInstructions string

	// RawExecuteInstructions overrides the adapter's default execute
	// preamble per playbook. Most callers should leave this empty.
	RawExecuteInstructions map[session.Playbook]string
}

// Adapter is an adapter.Adapter targeting one Hermes instance per
// playbook for execute and a separate Hermes for evaluate.
type Adapter struct {
	cfg Config
}

// New constructs an adapter. Returns an error for invalid config —
// adapter-construction failures should surface before any runner
// gets a chance to dispatch.
func New(cfg Config) (*Adapter, error) {
	if cfg.EvaluateEndpoint.BaseURL == "" {
		return nil, errors.New("hermes: EvaluateEndpoint.BaseURL required")
	}
	if len(cfg.ExecuteEndpoints) == 0 {
		return nil, errors.New("hermes: at least one ExecuteEndpoints entry required")
	}
	cfg.EvaluateEndpoint.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.EvaluateEndpoint.BaseURL), "/")
	normalized := make(map[session.Playbook]Endpoint, len(cfg.ExecuteEndpoints))
	for pb, ep := range cfg.ExecuteEndpoints {
		trimmed := strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/")
		if trimmed == "" {
			return nil, fmt.Errorf("hermes: ExecuteEndpoints[%q].BaseURL is empty", pb)
		}
		ep.BaseURL = trimmed
		normalized[pb] = ep
	}
	cfg.ExecuteEndpoints = normalized
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{} // no Timeout — SSE owns its own deadline
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = DefaultRunTimeout
	}
	if cfg.Tracer == nil {
		cfg.Tracer = trace.Discard{}
	}
	return &Adapter{cfg: cfg}, nil
}

// SingleEndpoint is a convenience that binds the evaluate step and
// every v0 playbook to one Hermes instance. Useful when developing
// against a single local Hermes (the v0 dev shape, before standing
// up N processes).
func SingleEndpoint(baseURL, model string) Config {
	ep := Endpoint{BaseURL: baseURL, Model: model}
	all := []session.Playbook{
		session.PlaybookPlan,
		session.PlaybookProcess,
		session.PlaybookCritique,
		session.PlaybookVerifyGrounded,
		session.PlaybookCompose,
	}
	exec := make(map[session.Playbook]Endpoint, len(all))
	for _, pb := range all {
		exec[pb] = ep
	}
	return Config{
		EvaluateEndpoint: ep,
		ExecuteEndpoints: exec,
	}
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return adapterName }

// Evaluate implements adapter.Adapter. Posts the AP envelope to the
// configured EvaluateEndpoint, drains the SSE stream, parses the
// JSON response, and stitches env.Evaluate.
func (a *Adapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	ctx, cancel := a.boundContext(ctx)
	if cancel != nil {
		defer cancel()
	}
	instructions := a.cfg.RawEvaluateInstructions
	if instructions == "" {
		instructions = renderEvaluateInstructions(env)
	}
	raw, usage, err := a.oneRun(ctx, a.cfg.EvaluateEndpoint, env, instructions, "evaluate")
	if err != nil {
		return env, err
	}
	a.recordCost(a.cfg.EvaluateEndpoint, usage)

	parsed, structured, err := parseEvaluateResponse(raw)
	if err != nil {
		return env, err
	}
	if !structured && a.cfg.RequireStructured {
		return env, fmt.Errorf("hermes: evaluate produced no structured envelope (raw length %d)", len(raw))
	}
	if !structured {
		// Conservative fallback: default to PlaybookProcess with low
		// confidence. The runner can decide whether to escalate.
		parsed.Playbook = string(session.PlaybookProcess)
		parsed.Confidence = 0.1
		parsed.Rationale = "unstructured evaluate response; fallback to process"
		trace.Info(a.cfg.Tracer, ctx, "hermes_evaluate_unstructured_fallback",
			slog.String("ap_id", string(env.ID)),
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
// playbook chosen by Evaluate, posts the AP envelope, drains the SSE
// stream, parses the playbook-specific JSON response, and stitches
// env.Execute.
func (a *Adapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if env.Evaluate == nil {
		return env, errors.New("hermes: Execute called before Evaluate (env.Evaluate is nil)")
	}
	pb := env.Evaluate.Playbook
	ep, ok := a.cfg.ExecuteEndpoints[pb]
	if !ok {
		return env, fmt.Errorf("hermes: no execute endpoint registered for playbook %q", pb)
	}

	ctx, cancel := a.boundContext(ctx)
	if cancel != nil {
		defer cancel()
	}

	instructions := a.cfg.RawExecuteInstructions[pb]
	if instructions == "" {
		instructions = renderExecuteInstructions(pb, env)
	}
	raw, usage, err := a.oneRun(ctx, ep, env, instructions, "execute")
	if err != nil {
		return env, err
	}
	a.recordCost(ep, usage)

	execute, err := parseExecuteResponse(pb, raw, a.cfg.RequireStructured)
	if err != nil {
		return env, err
	}
	execute.TokensUsed = usage.input + usage.output
	env.Execute = execute
	env.ExitReason = session.ExitDone
	return env, nil
}

// oneRun encapsulates one POST /v1/runs + SSE drain. Returns the raw
// run.completed output, the token usage, and any error.
func (a *Adapter) oneRun(ctx context.Context, ep Endpoint, env session.Envelope, instructions string, phase string) (string, tokenUsage, error) {
	runID, err := a.startRun(ctx, ep, env, instructions, phase)
	if err != nil {
		return "", tokenUsage{}, err
	}
	trace.Info(a.cfg.Tracer, ctx, "hermes_run_started",
		slog.String("ap_id", string(env.ID)),
		slog.String("phase", phase),
		slog.String("playbook", playbookOrEmpty(env)),
		slog.String("run_id", runID),
		slog.String("endpoint", ep.BaseURL),
	)
	raw, usage, err := a.streamEvents(ctx, ep, runID, env, phase)
	if err != nil {
		return "", tokenUsage{}, err
	}
	return raw, usage, nil
}

// startRun POSTs /v1/runs and returns the assigned run_id.
func (a *Adapter) startRun(ctx context.Context, ep Endpoint, env session.Envelope, instructions, phase string) (string, error) {
	body := map[string]any{
		"input": env.Input.Content,
		// Session ID is per-AP per-phase so evaluate and execute don't
		// alias each other within a Hermes that serves both (the
		// SingleEndpoint dev case). Idempotent on retry within a phase.
		"session_id":   string(env.ID) + ":" + phase,
		"instructions": instructions,
	}
	if ep.Model != "" {
		body["model"] = ep.Model
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("hermes: marshal run request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.BaseURL+"/v1/runs", bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("hermes: build run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.AuthToken)
	}

	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("hermes: POST /v1/runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("hermes: POST /v1/runs: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var parsed struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("hermes: decode run response: %w", err)
	}
	if parsed.RunID == "" {
		return "", errors.New("hermes: server returned empty run_id")
	}
	return parsed.RunID, nil
}

// streamEvents drains the SSE event stream and returns the
// run.completed output text + token usage. Terminal events
// (run.completed / run.failed / approval.request / run.cancelled)
// end the drain.
func (a *Adapter) streamEvents(ctx context.Context, ep Endpoint, runID string, env session.Envelope, phase string) (string, tokenUsage, error) {
	url := fmt.Sprintf("%s/v1/runs/%s/events", ep.BaseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", tokenUsage{}, fmt.Errorf("hermes: build events request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if a.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.AuthToken)
	}

	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", tokenUsage{}, fmt.Errorf("hermes: GET events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", tokenUsage{}, fmt.Errorf("hermes: GET events: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var (
		deltaBuf  strings.Builder
		gotFinal  bool
		finalText string
		usage     tokenUsage
	)

	dec := newSSEDecoder(resp.Body)
	for {
		evt, ok, err := dec.next(ctx)
		if err != nil {
			return "", tokenUsage{}, fmt.Errorf("hermes: read event: %w", err)
		}
		if !ok {
			if gotFinal {
				break
			}
			return "", tokenUsage{}, errors.New("hermes: event stream ended without terminal event")
		}

		trace.Info(a.cfg.Tracer, ctx, "hermes_event",
			slog.String("ap_id", string(env.ID)),
			slog.String("phase", phase),
			slog.String("run_id", runID),
			slog.String("event", evt.kind),
		)

		switch evt.kind {
		case "message.delta":
			deltaBuf.WriteString(evt.delta)
		case "reasoning.available":
			trace.Info(a.cfg.Tracer, ctx, "hermes_reasoning",
				slog.String("ap_id", string(env.ID)),
				slog.String("phase", phase),
				slog.String("text", evt.text),
			)
		case "tool.started", "tool.completed":
			// observability only
		case "approval.request":
			trace.Error(a.cfg.Tracer, ctx, "hermes_approval_required",
				slog.String("ap_id", string(env.ID)),
				slog.String("phase", phase),
				slog.String("run_id", runID),
				slog.String("tool", evt.text),
			)
			return "", tokenUsage{}, fmt.Errorf("hermes: run requires approval (tool=%q); v0 adapter does not auto-approve", evt.text)
		case "run.completed":
			gotFinal = true
			finalText = evt.text
			usage = evt.usage
			goto done
		case "run.failed":
			return "", tokenUsage{}, fmt.Errorf("hermes: run failed: %s", evt.text)
		case "run.cancelled":
			return "", tokenUsage{}, errors.New("hermes: run cancelled")
		}
	}
done:

	raw := finalText
	if raw == "" {
		raw = deltaBuf.String()
	}
	return raw, usage, nil
}

func (a *Adapter) boundContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, nil
	}
	return context.WithTimeout(ctx, a.cfg.RunTimeout)
}

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

func playbookOrEmpty(env session.Envelope) string {
	if env.Evaluate == nil {
		return ""
	}
	return string(env.Evaluate.Playbook)
}

// tokenUsage carries the run.completed usage payload.
type tokenUsage struct {
	input  int
	output int
	total  int
}
