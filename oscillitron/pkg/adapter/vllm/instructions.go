package vllm

// NOTE: these instruction templates are duplicated from
// pkg/adapter/hermes/instructions.go and pkg/adapter/ollama/instructions.go
// on purpose for v0. The prompts are substrate-agnostic — they
// describe the v0 playbook protocol, not anything vLLM-specific —
// and consolidating them into a shared package is a future refactor
// (worth doing once a third substrate motivates it; vLLM being the
// third, the shared-package extraction is the natural follow-up).
//
// If you change a prompt here, change it in hermes and ollama too,
// or the three adapters will drift in subtle ways the bench will
// then have to untangle.

import (
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

func renderEvaluateInstructions(env session.Envelope) string {
	contractHint := ""
	if s := strings.TrimSpace(env.OutputSchema); s != "" {
		contractHint = "\n\nThe downstream execute step will need to satisfy this output schema:\n" + s
	}
	return evaluateInstructions + contractHint
}

func renderExecuteInstructions(pb session.Playbook, env session.Envelope) string {
	schema := strings.TrimSpace(env.OutputSchema)
	if schema == "" {
		schema = "(no specific schema; satisfy the user request as best you can)"
	}
	switch pb {
	case session.PlaybookPlan:
		return fmt.Sprintf(executePlanInstructions, schema)
	case session.PlaybookProcess:
		return fmt.Sprintf(executeProcessInstructions, schema)
	case session.PlaybookCritique:
		return executeCritiqueInstructions
	case session.PlaybookVerifyGrounded:
		spec := "(no spec provided)"
		if env.VerifySpec != nil {
			spec = fmt.Sprintf("kind=%q\nspec=%s", env.VerifySpec.Kind, env.VerifySpec.Spec)
		}
		return fmt.Sprintf(executeVerifyGroundedInstructions, spec)
	case session.PlaybookCompose:
		return fmt.Sprintf(executeComposeInstructions, schema)
	default:
		return executeProcessInstructions
	}
}

const evaluateInstructions = `You are a routing classifier inside a call-tree reasoning system.

Given the AP input below, pick exactly ONE playbook from this v0 set and return a single JSON object with no surrounding prose:

{
  "playbook":   "<one of: plan | process | critique | verify_grounded | compose>",
  "rationale":  "<one short sentence explaining the pick>",
  "confidence": <number between 0.0 and 1.0>
}

Playbook descriptions:
  - plan            — Input is a complex task that needs decomposition into sub-tasks.
  - process         — Input is a single task you can answer directly.
  - critique        — Input is a prior result that needs inspection (pass/fail/issues).
  - verify_grounded — Input is a result + a grounded check spec (compile/exec/retrieval-match).
  - compose         — Input is a request to reduce N sibling results into one.

Pick the cheapest playbook that fits. Default to "process" when in doubt.`

const executePlanInstructions = `You are a planning specialist inside a call-tree reasoning system. The user task below needs decomposition into sub-tasks that other specialists will execute. Return a single JSON object with no surrounding prose:

{
  "sub_aps": [
    {
      "input_kind":         "task",
      "input":              "<what the sub-invocation should do>",
      "output_schema":      "<contract for the sub-output>",
      "classification":     "<expected level (e.g. 'internal' / 'public'); empty if unspecified>",
      "needs_verification": false,
      "verify_spec":        null
    }
  ],
  "recompose": "<one of: pairwise | sequential | none>"
}

Pick "sequential" when sub-task outputs need to be combined in order.
Pick "pairwise" when sub-task outputs can be combined in any pair-wise order (cheaper and v1 will parallelize).
Pick "none" when sub-tasks are independent and no recomposition is meaningful.

Set "needs_verification" only on sub-tasks whose result you want guaranteed to be critique-passed regardless of sampling.

The parent's overall output schema (your decomposition must serve this):
%s`

const executeProcessInstructions = `Answer the following. End your response with "confidence: X.X" on its own line (0.0 to 1.0).

%s`

const executeCritiqueInstructions = `You are a critic inside a call-tree reasoning system. The input below is a prior result that needs inspection. Return a single JSON object with no surrounding prose:

{
  "verdict": "<one of: pass | fail | issues>",
  "issues": [
    {"severity": "<info|warning|error>", "where": "<location in the input>", "what": "<description>"}
  ]
}

Verdict semantics:
  - pass   — the prior result is sound; nothing to flag.
  - issues — the prior result has problems worth noting but is not fundamentally wrong.
  - fail   — the prior result is incorrect or unsalvageable.

Leave "issues" as an empty array when verdict is "pass". Be honest — your verdict is the verifier signal the runtime uses to decide retry/proceed/escalate.`

const executeVerifyGroundedInstructions = `You are a grounded-check verifier inside a call-tree reasoning system. Run the check described below and return a single JSON object with no surrounding prose:

{
  "verdict": "<one of: pass | fail | issues>",
  "issues": [
    {"severity": "<info|warning|error>", "where": "<location>", "what": "<description>"}
  ]
}

Verdict semantics:
  - pass — the check succeeded (compile clean, exec exit 0, retrieval matched).
  - fail — the check failed (compile errors, exec non-zero, retrieval miss).
  - issues — the check completed but produced warnings worth surfacing.

Check spec:
%s`

const executeComposeInstructions = `Combine the following results into a single best answer. End your response with "confidence: X.X" on its own line (0.0 to 1.0).

%s`
