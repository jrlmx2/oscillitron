package ollama

// NOTE: same duplication note as instructions.go — these parsers are
// substrate-agnostic and mirror pkg/adapter/hermes/structured.go. They
// describe the v0 playbook output protocol, not anything
// Ollama-specific. Consolidate into a shared package once a third
// substrate motivates it.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/notice"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

type evaluatePayload struct {
	Playbook   string  `json:"playbook"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

type emitSubtreePayloadJSON struct {
	SubAPs    []executeSubAPSeedJSON `json:"sub_aps"`
	Recompose string                 `json:"recompose"`
}

type executeSubAPSeedJSON struct {
	InputKind         string         `json:"input_kind"`
	Input             string         `json:"input"`
	OutputSchema      string         `json:"output_schema"`
	Classification    string         `json:"classification"`
	NeedsVerification bool           `json:"needs_verification"`
	VerifySpec        *verifySpecRaw `json:"verify_spec,omitempty"`
}

type verifySpecRaw struct {
	Kind string `json:"kind"`
	Spec string `json:"spec"`
}

// returnResultPayloadJSON accepts the {response, confidence} shape
// produced by pkg/adapter/minimal.ProcessSchema, plus legacy field
// names (answer, content) for backward compatibility with older
// JSONL artifacts on disk and any caller still wiring the
// pre-rename envelope.
//
// parseReturnResultJSON picks the first non-empty of:
//
//	Response → Answer → Content
//
// Operators on the current `response_format` path set Response;
// the v3.5-era Answer name is kept as legacy fallback; Content is
// the original legacy envelope's field.
type returnResultPayloadJSON struct {
	Response   string  `json:"response,omitempty"`
	Answer     string  `json:"answer,omitempty"`
	Content    string  `json:"content,omitempty"`
	Confidence float64 `json:"confidence"`
}

type verifierSignalPayloadJSON struct {
	Verdict string      `json:"verdict"`
	Issues  []issueJSON `json:"issues,omitempty"`
}

type issueJSON struct {
	Severity string `json:"severity"`
	Where    string `json:"where"`
	What     string `json:"what"`
}

var jsonFenceRE = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")
var rawObjectRE = regexp.MustCompile(`(?s)^\s*(\{.*\})\s*$`)

// extractJSONObject pulls the first JSON object out of the raw output,
// preferring a whole-string object over a fenced block.
func extractJSONObject(raw string) (string, bool) {
	if m := rawObjectRE.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	if m := jsonFenceRE.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	return "", false
}

func parseEvaluateResponse(raw string) (evaluatePayload, bool, error) {
	obj, ok := extractJSONObject(raw)
	if !ok {
		return evaluatePayload{}, false, nil
	}
	var p evaluatePayload
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return evaluatePayload{}, false, fmt.Errorf("ollama: parse evaluate JSON: %w", err)
	}
	return p, true, nil
}

func parsePlaybook(s string) (session.Playbook, error) {
	switch session.Playbook(strings.TrimSpace(s)) {
	case session.PlaybookPlan:
		return session.PlaybookPlan, nil
	case session.PlaybookProcess:
		return session.PlaybookProcess, nil
	case session.PlaybookCritique:
		return session.PlaybookCritique, nil
	case session.PlaybookVerifyGrounded:
		return session.PlaybookVerifyGrounded, nil
	case session.PlaybookCompose:
		return session.PlaybookCompose, nil
	default:
		return "", fmt.Errorf("ollama: evaluate returned unknown playbook %q (expected one of plan|process|critique|verify_grounded|compose)", s)
	}
}

func parseRecomposeSpec(s string) (session.RecomposeSpec, error) {
	switch session.RecomposeSpec(strings.TrimSpace(s)) {
	case session.RecomposePairwise:
		return session.RecomposePairwise, nil
	case session.RecomposeSequential:
		return session.RecomposeSequential, nil
	case session.RecomposeNone:
		return session.RecomposeNone, nil
	default:
		return "", fmt.Errorf("ollama: plan returned unknown recompose spec %q", s)
	}
}

func parseVerdict(s string) (session.Verdict, error) {
	switch session.Verdict(strings.TrimSpace(s)) {
	case session.VerdictPass:
		return session.VerdictPass, nil
	case session.VerdictFail:
		return session.VerdictFail, nil
	case session.VerdictIssues:
		return session.VerdictIssues, nil
	default:
		return "", fmt.Errorf("ollama: critique/verify_grounded returned unknown verdict %q", s)
	}
}

func parseSeverity(s string) session.Severity {
	switch session.Severity(strings.TrimSpace(s)) {
	case session.SeverityInfo:
		return session.SeverityInfo
	case session.SeverityWarning:
		return session.SeverityWarning
	case session.SeverityError:
		return session.SeverityError
	default:
		return session.SeverityWarning
	}
}

func parseExecuteResponse(pb session.Playbook, raw string, require bool) (*session.Execute, error) {
	// Process and compose produce natural text with a trailing
	// "confidence: X.X" annotation — no JSON envelope. Go straight
	// to unstructuredFallback; applyEffectiveConfidence recovers
	// the confidence downstream.
	if pb == session.PlaybookProcess || pb == session.PlaybookCompose {
		return unstructuredFallback(pb, raw), nil
	}
	obj, ok := extractJSONObject(raw)
	if !ok {
		if require {
			return nil, fmt.Errorf("ollama: execute produced no structured envelope for %q (raw length %d)", pb, len(raw))
		}
		return unstructuredFallback(pb, raw), nil
	}
	switch pb {
	case session.PlaybookPlan:
		return parseEmitSubtreeJSON(obj)
	case session.PlaybookCritique, session.PlaybookVerifyGrounded:
		return parseVerifierSignalJSON(obj)
	default:
		return nil, fmt.Errorf("ollama: unknown playbook %q in Execute", pb)
	}
}

func parseEmitSubtreeJSON(obj string) (*session.Execute, error) {
	var p emitSubtreePayloadJSON
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil, fmt.Errorf("ollama: parse plan JSON: %w", err)
	}
	spec, err := parseRecomposeSpec(p.Recompose)
	if err != nil {
		return nil, err
	}
	seeds := make([]session.SubAPSeed, 0, len(p.SubAPs))
	for _, s := range p.SubAPs {
		kind := strings.TrimSpace(s.InputKind)
		if kind == "" {
			kind = "task"
		}
		seed := session.SubAPSeed{
			Input:             session.Payload{Kind: kind, Content: s.Input},
			OutputSchema:      s.OutputSchema,
			Classification:    classification.Level(strings.TrimSpace(s.Classification)),
			NeedsVerification: s.NeedsVerification,
		}
		if s.VerifySpec != nil {
			seed.VerifySpec = &session.VerifySpec{Kind: s.VerifySpec.Kind, Spec: s.VerifySpec.Spec}
		}
		seeds = append(seeds, seed)
	}
	return &session.Execute{
		Category: session.CategoryEmitSubtree,
		EmitSubtree: &session.EmitSubtreePayload{
			SubAPs:    seeds,
			Recompose: spec,
		},
	}, nil
}

func parseReturnResultJSON(obj string) (*session.Execute, error) {
	var p returnResultPayloadJSON
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil, fmt.Errorf("ollama: parse return_result JSON: %w", err)
	}
	// Field preference: Response (current schema) → Answer (v3.5
	// legacy) → Content (original legacy envelope). Each subsequent
	// fallback is empty when callers use a more recent shape; we
	// pick the first populated one.
	content := p.Response
	if content == "" {
		content = p.Answer
	}
	if content == "" {
		content = p.Content
	}
	return &session.Execute{
		Category: session.CategoryReturnResult,
		ReturnResult: &session.ReturnResultPayload{
			Result: session.Payload{Kind: "result", Content: content},
			// v3.5 percent-normalize: schema enforces type but not
			// numeric bounds; qwen2.5:7b emits 95/100 (percent).
			Confidence: notice.NormalizeConfidence(p.Confidence),
		},
	}, nil
}

func parseVerifierSignalJSON(obj string) (*session.Execute, error) {
	var p verifierSignalPayloadJSON
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil, fmt.Errorf("ollama: parse verifier_signal JSON: %w", err)
	}
	verdict, err := parseVerdict(p.Verdict)
	if err != nil {
		return nil, err
	}
	issues := make([]session.Issue, 0, len(p.Issues))
	for _, i := range p.Issues {
		issues = append(issues, session.Issue{
			Severity: parseSeverity(i.Severity),
			Where:    i.Where,
			What:     i.What,
		})
	}
	return &session.Execute{
		Category: session.CategoryVerifierSignal,
		VerifierSignal: &session.VerifierSignalPayload{
			Verdict: verdict,
			Issues:  issues,
		},
	}, nil
}

func unstructuredFallback(pb session.Playbook, raw string) *session.Execute {
	switch pb {
	case session.PlaybookPlan:
		return &session.Execute{
			Category: session.CategoryEmitSubtree,
			EmitSubtree: &session.EmitSubtreePayload{
				Recompose: session.RecomposeNone,
			},
		}
	case session.PlaybookCritique, session.PlaybookVerifyGrounded:
		return &session.Execute{
			Category: session.CategoryVerifierSignal,
			VerifierSignal: &session.VerifierSignalPayload{
				Verdict: session.VerdictIssues,
				Issues: []session.Issue{{
					Severity: session.SeverityWarning,
					What:     "unstructured response from substrate; treating as 'issues'",
				}},
			},
		}
	default:
		// Confidence: 0 = "not reported," NOT "zero confidence."
		// When the model emitted unstructured output (response_format
		// schema enforcement somehow failed AND it wasn't valid JSON
		// either), we have no parseable confidence signal. The field
		// stays 0 and downstream consumers MUST distinguish missing
		// from low. cope.Decide treats 0 as ShipWithCaveat, not as
		// "low confidence → escalate" — escalating on missing data
		// is expensive and wrong (the model is not actually known
		// to be uncertain).
		return &session.Execute{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result:     session.Payload{Kind: "result", Content: strings.TrimSpace(raw)},
				Confidence: 0,
			},
		}
	}
}
