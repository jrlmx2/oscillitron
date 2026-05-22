// CLAUDE GENERATED
package trace

import (
	"context"
	"log/slog"
	"testing"
)

type counterTracer struct {
	count int
	last  string
}

func (c *counterTracer) Event(_ context.Context, _ slog.Level, name string, _ ...slog.Attr) {
	c.count++
	c.last = name
}

func TestMulti_FansOutToAll(t *testing.T) {
	a, b, c := &counterTracer{}, &counterTracer{}, &counterTracer{}
	m := Multi{a, b, c}
	m.Event(context.Background(), slog.LevelInfo, "x")
	for i, ct := range []*counterTracer{a, b, c} {
		if ct.count != 1 {
			t.Errorf("backend[%d] count = %d, want 1", i, ct.count)
		}
		if ct.last != "x" {
			t.Errorf("backend[%d] last = %q, want x", i, ct.last)
		}
	}
}

func TestMulti_NilBackendIsSkipped(t *testing.T) {
	a := &counterTracer{}
	m := Multi{nil, a, nil}
	m.Event(context.Background(), slog.LevelInfo, "x")
	if a.count != 1 {
		t.Errorf("non-nil backend should still fire; count = %d", a.count)
	}
}

func TestMulti_EmptyIsNoop(t *testing.T) {
	Multi{}.Event(context.Background(), slog.LevelInfo, "x")
	// no panic = pass
}
