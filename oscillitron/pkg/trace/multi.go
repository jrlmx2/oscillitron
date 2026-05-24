package trace

import (
	"context"
	"log/slog"
)

// Multi fans every event out to N tracers. Use to combine local
// visibility (Slog → stderr) with remote shipping (a backend that
// exports to OTel/Langfuse/etc.) without forcing operators to pick
// one. Order is deterministic — backends are called in slice order.
//
// A trace.Tracer error in one backend doesn't stop the others; each
// backend handles its own failures internally (the Tracer interface
// is fire-and-forget, no error return).
type Multi []Tracer

// Event implements Tracer.
func (m Multi) Event(ctx context.Context, level slog.Level, name string, attrs ...slog.Attr) {
	for _, t := range m {
		if t == nil {
			continue
		}
		t.Event(ctx, level, name, attrs...)
	}
}

var _ Tracer = Multi(nil)
