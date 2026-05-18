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
	tr.Event(context.Background(), "hop", slog.String("from", "a"), slog.Int("n", 1))
	got := buf.String()
	if !strings.Contains(got, "msg=hop") || !strings.Contains(got, "from=a") || !strings.Contains(got, "n=1") {
		t.Errorf("event missing fields: %q", got)
	}
}

func TestDiscardNoop(t *testing.T) {
	Discard{}.Event(context.Background(), "x", slog.String("k", "v"))
}

func TestSlogNilLoggerUsesDefault(t *testing.T) {
	// Just shouldn't panic.
	Slog{}.Event(context.Background(), "x")
}
