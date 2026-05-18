// CLAUDE GENERATED
package session

import (
	"encoding/json"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/classification"
)

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	parent := ID("sess-parent")
	original := Envelope{
		ID:             "sess-001",
		Type:           TypeAnalyze,
		Objective:      "analyze the function f for off-by-one bugs",
		Classification: classification.Internal,
		Notes: Notes{
			Constraints:  []string{"do not modify the file"},
			PriorSignals: []string{"flagged by linter"},
			ContextTags:  []string{"go", "loop"},
		},
		Input: Input{
			Type:        "prompt",
			Content:     "for i := 0; i <= len(xs); i++ { ... }",
			ContentHash: "sha256:abc",
		},
		Outcome: &Outcome{
			ExitReason:     ExitDone,
			Verdict:        "off-by-one confirmed at loop bound",
			Signals:        []string{"i <= len(xs) should be i < len(xs)"},
			Confidence:     0.92,
			OpenQuestions:  nil,
			Contradictions: nil,
			FeedsInto:      []ID{"sess-002"},
		},
		Routing: Routing{
			Model:                    "qwen3-coder-30b-awq",
			ModelHash:                "sha256:beef",
			Reason:                   "code-analysis specialist",
			ClassificationConstraint: string(classification.Internal),
		},
		Trace: Trace{
			TokensInput:   1024,
			TokensOutput:  256,
			DurationMs:    1830,
			ParentSession: &parent,
			CostUSD:       0.00012,
		},
		Audit: nil,
	}

	data, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round Envelope
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if round.ID != original.ID {
		t.Errorf("ID: got %q, want %q", round.ID, original.ID)
	}
	if round.Outcome == nil {
		t.Fatal("outcome lost in round-trip")
	}
	if round.Outcome.ExitReason != ExitDone {
		t.Errorf("ExitReason: got %q, want %q", round.Outcome.ExitReason, ExitDone)
	}
	if round.Outcome.Confidence != 0.92 {
		t.Errorf("Confidence: got %v, want 0.92", round.Outcome.Confidence)
	}
	if round.Trace.ParentSession == nil || *round.Trace.ParentSession != parent {
		t.Errorf("ParentSession lost: got %v", round.Trace.ParentSession)
	}
}

func TestEnvelopeIsTerminal(t *testing.T) {
	cases := []struct {
		name string
		env  Envelope
		want bool
	}{
		{"nil outcome is not terminal", Envelope{}, false},
		{"done is terminal", Envelope{Outcome: &Outcome{ExitReason: ExitDone}}, true},
		{"inhibited is terminal", Envelope{Outcome: &Outcome{ExitReason: ExitInhibited}}, true},
		{"budget exhausted is NOT terminal (hands to next specialist)",
			Envelope{Outcome: &Outcome{ExitReason: ExitBudgetExhausted}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.env.IsTerminal(); got != c.want {
				t.Errorf("IsTerminal() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestOutcomeOmitemptyWhenNil(t *testing.T) {
	// An envelope with no Outcome (in-flight) should not emit an "outcome": null key.
	env := Envelope{ID: "sess-inflight", Type: TypeAnalyze}
	data, err := json.Marshal(&env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); contains(got, `"outcome":`) {
		t.Errorf("expected outcome to be omitted when nil; got %s", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
