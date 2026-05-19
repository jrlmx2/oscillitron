// CLAUDE GENERATED
package hermes

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// TestNew_RefusesShortConnectionCtx proves New() bails fast when the
// caller's ctx deadline is shorter than MinConnectionTimeout — without
// trying to spawn a process. We don't supply a real BinPath; the
// timeout check must trip BEFORE exec.Command, otherwise we'd see a
// "no such file" error instead.
func TestNew_RefusesShortConnectionCtx(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := New(ctx, Config{
		Name:                 "code",
		BinPath:              "/definitely/does/not/exist/hermes-acp",
		Cwd:                  "/tmp",
		MinConnectionTimeout: 30 * time.Second,
	})
	if err == nil {
		t.Fatal("expected error on short setup ctx, got nil")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Errorf("error should mention handshake/MinConnectionTimeout: %v", err)
	}
	// And critically: error must mention OUR runway, not a process-spawn failure.
	if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "executable") {
		t.Errorf("timeout check should fire BEFORE exec.Command; got process error: %v", err)
	}
}

func TestNew_AcceptsLongCtx(t *testing.T) {
	// 10m ctx is well above the default 30s floor. The error here
	// should be from exec (since BinPath is bogus), not from the timeout check.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err := New(ctx, Config{
		Name:    "code",
		BinPath: "/definitely/does/not/exist/hermes-acp",
		Cwd:     "/tmp",
	})
	if err == nil {
		t.Fatal("expected exec error from bogus BinPath")
	}
	if strings.Contains(err.Error(), "handshake") {
		t.Errorf("timeout check should NOT fire with long ctx: %v", err)
	}
}

func TestNew_DisablesCheckOnNegativeMin(t *testing.T) {
	// Negative MinConnectionTimeout disables the check entirely. We
	// then expect exec to fail (bogus path) rather than the timeout check.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := New(ctx, Config{
		Name:                 "code",
		BinPath:              "/definitely/does/not/exist/hermes-acp",
		Cwd:                  "/tmp",
		MinConnectionTimeout: -1,
	})
	if err == nil {
		t.Fatal("expected some error")
	}
	if strings.Contains(err.Error(), "handshake") {
		t.Errorf("negative MinConnectionTimeout should disable the floor check: %v", err)
	}
}

func TestCall_RefusesShortCallCtx(t *testing.T) {
	// Build the Adapter directly (no process). The minCallTimeout
	// check fires before any ACP traffic.
	a := &Adapter{
		name:           "code",
		minCallTimeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	out, err := a.Call(ctx, session.Envelope{Objective: "x"})
	if err != nil {
		t.Fatalf("Call should return a refusal Outcome, not an error: %v", err)
	}
	if out.ExitReason != session.ExitInhibited {
		t.Errorf("ExitReason = %q, want %q", out.ExitReason, session.ExitInhibited)
	}
	found := false
	for _, s := range out.Signals {
		if s == "prompt_timeout_too_short" {
			found = true
		}
	}
	if !found {
		t.Errorf("signals missing prompt_timeout_too_short: %v", out.Signals)
	}
}

func TestCall_NoDeadlineSkipsCheck(t *testing.T) {
	// A ctx with no deadline (context.Background()) means the caller
	// won't time us out — the floor check should pass through. We use
	// a nil-client Adapter and expect to reach the panic path that
	// proves we got past the gate.
	a := &Adapter{
		name:           "code",
		minCallTimeout: 30 * time.Second,
	}
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected nil-client panic (proving we passed the timeout gate)")
		}
	}()
	_, _ = a.Call(context.Background(), session.Envelope{Objective: "x"})
}

func TestCall_NegativeMinDisablesCheck(t *testing.T) {
	a := &Adapter{
		name:           "code",
		minCallTimeout: -1, // explicitly disabled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected nil-client panic (proving the disabled check passed through)")
		}
	}()
	_, _ = a.Call(ctx, session.Envelope{Objective: "x"})
}
