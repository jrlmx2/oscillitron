// CLAUDE GENERATED
package grader

import (
	"context"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

func TestAdapterGrader_HappyPath(t *testing.T) {
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{
				Kind:    "result",
				Content: "relevance=4,tone=5,completeness=3,professionalism=4,notes=clear and polite",
			}, 0.8)
	g := AdapterGrader{Adapter: a}
	res, err := g.Grade(context.Background(), Request{
		Task:      "decline politely",
		Candidate: "Thanks but no thanks.",
	})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if res.Score.Relevance != 4 || res.Score.Tone != 5 || res.Score.Completeness != 3 || res.Score.Professionalism != 4 {
		t.Errorf("scores wrong: %+v", res.Score)
	}
	if res.Score.Notes != "clear and polite" {
		t.Errorf("notes = %q", res.Score.Notes)
	}
	if res.Score.Total() != 16 {
		t.Errorf("Total = %d, want 16", res.Score.Total())
	}
}

func TestAdapterGrader_RejectsOutOfRange(t *testing.T) {
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{
				Content: "relevance=4,tone=7,completeness=3,professionalism=4",
			}, 0.5)
	g := AdapterGrader{Adapter: a}
	_, err := g.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err == nil {
		t.Fatal("expected error on out-of-range score")
	}
	if !strings.Contains(err.Error(), "range") {
		t.Errorf("error %q should mention range", err.Error())
	}
}

func TestAdapterGrader_RejectsMissingScores(t *testing.T) {
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{
				Content: "relevance=4,tone=5",
			}, 0.5)
	g := AdapterGrader{Adapter: a}
	_, err := g.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err == nil {
		t.Fatal("expected error on missing scores")
	}
	if !strings.Contains(err.Error(), "4 numeric scores") {
		t.Errorf("error %q should mention missing scores", err.Error())
	}
}

func TestAdapterGrader_RequiresAdapter(t *testing.T) {
	g := AdapterGrader{}
	_, err := g.Grade(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error with nil adapter")
	}
}

func TestAdapterGrader_Name(t *testing.T) {
	g := AdapterGrader{Adapter: stub.New("hermes-local")}
	if got := g.Name(); got != "adapter-grader(hermes-local)" {
		t.Errorf("Name() = %q", got)
	}
	if got := (AdapterGrader{}).Name(); got != "adapter-grader(<nil>)" {
		t.Errorf("nil-adapter Name() = %q", got)
	}
}

func TestParseKeyValueScore_TolerantsExtras(t *testing.T) {
	s, err := parseKeyValueScore(`relevance=3,tone=4,completeness=3,professionalism=4,notes=fine,extra=ignored`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Notes != "fine" {
		t.Errorf("Notes = %q", s.Notes)
	}
}

func TestParseKeyValueScore_TolerantsLeadingTrailingQuotes(t *testing.T) {
	// Some models wrap the content in stray quotes; we strip them.
	s, err := parseKeyValueScore(`"relevance=3,tone=4,completeness=3,professionalism=4"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Total() != 14 {
		t.Errorf("Total = %d, want 14", s.Total())
	}
}
