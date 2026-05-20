//go:build hermes_stage5

// CLAUDE GENERATED
// Package hermes implements adapter.Adapter against the
// OpenAI-compatible HTTP gateway shipped by Nous Research's
// hermes-agent (see gateway/platforms/api_server.py in that repo).
//
// Architecture (per parent CLAUDE.md, "Architecture", locked
// 2026-05-18):
//
//   - One long-lived Hermes instance per brain function. Each
//     instance is the *specialist* — the persistent learning
//     substrate (skills, memory, retrieval shards) that grows over
//     time within its cognitive niche.
//   - Each AP invocation is one *session* within that specialist's
//     Hermes. session_id maps to the envelope ID, so per-invocation
//     working memory stays isolated even though the long-term
//     specialist store persists across invocations.
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

// Endpoint binds a brain function to a Hermes instance URL.
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
	// Endpoints maps a brain function to its Hermes instance. At least
	// one entry required. Unknown brain functions cause Call to return
	// an error rather than falling back silently.
	Endpoints map[session.BrainFunction]Endpoint

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
	// default), parse failure falls back to placing the raw text in
	// Output.Content with zero-valued structured fields — useful for
	// dev against weak models or quick smoke tests.
	RequireStructured bool

	// RawInstructions overrides the adapter's default instructions
	// preamble. Most callers should leave this empty — the default
	// preamble teaches Hermes to emit the structured envelope the
	// adapter parses. Set this only when running against a
	// pre-configured Hermes that already knows the output contract.
	RawInstructions string
}

// Adapter is an adapter.Adapter targeting one Hermes instance per
// brain function.
type Adapter struct {
	cfg Config
}

// New constructs an adapter. Returns an error for invalid config —
// adapter-construction failures should surface before any oscillator
// gets a chance to dispatch.
func New(cfg Config) (*Adapter, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, errors.New("hermes: at least one endpoint required")
	}
	normalized := make(map[session.BrainFunction]Endpoint, len(cfg.Endpoints))
	for bf, ep := range cfg.Endpoints {
		trimmed := strings.TrimRight(strings.TrimSpace(ep.BaseURL), "/")
		if trimmed == "" {
			return nil, fmt.Errorf("hermes: endpoint for %q has empty BaseURL", bf)
		}
		ep.BaseURL = trimmed
		normalized[bf] = ep
	}
	cfg.Endpoints = normalized
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

// SingleEndpoint is a convenience that binds every well-known brain
// function to one Hermes instance. Useful when developing against a
// single local Hermes (the v0 dev shape, before standing up N
// processes).
func SingleEndpoint(baseURL, model string) Config {
	all := []session.BrainFunction{
		session.BrainPerception,
		session.BrainRetrieval,
		session.BrainPlanning,
		session.BrainReasoning,
		session.BrainCritic,
		session.BrainComposition,
	}
	eps := make(map[session.BrainFunction]Endpoint, len(all))
	for _, bf := range all {
		eps[bf] = Endpoint{BaseURL: baseURL, Model: model}
	}
	return Config{Endpoints: eps}
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return adapterName }

// Call implements adapter.Adapter — POSTs /v1/runs, drains the SSE
// event stream until run.completed / run.failed / approval.request /
// run.cancelled, then returns the assembled Output.
func (a *Adapter) Call(ctx context.Context, env session.Envelope) (session.Output, error) {
	ep, ok := a.cfg.Endpoints[env.BrainFunction]
	if !ok {
		return session.Output{}, fmt.Errorf("hermes: no endpoint registered for brain function %q", env.BrainFunction)
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.cfg.RunTimeout)
		defer cancel()
	}

	runID, err := a.startRun(ctx, ep, env)
	if err != nil {
		return session.Output{}, err
	}
	trace.Info(a.cfg.Tracer, ctx, "hermes_run_started",
		slog.String("session", string(env.ID)),
		slog.String("brain_function", string(env.BrainFunction)),
		slog.String("run_id", runID),
		slog.String("endpoint", ep.BaseURL),
	)

	out, err := a.streamEvents(ctx, ep, runID, env)
	if err != nil {
		return session.Output{}, err
	}
	return out, nil
}

