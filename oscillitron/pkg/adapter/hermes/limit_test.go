// CLAUDE GENERATED
package hermes

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// TestMaxContextTokens_RefusesOversizedPrompt verifies that the
// adapter short-circuits an oversized AP rather than handing it to
// Hermes. The check is purely client-side and doesn't need a running
// Hermes process — we construct the adapter struct directly to skip
// the spawn path.
func TestMaxContextTokens_RefusesOversizedPrompt(t *testing.T) {
	a := &Adapter{
		name:             "test",
		maxContextTokens: 1000, // 4000 chars at chars/4
	}

	// 5000-char objective → ~1250 tokens, over the 1000 cap.
	big := strings.Repeat("x", 5000)
	out, err := a.Call(context.Background(), session.Envelope{
		Objective: big,
	})
	if err != nil {
		t.Fatalf("Call returned error; want a refusal Outcome: %v", err)
	}
	if out.ExitReason != session.ExitInhibited {
		t.Errorf("ExitReason = %q, want %q", out.ExitReason, session.ExitInhibited)
	}
	found := false
	for _, s := range out.Signals {
		if s == "prompt_exceeds_max_context" {
			found = true
		}
	}
	if !found {
		t.Errorf("signals missing prompt_exceeds_max_context: %v", out.Signals)
	}
	if out.TokensInput < 1000 {
		t.Errorf("TokensInput = %d, want > 1000 (the cap)", out.TokensInput)
	}
}

// TestMaxContextTokens_ZeroDisablesCheck verifies that a zero-valued
// cap doesn't trip on any prompt size. This is the default behavior
// for adapters that don't set MaxContextTokens.
func TestMaxContextTokens_ZeroDisablesCheck(t *testing.T) {
	a := &Adapter{
		name:             "test",
		maxContextTokens: 0,
	}
	// With maxContextTokens=0 the early-return check is skipped, so
	// Call proceeds to the ACP client — which is nil here, so it would
	// panic. We catch the panic to confirm we got *past* the check.
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected nil-client panic (proving we passed the gate); got no panic")
		}
	}()
	_, _ = a.Call(context.Background(), session.Envelope{
		Objective: strings.Repeat("x", 100_000), // huge, but check is off
	})
}
