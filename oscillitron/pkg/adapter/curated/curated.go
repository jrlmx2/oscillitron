// Package curated wraps an adapter.Adapter, augmenting each Execute
// call's input with retrieved exemplars from a per-action store.
//
// This is the warm-path bridge in the three-tier curation
// architecture (warm path / cold path / substrate documented in
// the parent CLAUDE.md). The store is populated offline by the
// cold-path curation driver (v2 PR 4); this adapter reads from it
// on every hot-path Execute and prepends the top-K relevant
// exemplars to the input.
//
// Why wrap as an Adapter rather than modifying every existing one:
//
//   - One implementation works for every substrate (Hermes,
//     Anthropic, stub, future Ollama-direct, ...).
//   - Caller composes via curated.WithStore(inner, store).
//   - Inner adapters stay unaware of exemplars — separation of
//     concerns held cleanly at the adapter boundary.
//
// Evaluate is NOT augmented. Augmenting playbook selection with
// exemplars is a meta-level different from augmenting playbook
// *execution* — the right design for that hasn't been worked out
// and v0 skips it deliberately.
package curated

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jrlmx2/oscillitron/pkg/adapter"
	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/trace"
)

// Adapter wraps an inner adapter.Adapter with exemplar-augmented
// Execute calls. Zero value is not usable — Inner and Store are
// required.
type Adapter struct {
	// Inner is the substrate adapter that handles the actual call.
	// Required.
	Inner adapter.Adapter
	// Store is the per-action exemplar substrate to retrieve from.
	// Required when augmentation is desired; nil disables augmentation
	// (Execute passes through to Inner unchanged — useful for A/B
	// testing or warm-up runs before the store is populated).
	Store exemplar.Store
	// TopK is the maximum number of exemplars to retrieve per
	// Execute. Defaults to DefaultTopK (3) when zero. Set to 1 to
	// inject only the single best match; higher values give more
	// few-shot diversity at the cost of context tokens.
	TopK int
	// MaxExemplarTokens caps the total tokens the formatted exemplar
	// block can consume. After retrieval, exemplars are added
	// greedily (in BM25-rank order) until adding the next would
	// exceed this budget; remaining are dropped. 0 means no token
	// cap (still bounded by TopK).
	MaxExemplarTokens int
	// Formatter renders the retrieved exemplars into the prefix
	// block that gets prepended to env.Input.Content. Defaults to
	// DefaultFormatter when nil. Custom formatters can match
	// substrate-specific shapes (e.g., a model that was fine-tuned
	// on a particular few-shot template).
	Formatter func(exemplars []exemplar.Exemplar) string
	// Tracer optionally emits per-call observability events:
	//
	//   curated.exemplars_retrieved  action, count, tokens
	//   curated.passthrough          reason
	//   curated.retrieval_failed     action, err
	//
	// Nil = trace.Discard{}.
	Tracer trace.Tracer
	// ActionOverride, when non-empty, replaces env.Evaluate.Playbook
	// as the action key used for retrieval. Use when the warm-path
	// orchestrators' playbook differs from the action key the cold-
	// path curation wrote under (e.g., bench orchestrators always
	// emit PlaybookProcess but the operator curated to "qa-mcq").
	// Zero value (empty string) preserves the original behavior:
	// retrieve under env.Evaluate.Playbook.
	ActionOverride string
}

// DefaultTopK is the default number of exemplars retrieved per
// Execute when Adapter.TopK is zero. Three is enough to give
// few-shot diversity without ballooning context.
const DefaultTopK = 3

// WithStore is the canonical constructor. Wraps inner so every
// Execute call retrieves up to DefaultTopK exemplars from store
// and prepends them via DefaultFormatter. Tune via the returned
// Adapter's fields.
func WithStore(inner adapter.Adapter, store exemplar.Store) *Adapter {
	return &Adapter{Inner: inner, Store: store}
}

// Name returns "curated(<inner>)" for trace records.
func (a *Adapter) Name() string {
	if a == nil || a.Inner == nil {
		return "curated(<nil>)"
	}
	return "curated(" + a.Inner.Name() + ")"
}

// Evaluate passes through to Inner unchanged. Exemplar augmentation
// applies only to Execute — see the package doc for rationale.
func (a *Adapter) Evaluate(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if a == nil || a.Inner == nil {
		return env, fmt.Errorf("curated: Inner is required")
	}
	return a.Inner.Evaluate(ctx, env)
}

