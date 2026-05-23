// CLAUDE GENERATED
// Package anthropic provides an adapter.Adapter backed by the
// Anthropic Messages API. Used to drive Oscillitron's call tree
// against a frontier model (or Claude Haiku as a hosted cheap proxy
// during Phase 1 measurement, until real local-model infrastructure
// is in place).
//
// Naming note: pkg/anthropic is the underlying HTTP client; this
// package is the adapter that maps Oscillitron's Evaluate/Execute
// shape onto Messages API calls. They share a name but live at
// different paths.
//
// One adapter instance = one model. Construct separately for
// orchestrator-side (typically Haiku) and frontier-baseline-side
// (typically Sonnet) measurements.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	upstream "github.com/jrlmx2/oscillitron/pkg/anthropic"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Compile-time check.
var _ adapter.Adapter = (*Adapter)(nil)

// Default models. Operators override via Config.Model.
const (
	// DefaultOrchestratorModel — Haiku, the cheap audit tier; appropriate
	// for the orchestrator's per-AP calls during Phase 1.
	DefaultOrchestratorModel = "claude-haiku-4-5-20251001"
	// DefaultFrontierModel — Sonnet, the single-call frontier baseline.
	DefaultFrontierModel = "claude-sonnet-4-6"
	// DefaultMaxTokens caps response length on every call.
	DefaultMaxTokens = 2048
)

// Config configures the adapter.
type Config struct {
	// APIKey is required. Forwarded to pkg/anthropic.Client.
	APIKey string
	// BaseURL overrides the public endpoint. Useful for tests.
	BaseURL string
	// Model is the Anthropic model id used for every call.
	// Defaults to DefaultOrchestratorModel; pass DefaultFrontierModel
	// for the baseline.
	Model string
	// MaxTokens caps response length. Defaults to DefaultMaxTokens.
	MaxTokens int
	// HTTPClient lets callers inject a pre-configured HTTP client
	// (timeouts, proxies, tests).
	HTTPClient *http.Client
	// Name identifies the adapter in logs (default "anthropic-<model>").
	Name string
	// SystemPromptEvaluate / SystemPromptExecute override the built-in
	// preambles. Most callers leave these empty.
	SystemPromptEvaluate string
	SystemPromptExecute  map[session.Playbook]string
}

// Adapter is the adapter.Adapter implementation.
type Adapter struct {
	cfg    Config
	client *upstream.Client
}

