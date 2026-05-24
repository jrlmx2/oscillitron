package trace

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestWithCorrelation_StampsAndReads(t *testing.T) {
	ctx := WithCorrelation(context.Background(), "case", "c-001")
	ctx = WithCorrelation(ctx, "orchestrator", "vote-3")
	pairs := CorrelationFrom(ctx)
	if len(pairs) != 2 {
		t.Fatalf("pairs = %d, want 2", len(pairs))
	}
	if pairs[0].Key != "case" || pairs[0].Value != "c-001" {
		t.Errorf("pairs[0] = %+v", pairs[0])
	}
	if pairs[1].Key != "orchestrator" || pairs[1].Value != "vote-3" {
		t.Errorf("pairs[1] = %+v", pairs[1])
	}
}

func TestWithCorrelation_SameKeyOverwrites(t *testing.T) {
	ctx := WithCorrelation(context.Background(), "case", "c-001")
	ctx = WithCorrelation(ctx, "case", "c-002")
	pairs := CorrelationFrom(ctx)
	if len(pairs) != 1 {
		t.Fatalf("pairs = %d, want 1 (overwrite, not append)", len(pairs))
	}
	if pairs[0].Value != "c-002" {
		t.Errorf("pairs[0].Value = %q, want c-002", pairs[0].Value)
	}
}

func TestWithCorrelation_PreservesOrderOnOverwrite(t *testing.T) {
	ctx := WithCorrelation(context.Background(), "case", "c-001")
	ctx = WithCorrelation(ctx, "orchestrator", "vote-3")
	ctx = WithCorrelation(ctx, "case", "c-002") // overwrite first
	pairs := CorrelationFrom(ctx)
	if len(pairs) != 2 {
		t.Fatalf("pairs = %d, want 2", len(pairs))
	}
	if pairs[0].Key != "case" || pairs[1].Key != "orchestrator" {
		t.Errorf("order changed: %+v", pairs)
	}
}

func TestCorrelationFrom_NilSafe(t *testing.T) {
	if got := CorrelationFrom(nil); got != nil {
		t.Errorf("CorrelationFrom(nil) = %v, want nil", got)
	}
	if got := CorrelationFrom(context.Background()); got != nil {
		t.Errorf("CorrelationFrom(empty) = %v, want nil", got)
	}
}

func TestWithCorrelation_NilCtxIsBackground(t *testing.T) {
	ctx := WithCorrelation(nil, "k", "v") //nolint:staticcheck
	if ctx == nil {
		t.Fatal("expected non-nil ctx")
	}
	if len(CorrelationFrom(ctx)) != 1 {
		t.Errorf("expected 1 pair on nil-derived ctx")
	}
}

func TestSlog_PrependsCorrelationOnEvent(t *testing.T) {
	var buf bytes.Buffer
	tracer := Slog{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	ctx := WithCorrelation(context.Background(), "case", "c-001")
	ctx = WithCorrelation(ctx, "orchestrator", "vote-3")

	Info(tracer, ctx, "test.event", slog.String("payload", "hello"))

	out := buf.String()
	for _, want := range []string{"case=c-001", "orchestrator=vote-3", "payload=hello", "msg=test.event"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestSlog_NoCorrelation_NoExtraAttrs(t *testing.T) {
	var buf bytes.Buffer
	tracer := Slog{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	Info(tracer, context.Background(), "test.event", slog.String("payload", "hello"))

	out := buf.String()
	if !strings.Contains(out, "payload=hello") {
		t.Errorf("payload missing: %s", out)
	}
	if strings.Contains(out, "case=") {
		t.Errorf("unexpected correlation attr without stamp: %s", out)
	}
}

func TestDiscard_StaysQuiet(t *testing.T) {
	// Discard ignores everything, including correlation.
	ctx := WithCorrelation(context.Background(), "case", "c-001")
	Discard{}.Event(ctx, slog.LevelInfo, "irrelevant")
	// No-op: just confirming no panic.
}
