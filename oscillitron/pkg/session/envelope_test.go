package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/classification"
)

func TestEnvelopeJSONRoundTrip_AllCategories(t *testing.T) {
	cases := []struct {
		name    string
		execute *Execute
	}{
		{
			name: "emit_subtree",
			execute: &Execute{
				Category: CategoryEmitSubtree,
				EmitSubtree: &EmitSubtreePayload{
					SubAPs: []SubAPSeed{{
						Input:          Payload{Kind: "task", Content: "find the bug"},
						OutputSchema:   "{line, fix}",
						Classification: classification.Internal,
					}},
					Recompose: RecomposePairwise,
				},
				TokensUsed: 240,
			},
		},
		{
			name: "return_result",
			execute: &Execute{
				Category: CategoryReturnResult,
				ReturnResult: &ReturnResultPayload{
					Result:     Payload{Kind: "result", Content: "off-by-one at line 12"},
					Confidence: 0.92,
					Signals: Signals{
						Contradictions: []string{},
						OpenQuestions:  []string{"is the test suite complete?"},
					},
				},
				TokensUsed: 510,
			},
		},
		{
			name: "verifier_signal",
			execute: &Execute{
				Category: CategoryVerifierSignal,
				VerifierSignal: &VerifierSignalPayload{
					Verdict: VerdictIssues,
					Issues: []Issue{
						{Severity: SeverityWarning, Where: "line 12", What: "loop bound suspicious"},
					},
				},
				TokensUsed: 80,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := ID("ap-parent")
			scope := ScopeHandle("scope-parent")
			original := Envelope{
				SchemaVersion:  SchemaVersion,
				ID:             "ap-001",
				ParentID:       &parent,
				RootID:         "ap-root",
				Path:           []ID{"ap-root", "ap-parent", "ap-001"},
				ScopeHandle:    &scope,
				Input:          Payload{Kind: "task", Content: "code under review", ContentHash: "sha256:abc"},
				OutputSchema:   "report off-by-one bugs as {line, fix}",
				Classification: classification.Internal,
				Budget:         Budget{TokensRemaining: 4096, DepthRemaining: 5},
				Evaluate: &Evaluate{
					Playbook:   PlaybookProcess,
					Rationale:  "input is a task; no plan needed",
					Confidence: 0.81,
					TokensUsed: 120,
				},
				Execute:    c.execute,
				ExitReason: ExitDone,
				Trace:      Trace{TokensInput: 1024, TokensOutput: 256, DurationMs: 1830, CostUSD: 0.00012},
			}

			data, err := json.Marshal(&original)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var round Envelope
			if err := json.Unmarshal(data, &round); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if round.ID != original.ID || round.RootID != original.RootID {
				t.Errorf("ID/RootID lost: %+v", round)
			}
			if round.ParentID == nil || *round.ParentID != parent {
				t.Errorf("ParentID lost: %v", round.ParentID)
			}
			if round.ScopeHandle == nil || *round.ScopeHandle != scope {
				t.Errorf("ScopeHandle lost: %v", round.ScopeHandle)
			}
			if len(round.Path) != 3 {
				t.Errorf("Path lost: %v", round.Path)
			}
			if round.Evaluate == nil || round.Evaluate.Playbook != PlaybookProcess {
				t.Errorf("Evaluate lost: %+v", round.Evaluate)
			}
			if round.Execute == nil || round.Execute.Category != c.execute.Category {
				t.Fatalf("Execute lost: %+v", round.Execute)
			}
			// Category-discriminated payload survived.
			switch c.execute.Category {
			case CategoryEmitSubtree:
				if round.Execute.EmitSubtree == nil || len(round.Execute.EmitSubtree.SubAPs) != 1 {
					t.Errorf("emit_subtree payload lost: %+v", round.Execute)
				}
				if round.Execute.EmitSubtree.Recompose != RecomposePairwise {
					t.Errorf("recompose lost")
				}
			case CategoryReturnResult:
				if round.Execute.ReturnResult == nil || round.Execute.ReturnResult.Confidence != 0.92 {
					t.Errorf("return_result payload lost: %+v", round.Execute)
				}
			case CategoryVerifierSignal:
				if round.Execute.VerifierSignal == nil || round.Execute.VerifierSignal.Verdict != VerdictIssues {
					t.Errorf("verifier_signal payload lost: %+v", round.Execute)
				}
			}
		})
	}
}

