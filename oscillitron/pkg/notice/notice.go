// CLAUDE GENERATED
// Package notice is the v3.1 "notice" layer — cheap, deterministic
// detectors that flag inadequacy signals BEFORE and AFTER the
// substrate call. v3.1 ships the prompt-side detectors (§3.1 of
// scratch/v3-design.md). Response-side detectors land in v3.2.
//
// Notice analyzes BEHAVIOR, not CORRECTNESS. We don't need ground
// truth to know that:
//
//   - "we're shipping 4097 tokens of envelope around a 122-token
//     question" is concerning (Hermes-overload story);
//   - "our system prompt is 5× longer than the user content" is
//     concerning;
//   - "the input fills 95% of the model's context window" is
//     concerning.
//
// These detectors fire on input/output shape alone. They feed the
// "act" layer's coping decisions (v3.4): downgrade effective
// confidence, escalate, refuse, etc.
//
// v3.1 scope: prompt-side signals only. The detectors here are:
//
//   - InputOverflowsContext   — input tokens > model's context window
//   - InputApproachingContext — input tokens > 80% of context window
//   - PersonaHeavy            — system prompt much larger than user content
//
// Deliberately NOT in v3.1:
//   - Input size vs. running median (needs per-action history)
//   - Schema vs. substrate capability (needs a table; punt)
//   - Token budget vs. complexity (needs a heuristic)
//   - Conflicting instructions (hard NLP)
package notice

import "fmt"

// Signal identifies one detector. Stable string values — these
// appear in trace events, JSONL streams, and any downstream
// analysis. Adding new signals is additive; renaming or removing
// breaks consumers.
type Signal string

const (
	// InputOverflowsContext fires when the input token count exceeds
	// the model's context window. The substrate will truncate or
	// reject; small models may degenerate before they reason.
	InputOverflowsContext Signal = "input_overflows_context"

	// InputApproachingContext fires when input tokens exceed
	// ApproachingThreshold (default 0.80) of the model's context
	// window. Lower confidence in the output; the model has little
	// room to elaborate.
	InputApproachingContext Signal = "input_approaching_context"

	// PersonaHeavy fires when the system prompt (instructions,
	// persona, envelope) is much larger than the user content.
	// The Hermes-overload story: a 4097-token system prompt around
	// a 122-token question crushes small models. The default ratio
	// threshold is PersonaHeavyRatio (3.0×).
	PersonaHeavy Signal = "persona_heavy"
)

// Severity is the operational tier of a detection. Info is "noted
// for posterity"; warning is "worth a look"; error is "this is
// likely degrading output."
type Severity string

const (
	Info    Severity = "info"
	Warning Severity = "warning"
	Error   Severity = "error"
)

// Detection is one fired signal with context.
type Detection struct {
	// Signal identifies which detector fired.
	Signal Signal `json:"signal"`
	// Severity is the operational tier.
	Severity Severity `json:"severity"`
	// Message is human-readable for logs / reports.
	Message string `json:"message"`
	// Metric is the numeric value that triggered the detection
	// (ratio, fraction, token count, etc.). Optional; zero if the
	// detection isn't numeric. Surfacing it lets downstream
	// analysis aggregate without re-parsing Message.
	Metric float64 `json:"metric,omitempty"`
}

// Assessment is the result of running every prompt-side detector on
// one call. Empty Detections slice means "all clear." Score is a
// 0.0–1.0 summary: 0 if no detections, 1 if any Error-severity
// fired, ratio-weighted by warnings otherwise.
type Assessment struct {
	Detections []Detection `json:"detections,omitempty"`
	// Score summarizes the assessment. 0.0 = no concerns; 1.0 = an
	// Error-severity detection fired. Linear blend in between.
	Score float64 `json:"score"`
}

// CallInspection is the input to Inspect. Constructed by the caller
// (adapter, runner, or test harness) just before posting to the
// substrate.
type CallInspection struct {
	// SystemPrompt is the instructions + persona + envelope —
	// everything before the user's actual question. For
	// OpenAI-compat adapters this is the system-role message
	// content.
	SystemPrompt string
	// UserContent is the user-role message — the actual question or
	// task. For the bench, this is the case prompt.
	UserContent string
	// ContextSize is the substrate's context-window token capacity.
	// Zero disables the *OverflowContext detectors (we can't check
	// against an unknown window).
	ContextSize int
	// ApproxTokens estimates the token count of a string. nil
	// defaults to DefaultApproxTokens (bytes/4). Override with a
	// real tokenizer for accuracy when needed.
	ApproxTokens func(s string) int
}

