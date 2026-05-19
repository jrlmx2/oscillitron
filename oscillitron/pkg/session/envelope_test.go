// CLAUDE GENERATED
package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/classification"
)

func TestEnvelopeJSONRoundTrip(t *testing.T) {
	parent := ID("sess-parent")
	original := Envelope{
		SchemaVersion:  SchemaVersion,
		ID:             "sess-001",
		BrainFunction:  BrainReasoning,
		Classification: classification.Internal,
		Input: Input{
			Type:        "prompt",
			Content:     "for i := 0; i <= len(xs); i++ { ... }",
			ContentHash: "sha256:abc",
		},
		OutputSchema: "report off-by-one bugs as {line, fix}",
		ParentRef:    &parent,
		Budget:       Budget{TokensRemaining: 4096, DepthRemaining: 5},
		Output: &Output{
			Content:        "off-by-one at line 12: use i < len(xs)",
			Classification: "bug_found",
			Confidence:     0.92,
			Signals:        []string{"loop bound suspicious"},
			SubAPs: []SubAPSeed{{
				BrainFunction: BrainCritic,
				Input:         Input{Type: "subap_result", Content: "verify fix"},
				OutputSchema:  "approve | reject {reason}",
			}},
			ExitReason: ExitDone,
		},
		Trace: Trace{TokensInput: 1024, TokensOutput: 256, DurationMs: 1830, CostUSD: 0.00012},
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
	if round.BrainFunction != BrainReasoning {
		t.Errorf("BrainFunction: got %q, want %q", round.BrainFunction, BrainReasoning)
	}
	if round.Output == nil {
		t.Fatal("Output lost in round-trip")
	}
	if round.Output.Confidence != 0.92 {
		t.Errorf("Confidence: got %v, want 0.92", round.Output.Confidence)
	}
	if len(round.Output.SubAPs) != 1 || round.Output.SubAPs[0].BrainFunction != BrainCritic {
		t.Errorf("SubAPs lost: %+v", round.Output.SubAPs)
	}
	if round.ParentRef == nil || *round.ParentRef != parent {
		t.Errorf("ParentRef lost: %v", round.ParentRef)
	}
}

func TestEnvelopePredicates(t *testing.T) {
	cases := []struct {
		name                              string
		env                               Envelope
		complete, leaf, inhibited bool
	}{
		{"empty: not complete", Envelope{}, false, false, false},
		{"done leaf", Envelope{Output: &Output{ExitReason: ExitDone}}, true, true, false},
		{"done with subaps (not leaf)", Envelope{Output: &Output{
			ExitReason: ExitDone,
			SubAPs:     []SubAPSeed{{BrainFunction: BrainCritic}},
		}}, true, false, false},
		{"inhibited", Envelope{Output: &Output{ExitReason: ExitInhibited}}, true, true, true},
		{"budget exhausted leaf", Envelope{Output: &Output{ExitReason: ExitBudgetExhausted}}, true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.env.IsComplete(); got != c.complete {
				t.Errorf("IsComplete: got %v, want %v", got, c.complete)
			}
			if got := c.env.IsLeaf(); got != c.leaf {
				t.Errorf("IsLeaf: got %v, want %v", got, c.leaf)
			}
			if got := c.env.IsInhibited(); got != c.inhibited {
				t.Errorf("IsInhibited: got %v, want %v", got, c.inhibited)
			}
		})
	}
}

func TestOutputOmitemptyWhenNil(t *testing.T) {
	env := Envelope{ID: "sess-inflight", BrainFunction: BrainPerception}
	data, err := json.Marshal(&env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"output":`) {
		t.Errorf("expected output to be omitted when nil; got %s", string(data))
	}
}
