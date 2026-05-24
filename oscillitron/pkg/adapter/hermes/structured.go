package hermes

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter/minimal"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/notice"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// evaluatePayload is the JSON shape the adapter expects Hermes to
// emit from the evaluate step.
type evaluatePayload struct {
	Playbook   string  `json:"playbook"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

// emitSubtreePayloadJSON is the execute-step JSON for PlaybookPlan.
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

// returnResultPayloadJSON is the execute-step JSON for PlaybookProcess
// and PlaybookCompose.
type returnResultPayloadJSON struct {
	Content        string   `json:"content"`
	Confidence     float64  `json:"confidence"`
	GroundedPass   *bool    `json:"grounded_pass,omitempty"`
	Contradictions []string `json:"contradictions,omitempty"`
	OpenQuestions  []string `json:"open_questions,omitempty"`
}

// verifierSignalPayloadJSON is the execute-step JSON for
// PlaybookCritique and PlaybookVerifyGrounded.
type verifierSignalPayloadJSON struct {
	Verdict string      `json:"verdict"`
	Issues  []issueJSON `json:"issues,omitempty"`
}

type issueJSON struct {
	Severity string `json:"severity"`
	Where    string `json:"where"`
	What     string `json:"what"`
}

// jsonFenceRE matches a fenced JSON code block — Markdown's standard
// ```json ... ``` — anywhere in the output. The (?s) flag lets `.`
// span newlines. We tolerate any leading whitespace inside the fence.
var jsonFenceRE = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")

// rawObjectRE matches a JSON object that occupies the whole string
// (modulo whitespace). This is the "model obeyed the instructions
// and emitted JSON, nothing else" case.
var rawObjectRE = regexp.MustCompile(`(?s)^\s*(\{.*\})\s*$`)

// extractJSONObject pulls the first JSON object out of Hermes' raw
// output. Order of preference:
//
//  1. The whole output is a JSON object → use it.
//  2. A fenced ```json block exists → use that block.
//  3. Otherwise → return empty, structured=false, no error.
//
// If parse fails on a candidate that LOOKED like JSON, return an
// error — that's a real protocol violation worth surfacing.
func extractJSONObject(raw string) (string, bool) {
	if m := rawObjectRE.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	if m := jsonFenceRE.FindStringSubmatch(raw); m != nil {
		return m[1], true
	}
	return "", false
}

// parseEvaluateResponse pulls an evaluatePayload out of the raw text.
// Returns (payload, structured, error). The structured bool tells the
// caller whether a JSON object was found at all; the error fires only
// when JSON was present but malformed.
func parseEvaluateResponse(raw string) (evaluatePayload, bool, error) {
	obj, ok := extractJSONObject(raw)
	if !ok {
		return evaluatePayload{}, false, nil
	}
	var p evaluatePayload
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return evaluatePayload{}, false, fmt.Errorf("hermes: parse evaluate JSON: %w", err)
	}
	return p, true, nil
}

// parsePlaybook converts a Hermes-emitted string into a session.Playbook,
// rejecting anything outside the v0 set.
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
		return "", fmt.Errorf("hermes: evaluate returned unknown playbook %q (expected one of plan|process|critique|verify_grounded|compose)", s)
	}
}

// parseRecomposeSpec converts a Hermes-emitted string into a session.RecomposeSpec.
func parseRecomposeSpec(s string) (session.RecomposeSpec, error) {
	switch session.RecomposeSpec(strings.TrimSpace(s)) {
	case session.RecomposePairwise:
		return session.RecomposePairwise, nil
	case session.RecomposeSequential:
		return session.RecomposeSequential, nil
	case session.RecomposeNone:
		return session.RecomposeNone, nil
	default:
		return "", fmt.Errorf("hermes: plan returned unknown recompose spec %q", s)
	}
}

