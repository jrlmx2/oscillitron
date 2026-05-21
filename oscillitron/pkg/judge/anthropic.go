// CLAUDE GENERATED
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/anthropic"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// DefaultAnthropicModel is the default frontier model the Anthropic
// Judge points at. Haiku is the cheap-but-capable option appropriate
// for the audit layer — the verifier-policy sampler keeps the call
// volume bounded so a slightly stronger model is affordable.
const DefaultAnthropicModel = "claude-haiku-4-5-20251001"

// DefaultMaxTokens caps the Judge's reply length. Verdicts + issues
// fit comfortably in 512.
const DefaultMaxTokens = 512

// AnthropicJudge is a Judge implementation backed by the Anthropic
// Messages API. It calls the frontier model with a structured-output
// prompt and parses {verdict, issues} from the response.
//
// Configuration:
//
//   - APIKey is sent as x-api-key on every call. Set via the env var
//     ANTHROPIC_API_KEY by your caller — this package does not read
//     the environment itself (callers wire it explicitly so tests can
//     stub).
//   - Model defaults to DefaultAnthropicModel; override for stronger
//     audits at the cost of higher per-call spend.
//   - SystemPrompt overrides the built-in verifier instructions; most
//     callers should leave it empty.
type AnthropicJudge struct {
	client       *anthropic.Client
	model        string
	maxTokens    int
	systemPrompt string
}

// AnthropicConfig configures a new AnthropicJudge.
type AnthropicConfig struct {
	APIKey       string
	BaseURL      string       // optional; tests pass an httptest server URL
	Model        string       // optional; defaults to DefaultAnthropicModel
	MaxTokens    int          // optional; defaults to DefaultMaxTokens
	SystemPrompt string       // optional; overrides the built-in verifier preamble
	HTTPClient   *http.Client // optional; forwarded to anthropic.Client for timeouts / proxies / tests
}

// NewAnthropic constructs an AnthropicJudge. APIKey is required.
func NewAnthropic(cfg AnthropicConfig) (*AnthropicJudge, error) {
	clientCfg := anthropic.Config{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		HTTPClient: cfg.HTTPClient,
	}
	client, err := anthropic.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("judge: %w", err)
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
		systemPrompt = defaultJudgeSystemPrompt
	}
	return &AnthropicJudge{
		client:       client,
		model:        model,
		maxTokens:    maxTokens,
		systemPrompt: systemPrompt,
	}, nil
}

const defaultJudgeSystemPrompt = `You are a frontier verifier auditing a local critic's verdict on an AP output. ` +
	`Read the AP's input and produced result, plus the local critic's verdict and issues. ` +
	`Return your independent verdict as a single JSON object with this exact shape — no prose, no markdown:` + "\n" +
	`{"verdict": "pass" | "fail" | "issues", "issues": [{"severity": "info"|"warning"|"error", "what": "..."}]}` + "\n" +
	`If the local critic was right, repeat their verdict. If not, disagree and explain in issues[]. Issues may be empty.`

// Name implements Judge.
func (j *AnthropicJudge) Name() string { return "anthropic" }

// Judge implements Judge. Builds a structured-output prompt from the
// Request, calls the Anthropic API, and parses {verdict, issues} from
// the response. Returns an error if the response is unparseable —
// the runner treats this as "no audit signal for this AP" rather
// than failing the run.
func (j *AnthropicJudge) Judge(ctx context.Context, req Request) (Response, error) {
	userMsg := renderJudgeUserMessage(req)
	resp, err := j.client.Messages(ctx, anthropic.MessagesRequest{
		Model:     j.model,
		MaxTokens: j.maxTokens,
		System:    j.systemPrompt,
		Messages:  []anthropic.Message{{Role: "user", Content: userMsg}},
	})
	if err != nil {
		return Response{}, fmt.Errorf("judge: anthropic call: %w", err)
	}
	text := resp.FirstText()
	verdict, issues, perr := parseJudgeResponse(text)
	if perr != nil {
		return Response{}, fmt.Errorf("judge: parse response: %w (raw=%q)", perr, text)
	}
	return Response{
		Verdict:    verdict,
		Issues:     issues,
		TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}, nil
}

// renderJudgeUserMessage builds the user message body sent to the
// model. Keep it compact — the system prompt does the heavy lifting.
func renderJudgeUserMessage(req Request) string {
	var b strings.Builder
	b.WriteString("AP input:\n")
	b.WriteString(req.Target.Input.Content)
	b.WriteString("\n\nAP result:\n")
	if req.Target.Execute != nil && req.Target.Execute.ReturnResult != nil {
		b.WriteString(req.Target.Execute.ReturnResult.Result.Content)
	}
	b.WriteString("\n\nLocal critic verdict: ")
	b.WriteString(string(req.LocalVerdict))
	if len(req.LocalIssues) > 0 {
		b.WriteString("\nLocal critic issues:\n")
		for _, is := range req.LocalIssues {
			b.WriteString("- [")
			b.WriteString(string(is.Severity))
			b.WriteString("] ")
			b.WriteString(is.What)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nReply with the JSON object only.")
	return b.String()
}

// judgeResponseShape is the expected JSON shape from the model.
type judgeResponseShape struct {
	Verdict string `json:"verdict"`
	Issues  []struct {
		Severity string `json:"severity"`
		What     string `json:"what"`
		Where    string `json:"where,omitempty"`
	} `json:"issues"`
}

func parseJudgeResponse(raw string) (session.Verdict, []session.Issue, error) {
	body := extractFirstJSONObject(raw)
	if body == "" {
		return "", nil, fmt.Errorf("no JSON object in response")
	}
	var parsed judgeResponseShape
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", nil, err
	}
	verdict, err := parseVerdict(parsed.Verdict)
	if err != nil {
		return "", nil, err
	}
	issues := make([]session.Issue, 0, len(parsed.Issues))
	for _, is := range parsed.Issues {
		sev, _ := parseSeverity(is.Severity)
		issues = append(issues, session.Issue{
			Severity: sev,
			Where:    is.Where,
			What:     is.What,
		})
	}
	return verdict, issues, nil
}

// extractFirstJSONObject tolerates a model that wraps JSON in
// markdown fences or prose: scans for the first '{' and returns the
// substring through the matching '}'. Returns empty when no braces
// balance.
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

func parseVerdict(s string) (session.Verdict, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pass":
		return session.VerdictPass, nil
	case "fail":
		return session.VerdictFail, nil
	case "issues":
		return session.VerdictIssues, nil
	default:
		return "", fmt.Errorf("unknown verdict %q", s)
	}
}

func parseSeverity(s string) (session.Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return session.SeverityInfo, nil
	case "warning", "warn":
		return session.SeverityWarning, nil
	case "error", "err":
		return session.SeverityError, nil
	default:
		return session.SeverityInfo, fmt.Errorf("unknown severity %q", s)
	}
}

// Compile-time check.
var _ Judge = (*AnthropicJudge)(nil)
