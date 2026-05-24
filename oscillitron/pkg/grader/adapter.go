package grader

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/vram"
)

// AdapterGrader is a Grader backed by any adapter.Adapter — Anthropic,
// local Hermes, future OpenAI. Uses the adapter's Process playbook
// with a rubric prompt that asks the model to return scores as
// `key=value` pairs inside the standard `{content, confidence}` shape.
//
// Why key=value rather than nested JSON: smaller models reliably get
// `relevance=4,tone=5,...` right; they botch nested JSON escaping.
// Trade-off: the rubric must stay simple enough to express as flat
// key=value pairs (which the current 4-axis rubric does cleanly).
//
// Substrate-agnostic. The Grader interface is satisfied; phase1 (or
// any other consumer) can swap AnthropicGrader for AdapterGrader to
// run grading on the same substrate as the orchestrator — useful for
// pure-local measurement when you don't want any external API calls.
//
// Measurement note: a small local model is a noisier judge than
// Sonnet/Opus. AdapterGrader is appropriate for substrate-isolation
// measurements (e.g., "what is the orchestration's lift over solo on
// local-only infra") and less appropriate for grading absolute
// quality against frontier (you want a strong external judge for that).
type AdapterGrader struct {
	// Adapter is the substrate used for grading calls. Required.
	Adapter adapter.Adapter
	// SystemPromptOverride lets callers replace the built-in rubric
	// preamble. Most callers should leave this empty.
	SystemPromptOverride string
	// Governor optionally coordinates VRAM headroom with other
	// components hitting the same inference substrate (typically the
	// runner during the same bench case). When set, each Grade call
	// Acquires a lease, runs the adapter, and Releases — blocking if
	// granting would push past the governor's budget. Nil = no
	// coordination (legacy behavior).
	Governor *vram.Governor
}

const adapterGraderPrompt = `You are a strict quality grader. ` +
	`You receive (a) a task description and (b) a candidate response to that task. ` +
	`Score the candidate on a 1-5 scale across four dimensions:` + "\n" +
	`- relevance: does the response actually address the task?` + "\n" +
	`- tone: does it match what the task implies (formal/casual/firm/etc.)?` + "\n" +
	`- completeness: does it include the required elements?` + "\n" +
	`- professionalism: free of errors, hedging, generic filler.` + "\n" +
	`1 = unacceptable, 3 = OK, 5 = excellent. Be calibrated.`

// Name implements Grader.
func (g AdapterGrader) Name() string {
	if g.Adapter == nil {
		return "adapter-grader(<nil>)"
	}
	return "adapter-grader(" + g.Adapter.Name() + ")"
}

// Grade implements Grader.
func (g AdapterGrader) Grade(ctx context.Context, req Request) (Result, error) {
	if g.Adapter == nil {
		return Result{}, fmt.Errorf("adapter-grader: Adapter is required")
	}
	preamble := g.SystemPromptOverride
	if preamble == "" {
		preamble = adapterGraderPrompt
	}
	input := renderAdapterGraderInput(preamble, req)
	env := session.NewRoot(
		session.ID("grader-"+req.Notes),
		input,
		`{"content": "relevance=N,tone=N,completeness=N,professionalism=N,notes=...", "confidence": <0..1>}`,
		classification.Internal,
		session.Budget{TokensRemaining: 8_000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{
		Playbook:   session.PlaybookProcess,
		Confidence: 1.0,
	}
	// Optional VRAM coordination. Acquire returns a no-op lease for a
	// nil governor, so the call shape is uniform.
	lease, err := g.Governor.Acquire(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("adapter-grader: governor acquire: %w", err)
	}
	defer lease.Release()
	out, err := g.Adapter.Execute(ctx, env)
	if err != nil {
		return Result{}, fmt.Errorf("adapter-grader: execute: %w", err)
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		return Result{}, fmt.Errorf("adapter-grader: empty return_result")
	}
	content := out.Execute.ReturnResult.Result.Content
	score, err := parseKeyValueScore(content)
	if err != nil {
		return Result{}, fmt.Errorf("adapter-grader: parse score from %q: %w", content, err)
	}
	return Result{
		Score:      score,
		TokensUsed: out.Execute.TokensUsed,
		Model:      g.Adapter.Name(),
		Raw:        content,
	}, nil
}

func renderAdapterGraderInput(preamble string, req Request) string {
	var b strings.Builder
	b.WriteString(preamble)
	b.WriteString("\n\n=== Task ===\n")
	b.WriteString(req.Task)
	b.WriteString("\n\n=== Candidate ===\n")
	b.WriteString(req.Candidate)
	if req.Notes != "" {
		b.WriteString("\n\n=== Context ===\n")
		b.WriteString(req.Notes)
	}
	b.WriteString("\n\nReply with this exact JSON shape:\n")
	b.WriteString(`{"content": "relevance=N,tone=N,completeness=N,professionalism=N,notes=<short justification>", "confidence": <average score / 5>}`)
	b.WriteString("\nReplace each N with an integer 1-5. The notes value must not contain commas — use semicolons or rephrase. Reply with the JSON object only.")
	return b.String()
}

// parseKeyValueScore extracts {relevance,tone,completeness,professionalism,notes}
// from a comma-separated key=value string. Tolerant: missing trailing
// keys are 0 and will trip the range check; extra unknown keys are
// ignored. Notes can carry semicolons or other punctuation but not
// commas.
func parseKeyValueScore(s string) (Score, error) {
	if s == "" {
		return Score{}, fmt.Errorf("empty content")
	}
	// Some models wrap the response in stray quotes or backticks. Strip.
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"`")
	s = strings.TrimSpace(s)

	var out Score
	seenScores := 0
	for _, pair := range splitOutsideJSON(s) {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(pair[:eq]))
		val := strings.TrimSpace(pair[eq+1:])
		switch key {
		case "relevance":
			n, err := strconv.Atoi(val)
			if err != nil {
				return Score{}, fmt.Errorf("relevance not an integer: %q", val)
			}
			out.Relevance = n
			seenScores++
		case "tone":
			n, err := strconv.Atoi(val)
			if err != nil {
				return Score{}, fmt.Errorf("tone not an integer: %q", val)
			}
			out.Tone = n
			seenScores++
		case "completeness":
			n, err := strconv.Atoi(val)
			if err != nil {
				return Score{}, fmt.Errorf("completeness not an integer: %q", val)
			}
			out.Completeness = n
			seenScores++
		case "professionalism":
			n, err := strconv.Atoi(val)
			if err != nil {
				return Score{}, fmt.Errorf("professionalism not an integer: %q", val)
			}
			out.Professionalism = n
			seenScores++
		case "notes":
			out.Notes = val
		}
	}
	if seenScores < 4 {
		return Score{}, fmt.Errorf("expected 4 numeric scores, found %d", seenScores)
	}
	for _, val := range []int{out.Relevance, out.Tone, out.Completeness, out.Professionalism} {
		if val < 1 || val > 5 {
			return Score{}, fmt.Errorf("score out of range (1-5): %d", val)
		}
	}
	return out, nil
}

// splitOutsideJSON splits on commas that aren't inside quoted strings.
// Conservative — only handles double-quoted strings, which is what we
// asked the model to produce.
func splitOutsideJSON(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '"' && (i == 0 || s[i-1] != '\\') {
			inQuote = !inQuote
		}
		if ch == ',' && !inQuote {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// _ enforces that the regexp import is actually used somewhere (it's
// here for future expansion — e.g., extracting scores from prose
// responses). Remove this and the import once that lands.
var _ = regexp.MustCompile

// Compile-time check.
var _ Grader = AdapterGrader{}
