package grader

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jrlmx2/oscillitron/pkg/adapter/stub"
	"github.com/jrlmx2/oscillitron/pkg/session"
	"github.com/jrlmx2/oscillitron/pkg/vram"
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

// --- Governor wiring ---

// testModelSpec returns a small valid ModelSpec so NewGovernor accepts
// the config. The exact numbers don't matter for grader tests — we
// only care about Acquire/Release ordering, which the Ceiling controls.
func testModelSpec() vram.ModelSpec {
	return vram.ModelSpec{
		Name: "grader-test", Layers: 1, KVHiddenDim: 1, KVDtypeBytes: 2, ContextSize: 1,
	}
}

func TestAdapterGrader_Governor_AcquireSurroundsExecute(t *testing.T) {
	// Governor with ceiling=1. The first grade holds the only slot;
	// a concurrent grade must block until the first releases.
	g, err := vram.NewGovernor(vram.GovernorConfig{Model: testModelSpec(), Ceiling: 1})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{
				Kind:    "result",
				Content: "relevance=4,tone=4,completeness=4,professionalism=4,notes=ok",
			}, 0.8)
	gr := AdapterGrader{Adapter: a, Governor: g}

	// First grade in a goroutine that we control via a started/done
	// pair. While it holds the slot, a second grade should block.
	started := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		// Acquire a lease manually to keep the slot held — we want to
		// observe that the *second* Grade waits.
		lease, err := g.Acquire(context.Background())
		if err != nil {
			first <- err
			return
		}
		close(started)
		<-release
		lease.Release()
		first <- nil
	}()
	<-started

	// Second grade — must block on Acquire until release.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = gr.Grade(ctx, Request{Task: "x", Candidate: "y"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded while governor at capacity; got %v", err)
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("manual lease err: %v", err)
	}

	// Now Grade succeeds.
	res, err := gr.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err != nil {
		t.Fatalf("Grade after release: %v", err)
	}
	if res.Score.Total() != 16 {
		t.Errorf("Total = %d, want 16", res.Score.Total())
	}

	// After completion, governor should be clean.
	snap := g.Snapshot(context.Background())
	if snap.ActiveLeases != 0 {
		t.Errorf("ActiveLeases after Grade = %d, want 0", snap.ActiveLeases)
	}
}

func TestAdapterGrader_Governor_ReleasesOnAdapterError(t *testing.T) {
	// If the adapter Execute fails, the lease must still be released —
	// otherwise the governor would leak slots over time.
	g, err := vram.NewGovernor(vram.GovernorConfig{Model: testModelSpec(), Ceiling: 1})
	if err != nil {
		t.Fatalf("NewGovernor: %v", err)
	}
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{Content: "garbage-output"}, 0.5) // parse will fail
	gr := AdapterGrader{Adapter: a, Governor: g}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = gr.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
		}()
	}
	wg.Wait()

	snap := g.Snapshot(context.Background())
	if snap.ActiveLeases != 0 {
		t.Errorf("ActiveLeases after error-path Grades = %d, want 0 (lease leak)", snap.ActiveLeases)
	}
}

func TestAdapterGrader_NoGovernor_StillWorks(t *testing.T) {
	// Backwards compatibility: leaving Governor nil is the legacy
	// behavior — Grade runs without VRAM coordination.
	a := stub.New("test").
		WithDefaultPlaybook(session.PlaybookProcess).
		WithReturnResult(session.PlaybookProcess,
			session.Payload{
				Kind:    "result",
				Content: "relevance=5,tone=5,completeness=5,professionalism=5,notes=ok",
			}, 1.0)
	gr := AdapterGrader{Adapter: a} // no Governor
	res, err := gr.Grade(context.Background(), Request{Task: "x", Candidate: "y"})
	if err != nil {
		t.Fatalf("Grade without governor: %v", err)
	}
	if res.Score.Total() != 20 {
		t.Errorf("Total = %d, want 20", res.Score.Total())
	}
}
