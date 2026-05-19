// CLAUDE GENERATED
// Gated integration test for the Hermes adapter. Skipped unless
// OSCILLITRON_HERMES_BIN is set in the environment — keeps CI green
// while letting a developer with Hermes installed locally validate
// the full stdio round-trip.
//
// Run manually:
//
//	OSCILLITRON_HERMES_BIN=/path/to/hermes-acp-server \
//	OSCILLITRON_HERMES_CWD=/abs/path/to/workspace \
//	go test -count=1 -tags=integration ./internal/test/hermes/...
package hermes_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/hermes"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestHermesAdapter_RoundTrip(t *testing.T) {
	bin := os.Getenv("OSCILLITRON_HERMES_BIN")
	if bin == "" {
		t.Skip("OSCILLITRON_HERMES_BIN not set; skipping Hermes integration test")
	}
	cwd := os.Getenv("OSCILLITRON_HERMES_CWD")
	if cwd == "" {
		// Fall back to a temp dir so the test is still runnable; some
		// Hermes setups don't care about cwd content.
		cwd = t.TempDir()
	}
	if !filepath.IsAbs(cwd) {
		t.Fatalf("OSCILLITRON_HERMES_CWD must be absolute, got %q", cwd)
	}

	// First inference cold-starts the model in Ollama — on M-class
	// hardware that's 20-60s before token 1, plus inference time. Keep
	// the cap generous; the test still fails fast on protocol errors
	// because hermes.New short-circuits before the long Call().
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	a, err := hermes.New(ctx, hermes.Config{
		Name:    "code",
		BinPath: bin,
		Cwd:     cwd,
	})
	if err != nil {
		t.Fatalf("hermes.New: %v", err)
	}
	defer a.Close()

	out, err := a.Call(ctx, session.Envelope{
		ID:        "test-1",
		Type:      session.TypeAnalyze,
		Objective: "Say the word 'pong' and nothing else.",
		Input: session.Input{
			Type:    "prompt",
			Content: "ping",
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.ExitReason != session.ExitDone {
		t.Errorf("ExitReason = %q, want %q (verdict=%q)",
			out.ExitReason, session.ExitDone, out.Verdict)
	}
	if out.Verdict == "" {
		t.Error("Verdict is empty — expected at least some text from Hermes")
	}
	t.Logf("verdict: %q", out.Verdict)
}
