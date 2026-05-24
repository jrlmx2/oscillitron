package recomposer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/classification"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// AdapterSynth is a generic Synthesizer that uses any adapter.Adapter
// (Anthropic, Hermes, future OpenAI) for the synthesis call. It dispatches
// the merge as a Process AP, encoding the synthesizer system prompt and
// both payloads in the input. The adapter's Process playbook is expected
// to return the standard `{content, confidence}` shape — which every
// adapter in this codebase already produces.
//
// Why this exists: AnthropicSynthesizer hard-codes the Anthropic Messages
// API. For local-Hermes orchestration (and for OpenAI-style providers
// later), we need synthesis to use the same substrate as drafting — using
// a frontier model for synthesis would smuggle frontier capability into
// the cheap path and break the cost-wedge measurement.
//
// Usage:
//
//	a, _ := hermes.New(hermes.SingleEndpoint("http://127.0.0.1:8642", ""))
//	synth := recomposer.AdapterSynth{Adapter: a}
//	rec := recomposer.Synth{Synthesizer: synth}
//	rec.Recompose(ctx, spec, payloads)
type AdapterSynth struct {
	// Adapter is the substrate used for synthesis calls. Required.
	Adapter adapter.Adapter
	// SystemPromptOverride lets callers replace the default synthesis
	// preamble. Most callers should leave this empty — the default has
	// been tuned to "integrate not concatenate" and to honor task-
	// implied brevity. See AnthropicSynthesizer's prompt history if you
	// want to know why this matters.
	SystemPromptOverride string
}

// adapterSynthDefaultPrompt mirrors the tuned default from
// AnthropicSynthesizer. Kept inline so changes to one don't silently
// drift from the other.
const adapterSynthDefaultPrompt = `You are a synthesizer. ` +
	`You receive two pieces of work that both attempt the SAME task. ` +
	`Produce a single best answer to that task. ` +
	`Critical: do NOT union or concatenate the inputs. If the inputs propose competing alternatives (e.g., different time slots, different phrasings, different action choices), PICK ONE — the one that best fits the task. ` +
	`If the inputs cover overlapping content, deduplicate. ` +
	`If the task implies brevity, your output must be brief; do not preserve content from inputs that violate that implication just because both inputs included it. ` +
	`The output should look like one clean answer, not a merger.`

// Name implements Synthesizer.
func (s AdapterSynth) Name() string {
	if s.Adapter == nil {
		return "adapter-synth(<nil>)"
	}
	return "adapter-synth(" + s.Adapter.Name() + ")"
}

// Synthesize implements Synthesizer. Builds a Process AP whose input
// carries the synth prompt + both payloads, dispatches via the wrapped
// adapter, and parses the standard process-playbook response.
func (s AdapterSynth) Synthesize(ctx context.Context, req SynthesizeRequest) (SynthesizeResponse, error) {
	if s.Adapter == nil {
		return SynthesizeResponse{}, fmt.Errorf("adapter-synth: Adapter is required")
	}
	preamble := s.SystemPromptOverride
	if preamble == "" {
		preamble = adapterSynthDefaultPrompt
	}
	input := renderAdapterSynthInput(preamble, req)
	env := session.NewRoot(
		session.ID(fmt.Sprintf("synth-step-%d", req.StepIndex)),
		input,
		`{"content": "<merged>", "confidence": <0..1>}`,
		classification.Internal,
		session.Budget{TokensRemaining: 16_000, DepthRemaining: 1},
	)
	env.Evaluate = &session.Evaluate{
		Playbook:   session.PlaybookProcess,
		Confidence: 1.0,
	}
	out, err := s.Adapter.Execute(ctx, env)
	if err != nil {
		return SynthesizeResponse{}, fmt.Errorf("adapter-synth: execute: %w", err)
	}
	if out.Execute == nil || out.Execute.ReturnResult == nil {
		return SynthesizeResponse{}, fmt.Errorf("adapter-synth: empty return_result")
	}
	rr := out.Execute.ReturnResult
	return SynthesizeResponse{
		Content:    rr.Result.Content,
		Kind:       rr.Result.Kind,
		Confidence: rr.Confidence,
		TokensUsed: out.Execute.TokensUsed,
	}, nil
}

func renderAdapterSynthInput(preamble string, req SynthesizeRequest) string {
	var b strings.Builder
	b.WriteString(preamble)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Reduction step %d (spec=%s).\n\n", req.StepIndex, req.RecomposeSpec)
	b.WriteString("Left:\n")
	b.WriteString(req.Left.Result.Content)
	b.WriteString("\n\nRight:\n")
	b.WriteString(req.Right.Result.Content)
	b.WriteString("\n\nIntegrate Left and Right. ")
	b.WriteString(`Return JSON: {"content": "<the single best answer>", "confidence": <0..1>}.`)
	return b.String()
}

// Compile-time check.
var _ Synthesizer = AdapterSynth{}

// silence unused-import warning on encoding/json when builds tighten.
var _ = json.Marshal