// Execute retrieves exemplars for env.Evaluate.Playbook, prepends
// the formatted block to env.Input.Content, and dispatches to the
// inner adapter. The original env is not modified (we work on a
// copy — env is a value type).
//
// Pass-through cases (Inner is called with the original env):
//   - Store is nil
//   - env.Evaluate is nil or env.Evaluate.Playbook is empty
//   - Retrieve returns 0 exemplars
//   - Retrieve returns an error (logged as trace event, not surfaced)
//   - Token-budget trim leaves 0 exemplars
//
// Inner errors propagate verbatim. Pass-through does not retry or
// fall back; if the inner call fails, the caller sees its error.
func (a *Adapter) Execute(ctx context.Context, env session.Envelope) (session.Envelope, error) {
	if a == nil || a.Inner == nil {
		return env, fmt.Errorf("curated: Inner is required")
	}
	tracer := a.Tracer
	if tracer == nil {
		tracer = trace.Discard{}
	}

	if a.Store == nil {
		trace.Info(tracer, ctx, "curated.passthrough", slog.String("reason", "nil_store"))
		return a.Inner.Execute(ctx, env)
	}
	if env.Evaluate == nil || env.Evaluate.Playbook == "" {
		trace.Info(tracer, ctx, "curated.passthrough", slog.String("reason", "no_playbook"))
		return a.Inner.Execute(ctx, env)
	}

	k := a.TopK
	if k <= 0 {
		k = DefaultTopK
	}
	action := a.ActionOverride
	if action == "" {
		action = string(env.Evaluate.Playbook)
	}

	exemplars, err := a.Store.Retrieve(ctx, action, env.Input.Content, k)
	if err != nil {
		// Don't fail Execute — substrate is best-effort. Log and
		// proceed without augmentation.
		trace.Error(tracer, ctx, "curated.retrieval_failed",
			slog.String("action", action),
			slog.String("err", err.Error()),
		)
		return a.Inner.Execute(ctx, env)
	}
	if len(exemplars) == 0 {
		trace.Info(tracer, ctx, "curated.passthrough",
			slog.String("reason", "empty_corpus"),
			slog.String("action", action),
		)
		return a.Inner.Execute(ctx, env)
	}
	if a.MaxExemplarTokens > 0 {
		exemplars = trimToBudget(exemplars, a.MaxExemplarTokens)
		if len(exemplars) == 0 {
			trace.Info(tracer, ctx, "curated.passthrough",
				slog.String("reason", "budget_excluded_all"),
				slog.String("action", action),
				slog.Int("max_tokens", a.MaxExemplarTokens),
			)
			return a.Inner.Execute(ctx, env)
		}
	}

	format := a.Formatter
	if format == nil {
		format = DefaultFormatter
	}
	prefix := format(exemplars)
	totalTokens := 0
	for _, e := range exemplars {
		totalTokens += e.Tokens
	}
	trace.Info(tracer, ctx, "curated.exemplars_retrieved",
		slog.String("action", action),
		slog.Int("count", len(exemplars)),
		slog.Int("tokens", totalTokens),
	)

	env.Input.Content = prefix + env.Input.Content
	return a.Inner.Execute(ctx, env)
}

// trimToBudget keeps exemplars in input order (BM25-rank order from
// Retrieve) until adding the next one would exceed budget. Greedy:
// once an exemplar is excluded, subsequent ones are checked
// individually (a smaller one further down the list still fits).
func trimToBudget(exemplars []exemplar.Exemplar, budget int) []exemplar.Exemplar {
	used := 0
	out := make([]exemplar.Exemplar, 0, len(exemplars))
	for _, e := range exemplars {
		if used+e.Tokens > budget {
			continue
		}
		out = append(out, e)
		used += e.Tokens
	}
	return out
}

// DefaultFormatter renders exemplars as a compact, byte-stable
// "[similar-examples] ... [/similar-examples]" block. Stability
// matters: same exemplar set across consecutive calls produces
// the same prefix bytes, which means KV-cache prefix hits compound.
func DefaultFormatter(exemplars []exemplar.Exemplar) string {
	if len(exemplars) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[similar-examples count=")
	fmt.Fprintf(&b, "%d", len(exemplars))
	b.WriteString("]\n")
	for i, e := range exemplars {
		fmt.Fprintf(&b, "Example %d:\nQ: %s\nA: %s\n\n", i+1, e.Prompt, e.Output)
	}
	b.WriteString("[/similar-examples]\n\n")
	return b.String()
}

// Compile-time check that *Adapter satisfies adapter.Adapter.
var _ adapter.Adapter = (*Adapter)(nil)
