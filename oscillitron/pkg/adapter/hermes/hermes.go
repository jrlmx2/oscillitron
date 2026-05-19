// CLAUDE GENERATED
// Package hermes is the production adapter: it supervises one
// long-lived Hermes process per oscillator and talks to it over the
// Agent Client Protocol (acp subpackage). One persistent ACP session
// per Adapter; a mutex serializes APs through that session so Hermes'
// skill/memory updates accrue deterministically per "brain region"
// (CLAUDE.md, locked 2026-05-18).
//
// Thin first cut — scope is deliberately tight:
//   - One specialist per Adapter instance.
//   - Stdio supervision: this package owns the os/exec.Cmd lifecycle.
//   - AP -> single text ContentBlock; assistant chunks -> Outcome.Verdict.
//   - No mapping of Signals/Confidence/OpenQuestions/Contradictions yet —
//     ACP doesn't natively express them; that's a follow-up.
//   - No cost/token accounting yet — also follow-up.
//
// What's explicitly NOT here (deferred): MCP server config, auth flows,
// session resume, model switching, structured output parsing.
package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes/acp"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Default timeout floors. Picked to give Hermes enough runway for its
// internal staging (process spawn, ACP handshake, first-token latency
// against a cold model) before any caller-side deadline kicks in. Set
// these too low and we cancel Hermes mid-flight — wasting a round
// trip AND surfacing a confusing "client closed" error instead of
// a clear "your deadline was too short for this backend" signal.
const (
	DefaultMinConnectionTimeout = 30 * time.Second
	DefaultMinCallTimeout       = 30 * time.Second
)

// Config configures one Hermes specialist.
type Config struct {
	// Name is the oscillator name, used for logs and Adapter.Name().
	Name string
	// BinPath is the path to the Hermes ACP-server executable. Passed
	// directly to exec.Command — pre-configured wrappers (e.g. a shell
	// script that activates a venv) are fine.
	BinPath string
	// Args are extra arguments appended to the Hermes invocation.
	Args []string
	// Cwd is the working directory for the Hermes process. It's also
	// passed as session/new's `cwd` field. Required to be absolute per
	// ACP convention.
	Cwd string
	// Stderr, if non-nil, is where the Hermes process's stderr is copied.
	// Defaults to os.Stderr so the user sees Hermes panics.
	Stderr io.Writer
	// MaxContextTokens is the hard ceiling for a single AP's prompt.
	// Estimated client-side (chars/4); if a prompt exceeds it, Call
	// short-circuits with ExitInhibited rather than handing oversized
	// input to Hermes (which would silently truncate via its
	// compression pipeline, hiding the real problem).
	//
	// Zero disables the check. Set this to the model's native context
	// window minus a safety margin for system-prompt + scaffolds
	// (Hermes typically reserves ~25% for its own framing).
	MaxContextTokens int
	// MinConnectionTimeout is the floor for the New() handshake ctx.
	// If the caller's ctx has a deadline shorter than this floor, New
	// returns an error immediately rather than starting a process that
	// will be killed mid-initialize. Zero falls back to
	// DefaultMinConnectionTimeout.
	MinConnectionTimeout time.Duration
	// MinCallTimeout is the floor for every Call's ctx. If the caller's
	// ctx has a deadline shorter than this floor at Call entry, Call
	// short-circuits with ExitInhibited rather than firing a prompt
	// into Hermes that we know we'll have to cancel. Zero falls back
	// to DefaultMinCallTimeout. Set to a negative value to disable the
	// check.
	MinCallTimeout time.Duration
}

// Adapter is the production Hermes-wrapping Adapter.
//
// Lifecycle: New starts the process and runs the initialize +
// session/new handshake. Close terminates the process. Call sends one
// AP as a session/prompt turn and waits for the assistant's stop_reason,
// accumulating message chunks into Outcome.Verdict.
type Adapter struct {
	name             string
	maxContextTokens int
	minCallTimeout   time.Duration // resolved at New (0 = default, negative = disabled)
	cmd              *exec.Cmd
	client           *acp.Client
	sessionID        string

	// callMu serializes APs through the persistent ACP session. This
	// is the per-region lock from CLAUDE.md: skill/memory updates inside
	// Hermes accrue in deterministic order rather than racing.
	callMu sync.Mutex

	// chunkSink is set per-call (under callMu) so the notification
	// handler — which runs on the acp client's read goroutine — can
	// route assistant text chunks to the right collector. nil between
	// calls; chunks arriving outside a call are dropped.
	chunkMu   sync.Mutex
	chunkSink *strings.Builder
}