func TestEnvelopePredicates(t *testing.T) {
	emitDone := &Execute{Category: CategoryEmitSubtree, EmitSubtree: &EmitSubtreePayload{
		SubAPs:    []SubAPSeed{{Input: Payload{Kind: "task"}}},
		Recompose: RecomposeSequential,
	}}
	emitEmpty := &Execute{Category: CategoryEmitSubtree, EmitSubtree: &EmitSubtreePayload{Recompose: RecomposeNone}}
	returnResult := &Execute{Category: CategoryReturnResult, ReturnResult: &ReturnResultPayload{}}
	verifierSignal := &Execute{Category: CategoryVerifierSignal, VerifierSignal: &VerifierSignalPayload{Verdict: VerdictPass}}

	cases := []struct {
		name                      string
		env                       Envelope
		complete, leaf, inhibited bool
	}{
		{"in-flight (no exit reason)", Envelope{}, false, false, false},
		{"done, emit_subtree with children", Envelope{ExitReason: ExitDone, Execute: emitDone}, true, false, false},
		{"done, emit_subtree empty (leaf)", Envelope{ExitReason: ExitDone, Execute: emitEmpty}, true, true, false},
		{"done, return_result (leaf)", Envelope{ExitReason: ExitDone, Execute: returnResult}, true, true, false},
		{"done, verifier_signal (leaf)", Envelope{ExitReason: ExitDone, Execute: verifierSignal}, true, true, false},
		{"inhibited", Envelope{ExitReason: ExitInhibited}, true, true, true},
		{"budget exhausted", Envelope{ExitReason: ExitBudgetExhausted}, true, true, false},
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

func TestOmitemptyWhenZero(t *testing.T) {
	env := Envelope{ID: "ap-inflight", RootID: "ap-inflight", Path: []ID{"ap-inflight"}}
	data, err := json.Marshal(&env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, field := range []string{`"parent_id":`, `"scope_handle":`, `"evaluate":`, `"execute":`, `"verify_spec":`, `"audit":`, `"exit_reason":`} {
		if strings.Contains(s, field) {
			t.Errorf("expected %s to be omitted when zero; got %s", field, s)
		}
	}
}

func TestNewRoot(t *testing.T) {
	env := NewRoot("ap-root", "solve the puzzle", "{answer}", classification.Internal,
		Budget{TokensRemaining: 8192, DepthRemaining: 6})
	if env.ID != "ap-root" {
		t.Errorf("ID: got %q", env.ID)
	}
	if env.RootID != env.ID {
		t.Errorf("RootID should equal ID at root; got %q vs %q", env.RootID, env.ID)
	}
	if env.ParentID != nil {
		t.Errorf("root should have no ParentID; got %v", env.ParentID)
	}
	if env.ScopeHandle != nil {
		t.Errorf("root should have no ScopeHandle; got %v", env.ScopeHandle)
	}
	if len(env.Path) != 1 || env.Path[0] != env.ID {
		t.Errorf("Path should be [self] at root; got %v", env.Path)
	}
	if env.Input.Kind != "task" {
		t.Errorf("root input should be a task; got %q", env.Input.Kind)
	}
	if env.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion not stamped: %q", env.SchemaVersion)
	}
	if env.Depth() != 1 {
		t.Errorf("root Depth should be 1; got %d", env.Depth())
	}
}

func TestNewChild(t *testing.T) {
	root := NewRoot("ap-root", "decompose", "{steps}", classification.Internal,
		Budget{TokensRemaining: 8192, DepthRemaining: 6})
	seed := SubAPSeed{
		Input:          Payload{Kind: "task", Content: "step 1"},
		OutputSchema:   "{step_result}",
		Classification: classification.Internal,
	}
	child := NewChild(&root, seed, "ap-child", "scope-root", Budget{TokensRemaining: 4096, DepthRemaining: 5})

	if child.ParentID == nil || *child.ParentID != root.ID {
		t.Errorf("ParentID not stamped: %v", child.ParentID)
	}
	if child.RootID != root.RootID {
		t.Errorf("RootID not propagated: got %q want %q", child.RootID, root.RootID)
	}
	if len(child.Path) != 2 || child.Path[0] != root.ID || child.Path[1] != child.ID {
		t.Errorf("Path: got %v", child.Path)
	}
	if child.ScopeHandle == nil || *child.ScopeHandle != ScopeHandle("scope-root") {
		t.Errorf("ScopeHandle not stamped: %v", child.ScopeHandle)
	}
	if child.Input.Content != "step 1" {
		t.Errorf("Input not carried from seed: %v", child.Input)
	}
	if child.Depth() != 2 {
		t.Errorf("child Depth should be 2; got %d", child.Depth())
	}

	// Path on the child must not alias the parent's slice (or appending
	// to the child's path could mutate the parent's path).
	root.Path = append(root.Path, "ap-mutated")
	if len(child.Path) != 2 {
		t.Errorf("child.Path was aliased to parent's slice; mutation leaked through")
	}
}