// Inspect runs all v3.1 prompt-side detectors and returns the
// Assessment. Cheap, deterministic, no I/O.
func Inspect(c CallInspection) Assessment {
	tok := c.ApproxTokens
	if tok == nil {
		tok = DefaultApproxTokens
	}
	sysTok := tok(c.SystemPrompt)
	usrTok := tok(c.UserContent)
	inputTok := sysTok + usrTok

	var detections []Detection

	// --- Context-window detectors ---
	// Only run when ContextSize > 0; otherwise we have nothing to
	// compare against.
	if c.ContextSize > 0 {
		if inputTok >= c.ContextSize {
			detections = append(detections, Detection{
				Signal:   InputOverflowsContext,
				Severity: Error,
				Message:  fmt.Sprintf("input %d tokens >= context window %d; substrate will truncate or reject", inputTok, c.ContextSize),
				Metric:   float64(inputTok) / float64(c.ContextSize),
			})
		} else {
			ratio := float64(inputTok) / float64(c.ContextSize)
			if ratio >= ApproachingThreshold {
				detections = append(detections, Detection{
					Signal:   InputApproachingContext,
					Severity: Warning,
					Message:  fmt.Sprintf("input %d tokens fills %.0f%% of context window %d; little room to elaborate", inputTok, ratio*100, c.ContextSize),
					Metric:   ratio,
				})
			}
		}
	}

	// --- Persona-heavy detector ---
	// Fires when system prompt is much larger than user content.
	// Skip when user content is trivially small (<= 8 tokens) —
	// otherwise every "yes/no?" trips it.
	if usrTok > 8 {
		ratio := float64(sysTok) / float64(usrTok)
		if ratio >= PersonaHeavyRatio {
			sev := Warning
			if ratio >= PersonaCriticalRatio {
				sev = Error
			}
			detections = append(detections, Detection{
				Signal:   PersonaHeavy,
				Severity: sev,
				Message:  fmt.Sprintf("system prompt is %.1f× user content (%d tokens vs %d); small substrates degrade", ratio, sysTok, usrTok),
				Metric:   ratio,
			})
		}
	}

	return Assessment{
		Detections: detections,
		Score:      computeScore(detections),
	}
}

// computeScore maps detections to a 0.0–1.0 assessment score. Any
// Error severity caps at 1.0; otherwise it's the warning count
// scaled (each warning contributes 0.3, capped at 0.9 to leave
// room for Error to dominate).
func computeScore(d []Detection) float64 {
	if len(d) == 0 {
		return 0.0
	}
	score := 0.0
	for _, det := range d {
		switch det.Severity {
		case Error:
			return 1.0
		case Warning:
			score += 0.3
		case Info:
			score += 0.05
		}
	}
	if score > 0.9 {
		score = 0.9
	}
	return score
}

// DefaultApproxTokens estimates tokens as bytes/4. Decent for
// English text; over-counts CJK and code-heavy content. Acceptable
// for the "is this prompt huge" signals we're trying to detect —
// off-by-30% is still a useful signal when the question is "is
// the prompt 4× the user content."
func DefaultApproxTokens(s string) int {
	return (len(s) + 3) / 4
}

// Inspector is the integration shim for adapters: it bundles
// reusable inspection config (context size, tokenizer) so call sites
// don't have to reconstruct a CallInspection per call. Adapters take
// an `*Inspector` in their Config and run pre-call inspection when
// it's non-nil.
//
// Future v3 phases will extend Inspector with per-action state
// (rolling medians, calibration history) — keeping the shim now
// avoids touching every adapter call site again later.
type Inspector struct {
	// ContextSize is the substrate's context window in tokens.
	// Zero disables the context-overflow detectors.
	ContextSize int
	// ApproxTokens estimates the token count of a string. nil →
	// DefaultApproxTokens.
	ApproxTokens func(s string) int
}

// Inspect runs the prompt-side detectors on one call's system
// prompt + user content. Returns the Assessment.
func (i *Inspector) Inspect(systemPrompt, userContent string) Assessment {
	if i == nil {
		return Assessment{}
	}
	return Inspect(CallInspection{
		SystemPrompt: systemPrompt,
		UserContent:  userContent,
		ContextSize:  i.ContextSize,
		ApproxTokens: i.ApproxTokens,
	})
}

// Tunables for v3.1. Exported so operators / tests can override.
// Names use the same casing as the underlying constant style for
// clarity (these are values not Go constants because operators
// may want runtime configurability later).
var (
	// ApproachingThreshold is the fraction of context window that
	// triggers InputApproachingContext. 0.80 = 80%.
	ApproachingThreshold = 0.80

	// PersonaHeavyRatio is the system-prompt/user-content ratio
	// that triggers a warning. 3.0 = system prompt is 3× user.
	PersonaHeavyRatio = 3.0

	// PersonaCriticalRatio escalates to Error severity when the
	// ratio exceeds this value. 10.0 = the Hermes-overload regime
	// (4097/122 = 33×).
	PersonaCriticalRatio = 10.0
)
