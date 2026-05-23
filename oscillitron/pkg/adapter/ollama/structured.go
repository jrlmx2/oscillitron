// CLAUDE GENERATED
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

// returnResultPayloadJSON accepts BOTH:
//   - the legacy envelope: {content, confidence, grounded_pass, ...}
//   - the v3.5 structured-output envelope: {answer, confidence}
//
// parseReturnResultJSON picks Answer when present (preferred for
// MCQ benchmarks), falling back to Content. Operators using the
// schema enforcement set Answer; legacy callers keep Content.
type returnResultPayloadJSON struct {
	Content        string   `json:"content,omitempty"`
	Answer         string   `json:"answer,omitempty"`
	Confidence     float64  `json:"confidence"`
	GroundedPass   *bool    `json:"grounded_pass,omitempty"`
	Contradictions []string `json:"contradictions,omitempty"`
	OpenQuestions  []string `json:"open_questions,omitempty"`
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
	case session.PlaybookProcess, session.PlaybookCompose:
		return parseReturnResultJSON(obj)
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
	// v3.5: Answer (structured-output schema) takes precedence over
	// Content (legacy envelope). Falls back to Content when Answer
	// is absent so existing JSON-envelope responses keep working.
	content := p.Answer
	if content == "" {
		content = p.Content
	}
	return &session.Execute{
		Category: session.CategoryReturnResult,
		ReturnResult: &session.ReturnResultPayload{
			Result:     session.Payload{Kind: "result", Content: content},
			Confidence: p.Confidence,
			Signals: session.Signals{
				GroundedPass:   p.GroundedPass,
				Contradictions: p.Contradictions,
				OpenQuestions:  p.OpenQuestions,
			},
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
		// Confidence: 0 = "not reported," NOT "zero confidence." When
		// the substrate doesn't emit a parseable confidence (neither
		// JSON envelope nor a `confidence: X.X` minimal-output line),
		// downstream consumers MUST distinguish missing from low.
		// Critically: cope.Decide treats 0 as ShipWithCaveat, NOT as
		// "low confidence → escalate" — escalating on missing data
		// is expensive and wrong (we don't actually know the model
		// is uncertain).
		//
		// applyEffectiveConfidence (in ollama.go) checks `<= 0.0` to
		// know it can stamp a recovered confidence from raw text; if
		// no confidence: line was emitted either, the field stays 0
		// all the way through to the report.
		return &session.Execute{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result:     session.Payload{Kind: "result", Content: strings.TrimSpace(raw)},
				Confidence: 0,
			},
		}
	}
}