// startRun POSTs /v1/runs and returns the assigned run_id.
func (a *Adapter) startRun(ctx context.Context, ep Endpoint, env session.Envelope) (string, error) {
	body := map[string]any{
		// "input" is the user message Hermes will route through its
		// conversation loop. We pass the AP's Input.Content verbatim;
		// upstream prompting (OutputSchema, classification preamble) is
		// supplied as "instructions" so the substrate sees a clean
		// system / user split.
		"input": env.Input.Content,
		// "session_id" — within a specialist's Hermes, sessions are
		// per-invocation. Reusing the envelope ID gives idempotency on
		// retry and makes Hermes' own session search find the right
		// trace later.
		"session_id": string(env.ID),
	}
	// Instructions tell Hermes how to format its reply so the adapter
	// can re-derive structured Output fields. Callers can override by
	// setting Config.RawInstructions when the substrate is already
	// configured for a different output contract.
	if a.cfg.RawInstructions != "" {
		body["instructions"] = a.cfg.RawInstructions
	} else {
		body["instructions"] = renderInstructions(env.OutputSchema)
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
		// Hermes returns 202 on success per api_server.py; tolerate 200
		// for OpenAI-compat frontends that prefer it.
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

// streamEvents drains the SSE event stream and assembles the final
// Output. Terminal events (run.completed / run.failed / approval.request
// / run.cancelled) end the drain.
func (a *Adapter) streamEvents(ctx context.Context, ep Endpoint, runID string, env session.Envelope) (session.Output, error) {
	url := fmt.Sprintf("%s/v1/runs/%s/events", ep.BaseURL, runID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return session.Output{}, fmt.Errorf("hermes: build events request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if a.cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.cfg.AuthToken)
	}

	resp, err := a.cfg.HTTPClient.Do(req)
	if err != nil {
		return session.Output{}, fmt.Errorf("hermes: GET events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return session.Output{}, fmt.Errorf("hermes: GET events: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
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
			return session.Output{}, fmt.Errorf("hermes: read event: %w", err)
		}
		if !ok {
			// Stream closed without a terminal event. Treat as failure
			// — Hermes' sentinel is the `: stream closed` comment which
			// the decoder swallows; any normal completion would have
			// emitted run.completed before close.
			if gotFinal {
				break
			}
			return session.Output{}, errors.New("hermes: event stream ended without terminal event")
		}

		trace.Info(a.cfg.Tracer, ctx, "hermes_event",
			slog.String("session", string(env.ID)),
			slog.String("run_id", runID),
			slog.String("event", evt.kind),
		)

		switch evt.kind {
		case "message.delta":
			deltaBuf.WriteString(evt.delta)
		case "reasoning.available":
			// Surface as a signal — the inhibitor's signals channel is
			// where unstructured reasoning notes belong.
			trace.Info(a.cfg.Tracer, ctx, "hermes_reasoning",
				slog.String("session", string(env.ID)),
				slog.String("text", evt.text),
			)
		case "tool.started", "tool.completed":
			// Pure observability — the trace event above is the record.
		case "approval.request":
			// v0: orchestrator owns gating; an approval-required run
			// is treated as inhibited. Surfacing as an error lets the
			// oscillator wrap it in an ExitInhibited Output.
			trace.Error(a.cfg.Tracer, ctx, "hermes_approval_required",
				slog.String("session", string(env.ID)),
				slog.String("run_id", runID),
				slog.String("tool", evt.text),
			)
			return session.Output{}, fmt.Errorf("hermes: run requires approval (tool=%q); v0 adapter does not auto-approve", evt.text)
		case "run.completed":
			gotFinal = true
			finalText = evt.text
			usage = evt.usage
			// Break immediately. The server normally closes the stream
			// right after run.completed, but draining further keeps
			// the HTTP connection open for no benefit and would
			// process any (unexpected) late events.
			goto done
		case "run.failed":
			return session.Output{}, fmt.Errorf("hermes: run failed: %s", evt.text)
		case "run.cancelled":
			return session.Output{}, errors.New("hermes: run cancelled")
		}
	}
done:

	// Prefer the run.completed "output" field over the streamed
	// message.delta accumulation — message.delta carries provider
	// tokens which can include tool-call artifacts; run.completed
	// carries the final assistant response Hermes itself decided was
	// the answer.
	raw := finalText
	if raw == "" {
		raw = deltaBuf.String()
	}

	if a.cfg.Cost != nil && (usage.input > 0 || usage.output > 0) {
		model := ep.Model
		if model == "" {
			model = adapterName
		}
		a.cfg.Cost.Record(model, usage.input, usage.output)
	}

	payload, structured, err := extractStructured(raw)
	if err != nil {
		// JSON-looking but unparseable — protocol violation.
		return session.Output{}, err
	}
	if !structured && a.cfg.RequireStructured {
		return session.Output{}, fmt.Errorf("hermes: run produced no structured envelope (raw length %d)", len(raw))
	}
	out := payload.toOutput()
	out.ExitReason = session.ExitDone
	if !structured {
		trace.Info(a.cfg.Tracer, ctx, "hermes_unstructured_fallback",
			slog.String("session", string(env.ID)),
			slog.String("run_id", runID),
			slog.Int("raw_length", len(raw)),
		)
	}
	return out, nil
}

// tokenUsage carries the run.completed usage payload.
type tokenUsage struct {
	input  int
	output int
	total  int
}