// New constructs an Adapter. APIKey is required.
func New(cfg Config) (*Adapter, error) {
	if cfg.Model == "" {
		cfg.Model = DefaultOrchestratorModel
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.Name == "" {
		cfg.Name = "anthropic-" + cfg.Model
	}
	client, err := upstream.New(upstream.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic adapter: %w", err)
	}
	return &Adapter{cfg: cfg, client: client}, nil
}

// Name implements adapter.Adapter.
func (a *Adapter) Name() string { return a.cfg.Name }

// Evaluate implements adapter.Adapter. Calls Anthropic with a
// playbook-pick prompt, parses {playbook, rationale, confidence}.
//
// For Phase 1, the playbook-pick can be deterministic per workload —
// pass SystemPromptEvaluate to pin the choice to "process" or "plan"
// without a real LLM call. The default prompt makes a real Anthropic
// call; useful for workload-agnostic measurement but adds latency.
func (a *Adapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	system := a.cfg.SystemPromptEvaluate
	if system == "" {
		system = defaultEvaluateSystem
	}
	user := renderEvaluateUser(env)
	resp, err := a.client.Messages(ctx, upstream.MessagesRequest{
		Model:     a.cfg.Model,
		MaxTokens: a.cfg.MaxTokens,
		System:    system,
		Messages:  []upstream.Message{{Role: "user", Content: user}},
	})
	if err != nil {
		return env, fmt.Errorf("anthropic adapter evaluate: %w", err)
	}
	pb, rationale, confidence, perr := parseEvaluateResponse(resp.FirstText())
	if perr != nil {
		return env, fmt.Errorf("anthropic adapter evaluate parse: %w (raw=%q)", perr, resp.FirstText())
	}
	env.Evaluate = &session.Evaluate{
		Playbook:   pb,
		Rationale:  rationale,
		Confidence: confidence,
		TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	return env, nil
}

// Execute implements adapter.Adapter. Dispatches on env.Evaluate.Playbook
// and produces the matching Execute payload.
func (a *Adapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if env.Evaluate == nil {
		return env, fmt.Errorf("anthropic adapter: Execute called before Evaluate")
	}
	pb := env.Evaluate.Playbook
	system := a.cfg.SystemPromptExecute[pb]
	if system == "" {
		system = defaultExecuteSystem[pb]
		if system == "" {
			return env, fmt.Errorf("anthropic adapter: no system prompt for playbook %q", pb)
		}
	}
	user := renderExecuteUser(pb, env)
	resp, err := a.client.Messages(ctx, upstream.MessagesRequest{
		Model:     a.cfg.Model,
		MaxTokens: a.cfg.MaxTokens,
		System:    system,
		Messages:  []upstream.Message{{Role: "user", Content: user}},
	})
	if err != nil {
		return env, fmt.Errorf("anthropic adapter execute %s: %w", pb, err)
	}
	tokens := resp.Usage.InputTokens + resp.Usage.OutputTokens
	exec, perr := parseExecuteResponse(pb, resp.FirstText(), tokens)
	if perr != nil {
		return env, fmt.Errorf("anthropic adapter execute %s parse: %w (raw=%q)", pb, perr, resp.FirstText())
	}
	env.Execute = exec
	env.ExitReason = session.ExitDone
	return env, nil
}

// --- evaluate prompts + parsing ---

const defaultEvaluateSystem = `You are the dispatcher for a multi-agent system. ` +
	`Read the user's input and pick which playbook should run. Reply with a single JSON object — no prose, no markdown:` + "\n" +
	`{"playbook": "plan" | "process" | "critique" | "verify_grounded" | "compose", "rationale": "<short>", "confidence": <0.0..1.0>}` + "\n" +
	`Guidance: pick "plan" for tasks that need decomposition (multiple steps); pick "process" for single direct work; pick "critique" when the input is asking you to review a prior result; pick "verify_grounded" when there is a check spec to run; pick "compose" when reducing sibling results.`

func renderEvaluateUser(env session.Envelope) string {
	return "AP input:\n" + env.Input.Content + "\n\nReply with the JSON object only."
}

func parseEvaluateResponse(raw string) (session.Playbook, string, float64, error) {
	body := extractFirstJSONObject(raw)
	if body == "" {
		return "", "", 0, fmt.Errorf("no JSON object found")
	}
	var parsed struct {
		Playbook   string  `json:"playbook"`
		Rationale  string  `json:"rationale"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", "", 0, err
	}
	pbStr := strings.ToLower(strings.TrimSpace(parsed.Playbook))
	switch pbStr {
	case "plan", "process", "critique", "verify_grounded", "compose":
		// valid
	default:
		return "", "", 0, fmt.Errorf("unknown playbook %q", parsed.Playbook)
	}
	return session.Playbook(pbStr), parsed.Rationale, parsed.Confidence, nil
}

// --- execute prompts + parsing ---

// defaultExecuteSystem maps playbook → system prompt. The prompts are
// deliberately terse — the per-AP envelope.Input.Content already
// contains the task-specific instructions. The system prompt locks
// the *output shape* for parsing.
var defaultExecuteSystem = map[session.Playbook]string{
	session.PlaybookPlan: `You are a planner. Decompose the user's input into sub-tasks. ` +
		`Return JSON — no prose, no markdown:` + "\n" +
		`{"sub_aps": [{"input": "<task>", "needs_verification": <bool>}], "recompose": "sequential" | "pairwise" | "none"}`,
	session.PlaybookProcess: `You are a worker. Perform the task in the user's input directly. ` +
		`Return JSON — no prose, no markdown:` + "\n" +
		`{"content": "<your result>", "confidence": <0.0..1.0>}`,
	session.PlaybookCritique: `You are a reviewer. Read the prior result and return your verdict. ` +
		`Return JSON — no prose, no markdown:` + "\n" +
		`{"verdict": "pass" | "fail" | "issues", "issues": [{"severity": "info"|"warning"|"error", "what": "..."}]}`,
	session.PlaybookVerifyGrounded: `You verify outputs against a check spec. ` +
		`Return JSON — no prose, no markdown:` + "\n" +
		`{"verdict": "pass" | "fail" | "issues", "issues": [{"severity": "info"|"warning"|"error", "what": "..."}]}`,
	session.PlaybookCompose: `You combine sibling results into a single coherent output. ` +
		`Return JSON — no prose, no markdown:` + "\n" +
		`{"content": "<combined>", "confidence": <0.0..1.0>}`,
}

func renderExecuteUser(pb session.Playbook, env session.Envelope) string {
	var b strings.Builder
	b.WriteString("AP input:\n")
	b.WriteString(env.Input.Content)
	if env.OutputSchema != "" {
		b.WriteString("\n\nOutput schema hint:\n")
		b.WriteString(env.OutputSchema)
	}
	b.WriteString("\n\nReply with the JSON object only.")
	return b.String()
}

func parseExecuteResponse(pb session.Playbook, raw string, tokensUsed int) (*session.Execute, error) {
	body := extractFirstJSONObject(raw)
	if body == "" {
		return nil, fmt.Errorf("no JSON object found")
	}
	switch pb {
	case session.PlaybookPlan:
		var p struct {
			SubAPs []struct {
				Input             string `json:"input"`
				OutputSchema      string `json:"output_schema,omitempty"`
				NeedsVerification bool   `json:"needs_verification,omitempty"`
			} `json:"sub_aps"`
			Recompose string `json:"recompose"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			return nil, err
		}
		seeds := make([]session.SubAPSeed, 0, len(p.SubAPs))
		for _, s := range p.SubAPs {
			seeds = append(seeds, session.SubAPSeed{
				Input:             session.Payload{Kind: "task", Content: s.Input},
				OutputSchema:      s.OutputSchema,
				NeedsVerification: s.NeedsVerification,
			})
		}
		recompose := session.RecomposeSequential
		switch strings.ToLower(strings.TrimSpace(p.Recompose)) {
		case "pairwise":
			recompose = session.RecomposePairwise
		case "none":
			recompose = session.RecomposeNone
		}
		return &session.Execute{
			Category:    session.CategoryEmitSubtree,
			EmitSubtree: &session.EmitSubtreePayload{SubAPs: seeds, Recompose: recompose},
			TokensUsed:  tokensUsed,
		}, nil
	case session.PlaybookProcess, session.PlaybookCompose:
		var p struct {
			Content    string  `json:"content"`
			Confidence float64 `json:"confidence"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			return nil, err
		}
		if p.Confidence < 0 {
			p.Confidence = 0
		} else if p.Confidence > 1 {
			p.Confidence = 1
		}
		return &session.Execute{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result:     session.Payload{Kind: "result", Content: p.Content},
				Confidence: p.Confidence,
			},
			TokensUsed: tokensUsed,
		}, nil
	case session.PlaybookCritique, session.PlaybookVerifyGrounded:
		var p struct {
			Verdict string `json:"verdict"`
			Issues  []struct {
				Severity string `json:"severity"`
				What     string `json:"what"`
				Where    string `json:"where,omitempty"`
			} `json:"issues"`
		}
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			return nil, err
		}
		verdict, err := parseVerdict(p.Verdict)
		if err != nil {
			return nil, err
		}
		issues := make([]session.Issue, 0, len(p.Issues))
		for _, is := range p.Issues {
			sev, _ := parseSeverity(is.Severity)
			issues = append(issues, session.Issue{Severity: sev, Where: is.Where, What: is.What})
		}
		return &session.Execute{
			Category:       session.CategoryVerifierSignal,
			VerifierSignal: &session.VerifierSignalPayload{Verdict: verdict, Issues: issues},
			TokensUsed:     tokensUsed,
		}, nil
	default:
		return nil, fmt.Errorf("unknown playbook %q", pb)
	}
}

func parseVerdict(s string) (session.Verdict, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pass":
		return session.VerdictPass, nil
	case "fail":
		return session.VerdictFail, nil
	case "issues":
		return session.VerdictIssues, nil
	}
	return "", fmt.Errorf("unknown verdict %q", s)
}

func parseSeverity(s string) (session.Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return session.SeverityInfo, nil
	case "warning", "warn":
		return session.SeverityWarning, nil
	case "error", "err":
		return session.SeverityError, nil
	}
	return session.SeverityInfo, fmt.Errorf("unknown severity %q", s)
}

// extractFirstJSONObject is the same brace-balancing scanner used by
// pkg/judge and pkg/recomposer. Tolerates markdown-fenced JSON.
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