// roughTokens is a client-side token estimate. ACP doesn't expose
// per-turn input/output token counts (its `usage_update` carries
// {size, used} for context-bar UI, not per-call accounting). Hermes
// itself knows the real counts but doesn't surface them through ACP.
// chars/4 is the standard rough estimate for English-leaning content;
// will under-count for code (longer tokens) and over-count for very
// short strings. Good enough for the frontier-counterfactual cost
// ratio, which is what drives the Phase 1 GTM story.
func roughTokens(s string) int {
	return (len(s) + 3) / 4
}

// New spawns a Hermes process per cfg and performs the ACP handshake.
// On any error, the process is torn down before return.
//
// The supplied ctx bounds the initialize + session/new handshake only.
// The Hermes process itself is started under context.Background() so
// its lifetime is managed by Adapter.Close(), not by the caller's
// short-lived setup ctx. Callers can pass a startup-deadline context
// without worrying about it killing the process mid-call later.
func New(ctx context.Context, cfg Config) (*Adapter, error) {
	if cfg.Name == "" {
		return nil, errors.New("hermes: Config.Name is required")
	}
	if cfg.BinPath == "" {
		return nil, errors.New("hermes: Config.BinPath is required")
	}
	if cfg.Cwd == "" {
		return nil, errors.New("hermes: Config.Cwd is required (must be absolute)")
	}
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	// Resolve timeout floors: zero -> default, negative -> disabled.
	minConn := cfg.MinConnectionTimeout
	if minConn == 0 {
		minConn = DefaultMinConnectionTimeout
	}
	minCall := cfg.MinCallTimeout
	if minCall == 0 {
		minCall = DefaultMinCallTimeout
	}

	// Refuse to start if the caller's setup ctx is already too tight.
	// Better to fail here with a clear message than to spawn a process,
	// fire initialize, and have it killed mid-handshake.
	if minConn > 0 {
		if dl, ok := ctx.Deadline(); ok {
			remaining := time.Until(dl)
			if remaining < minConn {
				return nil, fmt.Errorf(
					"hermes: setup ctx has %v remaining; need at least %v for handshake "+
						"(set MinConnectionTimeout / OSCILLITRON_HERMES_MIN_CONNECTION_TIMEOUT "+
						"to override or pass a longer ctx)",
					remaining, minConn)
			}
		}
	}

	// Process lifetime: bound to Adapter, not to the caller's setup ctx.
	// Adapter.Close() runs Process.Kill() + Wait() to tear down.
	cmd := exec.Command(cfg.BinPath, cfg.Args...)
	cmd.Dir = cfg.Cwd
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("hermes: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("hermes: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hermes: start %q: %w", cfg.BinPath, err)
	}

	a := &Adapter{
		name:             cfg.Name,
		maxContextTokens: cfg.MaxContextTokens,
		minCallTimeout:   minCall,
		cmd:              cmd,
	}
	a.client = acp.NewClient(stdout, stdin, a.onNotification)

	if err := a.handshake(ctx, cfg.Cwd); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

func (a *Adapter) handshake(ctx context.Context, cwd string) error {
	if _, err := a.client.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.CurrentProtocolVersion,
		ClientInfo: &acp.Implementation{
			Name:    "oscillitron",
			Version: "0.0.1",
		},
		ClientCapabilities: acp.ClientCapabilities{},
	}); err != nil {
		return fmt.Errorf("hermes: initialize: %w", err)
	}
	resp, err := a.client.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []json.RawMessage{},
	})
	if err != nil {
		return fmt.Errorf("hermes: session/new: %w", err)
	}
	a.sessionID = resp.SessionID
	return nil
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return a.name }

// MinCallTimeout implements adapter.MinTimeoutAdvisory. Returns the
// effective floor or 0 if disabled. The runner reads this at startup
// to warn when a chain-level timeout is too short for this adapter.
func (a *Adapter) MinCallTimeout() time.Duration {
	if a.minCallTimeout < 0 {
		return 0
	}
	return a.minCallTimeout
}

