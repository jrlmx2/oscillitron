// CLAUDE GENERATED
package suffix

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// stubLoader returns a fixed slice + error for tests.
type stubLoader struct {
	name  string
	cases []benchmark.Case
	err   error
}

func (s stubLoader) Name() string                                  { return s.name }
func (s stubLoader) Load(_ context.Context) ([]benchmark.Case, error) { return s.cases, s.err }

func TestName_DelegatesToInner(t *testing.T) {
	l := Loader{Inner: stubLoader{name: "gpqa-diamond"}}
	if got := l.Name(); got != "gpqa-diamond" {
		t.Errorf("Name() = %q, want gpqa-diamond", got)
	}
}

func TestName_NilInner_ReturnsSentinel(t *testing.T) {
	if got := (Loader{}).Name(); got != "suffix(<nil>)" {
		t.Errorf("Name() = %q, want suffix(<nil>)", got)
	}
}

func TestLoad_AppendsSuffixToEveryCase(t *testing.T) {
	inner := stubLoader{
		cases: []benchmark.Case{
			{ID: "c1", Prompt: "What is X?", Expected: "A"},
			{ID: "c2", Prompt: "What is Y?", Expected: "B"},
		},
	}
	l := Loader{Inner: inner, Suffix: "\n\nAnswer: X"}
	got, err := l.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	wantPrompts := []string{
		"What is X?\n\nAnswer: X",
		"What is Y?\n\nAnswer: X",
	}
	for i, want := range wantPrompts {
		if got[i].Prompt != want {
			t.Errorf("got[%d].Prompt = %q, want %q", i, got[i].Prompt, want)
		}
	}
}

func TestLoad_OtherFieldsPreserved(t *testing.T) {
	inner := stubLoader{
		cases: []benchmark.Case{
			{ID: "c1", Prompt: "Q", Expected: "A", Metadata: map[string]string{"subdomain": "physics"}},
		},
	}
	l := Loader{Inner: inner, Suffix: ".END"}
	got, _ := l.Load(context.Background())
	if got[0].ID != "c1" || got[0].Expected != "A" || got[0].Metadata["subdomain"] != "physics" {
		t.Errorf("non-Prompt fields mutated: %+v", got[0])
	}
}

func TestLoad_EmptySuffix_NoMutation(t *testing.T) {
	inner := stubLoader{
		cases: []benchmark.Case{
			{ID: "c1", Prompt: "What is X?"},
		},
	}
	l := Loader{Inner: inner, Suffix: ""}
	got, _ := l.Load(context.Background())
	if got[0].Prompt != "What is X?" {
		t.Errorf("empty suffix should not mutate; got %q", got[0].Prompt)
	}
}

func TestLoad_InnerError_Propagates(t *testing.T) {
	innerErr := errors.New("disk gone")
	l := Loader{Inner: stubLoader{err: innerErr}, Suffix: "x"}
	_, err := l.Load(context.Background())
	if !errors.Is(err, innerErr) {
		t.Errorf("inner error should propagate; got %v", err)
	}
}

func TestLoad_NilInner_ReturnsSentinel(t *testing.T) {
	_, err := Loader{}.Load(context.Background())
	if err == nil {
		t.Fatal("expected error with nil Inner")
	}
	if !strings.Contains(err.Error(), "Inner") {
		t.Errorf("error should mention Inner; got %v", err)
	}
}

func TestLoad_PreservesOrder(t *testing.T) {
	inner := stubLoader{
		cases: []benchmark.Case{
			{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
		},
	}
	got, _ := Loader{Inner: inner, Suffix: ".s"}.Load(context.Background())
	for i, want := range []string{"a", "b", "c", "d"} {
		if got[i].ID != want {
			t.Errorf("got[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
}
