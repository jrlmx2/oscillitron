// Package thinking selects the reasoning-mode policy for substrates
// that expose a per-call thinking trace (qwen3.x family, deepseek-r1,
// magistral, gpt-oss, exaone-deep, glm-5, kimi-k2-thinking, gemma4,
// etc.).
//
// Thinking is variable substrate effort, the same shape as
// stakes.AttemptScale. Both are knobs the orchestrator turns
// per-call: spend more compute on harder problems. The Policy
// abstraction lets adapters honor that decision uniformly across
// substrates without hardcoding "think: false" anywhere.
//
// The architectural lock (2026-05-24): substrates without reasoning
// mode silently no-op the flag — the per-call policy decision still
// runs, the request body still carries the flag, the substrate just
// ignores it. Cross-substrate semantics stay consistent regardless of
// which model is wired.
package thinking

import "github.com/jrlmx2/oscillitron/pkg/session"

// Policy decides whether thinking-mode should be enabled for a given
// AP call. Implementations inspect the envelope's Stakes, Playbook,
// Path, etc., to make the per-call call.
type Policy interface {
	// ShouldThink returns whether thinking-mode should be enabled.
	// The adapter layer translates this into the substrate-specific
	// request field (e.g., `"think": false` on Ollama's
	// /v1/chat/completions; equivalent surface on vLLM, LM Studio).
	ShouldThink(env session.Envelope) bool
}

// AlwaysOn enables thinking on every call. Substrate-default behavior
// for reasoning-tuned models (which already think by default if no
// flag is set). Honest baseline; expensive.
type AlwaysOn struct{}

// ShouldThink implements Policy.
func (AlwaysOn) ShouldThink(session.Envelope) bool { return true }

// AlwaysOff disables thinking on every call. Fast-bench mode; lets
// reasoning-capable substrates run at non-reasoning speed for
// apples-to-apples comparison with non-reasoning substrates.
type AlwaysOff struct{}

// ShouldThink implements Policy.
func (AlwaysOff) ShouldThink(session.Envelope) bool { return false }

// ByStakes enables thinking on high-stakes calls only. Mirrors the
// shape of stakes.AttemptScale: low stakes get the cheap path,
// medium gets the baseline, high gets the expensive variant. The
// "expensive variant" here is the reasoning trace.
//
// Default mapping: High → true; Low / Medium / unset → false. The
// zero value gives sensible behavior; operators can override the
// per-level flags if a different mapping fits a workload.
type ByStakes struct {
	// On controls which stakes levels enable thinking. Nil-safe: the
	// zero value enables thinking only for High.
	On map[string]bool
}

// ShouldThink implements Policy.
func (p ByStakes) ShouldThink(env session.Envelope) bool {
	if p.On == nil {
		return string(env.Stakes) == "high"
	}
	return p.On[string(env.Stakes)]
}

// ByPlaybook enables thinking based on the playbook selected by the
// evaluate step. Plan and Compose intrinsically benefit from
// reasoning (decomposition and synthesis are reasoning-shaped tasks);
// Process on simple MCQ rarely does. The map's zero value is "off."
type ByPlaybook struct {
	// On controls which playbooks enable thinking. Nil-safe: zero
	// value enables thinking for none (the policy becomes equivalent
	// to AlwaysOff).
	On map[session.Playbook]bool
}

// ShouldThink implements Policy.
func (p ByPlaybook) ShouldThink(env session.Envelope) bool {
	if env.Evaluate == nil {
		return false
	}
	return p.On[env.Evaluate.Playbook]
}

// Composite combines multiple policies via OR. Thinking is enabled
// when any wrapped policy says so. Useful for stacking — e.g.,
// "think on high stakes OR on plan playbook." For AND semantics,
// write a custom Policy.
type Composite struct {
	Policies []Policy
}

// ShouldThink implements Policy.
func (p Composite) ShouldThink(env session.Envelope) bool {
	for _, inner := range p.Policies {
		if inner != nil && inner.ShouldThink(env) {
			return true
		}
	}
	return false
}

// Compile-time checks.
var (
	_ Policy = AlwaysOn{}
	_ Policy = AlwaysOff{}
	_ Policy = ByStakes{}
	_ Policy = ByPlaybook{}
	_ Policy = Composite{}
)