// Call implements adapter.Adapter. Serializes through the per-region
// mutex so Hermes' skill/memory updates accrue in AP-order.
func (a *Adapter) Call(ctx context.Context, env session.Envelope) (session.Outcome, error) {
	// Refuse early if the caller's ctx deadline is shorter than the
	// per-Call floor. Doing this BEFORE acquiring callMu avoids a
	// queue of doomed calls piling up under contention.
	if a.minCallTimeout > 0 {
		if dl, ok := ctx.Deadline(); ok {
			remaining := time.Until(dl)
			if remaining < a.minCallTimeout {
				return session.Outcome{
					ExitReason: session.ExitInhibited,
					Verdict: fmt.Sprintf(
						"ctx has %v remaining; need at least %v for %s",
						remaining, a.minCallTimeout, a.name),
					Signals: []string{"prompt_timeout_too_short"},
				}, nil
			}
		}
	}

	a.callMu.Lock()
	defer a.callMu.Unlock()

	var sink strings.Builder
	a.chunkMu.Lock()
	a.chunkSink = &sink
	a.chunkMu.Unlock()
	defer func() {
		a.chunkMu.Lock()
		a.chunkSink = nil
		a.chunkMu.Unlock()
	}()

	promptText := renderAP(env)
	if a.maxContextTokens > 0 {
		estIn := roughTokens(promptText)
		if estIn > a.maxContextTokens {
			// Refuse the call rather than letting Hermes silently truncate.
			// Surfaced as ExitInhibited so the orchestrator can decide
			// whether to re-decompose or abort the chain.
			return session.Outcome{
				ExitReason: session.ExitInhibited,
				Verdict: fmt.Sprintf(
					"prompt of ~%d tokens exceeds configured max of %d for %s",
					estIn, a.maxContextTokens, a.name),
				Signals:        []string{"prompt_exceeds_max_context"},
				Contradictions: []string{"upstream produced oversized AP"},
				TokensInput:    estIn,
				TokensOutput:   0,
			}, nil
		}
	}
	resp, err := a.client.Prompt(ctx, acp.PromptRequest{
		SessionID: a.sessionID,
		Prompt: []acp.ContentBlock{{
			Type: "text",
			Text: promptText,
		}},
	})
	if err != nil {
		// Context cancellation is reported as inhibition rather than
		// adapter failure — the orchestrator decides what to do next.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			partial, _ := extractStructured(sink.String())
			return session.Outcome{
				ExitReason:   session.ExitInhibited,
				Verdict:      partial,
				Signals:      []string{"context_canceled"},
				TokensInput:  roughTokens(promptText),
				TokensOutput: roughTokens(partial),
			}, nil
		}
		return session.Outcome{}, err
	}

	exit := session.ExitDone
	switch resp.StopReason {
	case acp.StopReasonCancelled, acp.StopReasonRefusal:
		exit = session.ExitInhibited
	case acp.StopReasonToolUse:
		// Tool-use mid-turn isn't terminal from ACP's view, but the thin
		// cut doesn't service tool calls — surface as inhibited so the
		// caller knows something interrupted.
		exit = session.ExitInhibited
	}
	rawOutput := sink.String()
	verdict, block := extractStructured(rawOutput)
	// If the model put its entire response inside the JSON block (no
	// preamble text), verdict ends up empty after extraction. Fall back
	// to the raw output so downstream consumers still get the answer.
	if verdict == "" {
		verdict = rawOutput
	}
	outcome := session.Outcome{
		ExitReason:     exit,
		Verdict:        verdict,
		Signals:        block.Signals,
		OpenQuestions:  block.OpenQuestions,
		Contradictions: block.Contradictions,
		TokensInput:    roughTokens(promptText),
		TokensOutput:   roughTokens(rawOutput),
	}
	if block.Confidence != nil {
		outcome.Confidence = *block.Confidence
	}
	return outcome, nil
}

// Close terminates the Hermes process. Safe to call multiple times.
func (a *Adapter) Close() error {
	if a.client != nil {
		_ = a.client.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
		_ = a.cmd.Wait()
	}
	return nil
}

// onNotification handles agent->client notifications. The only one
// we currently consume is session/update carrying agent_message_chunk.
// Other variants (usage_update, available_commands_update, plan, ...)
// are ignored — usage_update in particular is context-bar metering,
// not per-turn token accounting.
func (a *Adapter) onNotification(method string, params json.RawMessage) {
	if method != "session/update" {
		return
	}
	var note acp.SessionNotification
	if err := json.Unmarshal(params, &note); err != nil {
		return
	}
	var chunk acp.AgentMessageChunk
	if err := json.Unmarshal(note.Update, &chunk); err != nil {
		return
	}
	if chunk.SessionUpdate != "agent_message_chunk" {
		return
	}
	a.chunkMu.Lock()
	sink := a.chunkSink
	a.chunkMu.Unlock()
	if sink == nil {
		return
	}
	sink.WriteString(chunk.Content.Text)
}

// renderAP converts the AP envelope into the text payload Hermes sees.
// Appends outputFormatInstruction so the response carries a trailing
// fenced JSON block for the structured Outcome fields. Verdict text
// (free-form) and the structured fields share one turn.
func renderAP(env session.Envelope) string {
	var b strings.Builder
	if env.Objective != "" {
		b.WriteString("Objective: ")
		b.WriteString(env.Objective)
		b.WriteString("\n\n")
	}
	if env.Input.Content != "" {
		b.WriteString(env.Input.Content)
	}
	b.WriteString(outputFormatInstruction)
	return b.String()
}

var _ adapter.Adapter = (*Adapter)(nil)