// parseVerdict converts a Hermes-emitted string into a session.Verdict.
func parseVerdict(s string) (session.Verdict, error) {
	switch session.Verdict(strings.TrimSpace(s)) {
	case session.VerdictPass:
		return session.VerdictPass, nil
	case session.VerdictFail:
		return session.VerdictFail, nil
	case session.VerdictIssues:
		return session.VerdictIssues, nil
	default:
		return "", fmt.Errorf("hermes: critique/verify_grounded returned unknown verdict %q", s)
	}
}

// parseSeverity converts a Hermes-emitted string into a session.Severity,
// defaulting to SeverityWarning for unknown values (issues should not
// be silently dropped on a malformed severity tag).
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

// parseExecuteResponse pulls a playbook-specific Execute payload out
// of the raw text and wraps it in a session.Execute.
//
// require controls strictness: when true, a non-JSON output is an
// error; when false, the function returns a low-confidence fallback
// payload appropriate for the playbook's category.
func parseExecuteResponse(pb session.Playbook, raw string, require bool) (*session.Execute, error) {
	obj, ok := extractJSONObject(raw)
	if !ok {
		if require {
			return nil, fmt.Errorf("hermes: execute produced no structured envelope for %q (raw length %d)", pb, len(raw))
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
		return nil, fmt.Errorf("hermes: unknown playbook %q in Execute", pb)
	}
}

// parseEmitSubtreeJSON decodes the plan-playbook execute payload.
func parseEmitSubtreeJSON(obj string) (*session.Execute, error) {
	var p emitSubtreePayloadJSON
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil, fmt.Errorf("hermes: parse plan JSON: %w", err)
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

// parseReturnResultJSON decodes a process- or compose-playbook
// execute payload.
func parseReturnResultJSON(obj string) (*session.Execute, error) {
	var p returnResultPayloadJSON
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil, fmt.Errorf("hermes: parse return_result JSON: %w", err)
	}
	return &session.Execute{
		Category: session.CategoryReturnResult,
		ReturnResult: &session.ReturnResultPayload{
			Result: session.Payload{Kind: "result", Content: p.Content},
			// v3.5 percent-normalize. See ollama/structured.go.
			Confidence: notice.NormalizeConfidence(p.Confidence),
			Signals: session.Signals{
				GroundedPass:   p.GroundedPass,
				Contradictions: p.Contradictions,
				OpenQuestions:  p.OpenQuestions,
			},
		},
	}, nil
}

// parseVerifierSignalJSON decodes a critique- or verify_grounded-
// playbook execute payload.
func parseVerifierSignalJSON(obj string) (*session.Execute, error) {
	var p verifierSignalPayloadJSON
	if err := json.Unmarshal([]byte(obj), &p); err != nil {
		return nil, fmt.Errorf("hermes: parse verifier_signal JSON: %w", err)
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

// unstructuredFallback shapes a low-confidence payload when the model
// failed to emit JSON. Shape matches the playbook's category so the
// runner can proceed without a special-case branch.
func unstructuredFallback(pb session.Playbook, raw string) *session.Execute {
	switch pb {
	case session.PlaybookPlan:
		// No sub-APs and no recompose spec; runner treats this as an
		// empty subtree.
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
	default: // process, compose
		// Confidence: 0 = "not reported" (NOT "zero confidence"). See
		// pkg/adapter/ollama/structured.go for the full rationale —
		// cope.Decide treats 0 as ShipWithCaveat (not escalate),
		// which is the correct behavior when we don't actually know
		// how confident the substrate is.
		// XML-tag path: recover confidence from <confidence>X</confidence>
		// when the substrate emitted the canonical tag format.
		conf, _ := minimal.ExtractConfidenceTag(raw)
		return &session.Execute{
			Category: session.CategoryReturnResult,
			ReturnResult: &session.ReturnResultPayload{
				Result:     session.Payload{Kind: "result", Content: strings.TrimSpace(raw)},
				Confidence: conf,
			},
		}
	}
}
