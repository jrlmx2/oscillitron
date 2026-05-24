package trace

import "context"

// correlationKey is the unexported context-value key for correlation
// pairs. Type-keyed so it can't collide with caller-defined keys.
type correlationKey struct{}

// CorrelationPair is one key=value identifying the originating
// scope of a trace event (case=gpqa-001, orchestrator=vote-3,
// attempt=2, etc.).
type CorrelationPair struct {
	Key   string
	Value string
}

// WithCorrelation returns a new context with the given key=value
// added to its correlation set. Same-key updates overwrite in
// place — semantics match "set this scope identifier to this value
// for downstream events."
//
// Stamp at boundaries where a new logical scope begins (per-case,
// per-attempt) so events emitted by descendants (runner, governor,
// adapter) carry the originating identifiers without any code at
// those layers needing to know about the bench's case ID.
//
// Cheap: each call allocates one slice + one context value.
// Correlation lookups during trace emission are O(N) in the
// number of stamped keys — typically 1-3 in practice.
func WithCorrelation(ctx context.Context, key, value string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	existing := CorrelationFrom(ctx)
	pairs := make([]CorrelationPair, 0, len(existing)+1)
	overwrote := false
	for _, p := range existing {
		if p.Key == key {
			pairs = append(pairs, CorrelationPair{Key: key, Value: value})
			overwrote = true
			continue
		}
		pairs = append(pairs, p)
	}
	if !overwrote {
		pairs = append(pairs, CorrelationPair{Key: key, Value: value})
	}
	return context.WithValue(ctx, correlationKey{}, pairs)
}

// CorrelationFrom returns the correlation pairs stamped into ctx,
// or nil if none. Order matches stamping order, with overwrites
// preserving position.
func CorrelationFrom(ctx context.Context) []CorrelationPair {
	if ctx == nil {
		return nil
	}
	pairs, _ := ctx.Value(correlationKey{}).([]CorrelationPair)
	return pairs
}
