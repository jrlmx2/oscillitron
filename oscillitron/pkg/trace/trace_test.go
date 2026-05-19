// CLAUDE GENERATED
package trace

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSlogEmitsEvent(t *testing.T) {
	var buf bytes.Buffer
	tr := Slog{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	tr.Event(context.Background(), slog.LevelInfo, "hop", slog.String("from", "a"), slog.Int("n", 1))
	got := buf.String()
	if !strings.Contains(got, "msg=hop") || !strings.Contains(got, "from=a") || !strings.Contains(got, "n=1") {
		t.Errorf("event missing fields: %q", got)
	}
}

func TestSlogRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	// Handler only emits >= Warn so Info is dropped, Error is kept.
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	tr := Slog{Logger: slog.New(h)}
	Info(tr, context.Background(), "low")
	Error(tr, context.Background(), "high")
	got := buf.String()
	if strings.Contains(got, "msg=low") {
		t.Errorf("Info should have been filtered: %q", got)
	}
	if !strings.Contains(got, "msg=high") {
		t.Errorf("Error should have been emitted: %q", got)
	}
}

func TestDiscardNoop(t *testing.T) {
	Discard{}.Event(context.Background(), slog.LevelInfo, "x", slog.String("k", "v"))
}

func TestSlogNilLoggerUsesDefault(t *testing.T) {
	// Just shouldn't panic.
	Slog{}.Event(context.Background(), slog.LevelInfo, "x")
}

func TestSugarHelpers(t *testing.T) {
	var buf bytes.Buffer
	tr := Slog{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	Info(tr, context.Background(), "an_info")
	Error(tr, context.Background(), "an_error")
	got := buf.String()
	if !strings.Contains(got, "level=INFO") || !strings.Contains(got, "msg=an_info") {
		t.Errorf("Info missing: %q", got)
	}
	if !strings.Contains(got, "level=ERROR") || !strings.Contains(got, "msg=an_error") {
		t.Errorf("Error missing: %q", got)
	}
}
