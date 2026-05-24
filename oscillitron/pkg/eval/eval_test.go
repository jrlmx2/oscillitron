package eval

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func echoRunner(_ context.Context, t Task) (string, error) {
	return t.Prompt, nil
}

func TestSubstringGraderMatches(t *testing.T) {
	s, err := SubstringGrader{}.Grade(context.Background(),
		Task{Reference: "hello"}, "Hello, World")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.Value != 1.0 {
		t.Errorf("Value = %f, want 1.0", s.Value)
	}
}

func TestSubstringGraderMisses(t *testing.T) {
	s, _ := SubstringGrader{}.Grade(context.Background(),
		Task{Reference: "xyz"}, "no match here")
	if s.Value != 0 {
		t.Errorf("Value = %f, want 0", s.Value)
	}
}

func TestSubstringGraderNoReference(t *testing.T) {
	s, _ := SubstringGrader{}.Grade(context.Background(),
		Task{}, "anything")
	if s.Value != 0 || !strings.Contains(s.Reason, "no reference") {
		t.Errorf("unexpected Score = %+v", s)
	}
}

func TestRunHappyPath(t *testing.T) {
	tasks := []Task{
		{ID: "1", Prompt: "the quick brown fox", Reference: "fox"},
		{ID: "2", Prompt: "hello world", Reference: "world"},
		{ID: "3", Prompt: "no match", Reference: "absent"},
	}
	r, err := Run(context.Background(), tasks, echoRunner, SubstringGrader{}, RunOpts{PassThreshold: 0.5})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if r.Total != 3 || r.Errored != 0 {
		t.Errorf("Total/Errored = %d/%d, want 3/0", r.Total, r.Errored)
	}
	if r.PassCount != 2 {
		t.Errorf("PassCount = %d, want 2", r.PassCount)
	}
	// Mean: (1 + 1 + 0) / 3 = 0.6667
	if r.MeanScore < 0.66 || r.MeanScore > 0.67 {
		t.Errorf("MeanScore = %f, want ~0.667", r.MeanScore)
	}
	if r.PassRate < 0.66 || r.PassRate > 0.67 {
		t.Errorf("PassRate = %f, want ~0.667", r.PassRate)
	}
}

func TestRunContinuesOnRunnerError(t *testing.T) {
	tasks := []Task{
		{ID: "1", Prompt: "ok", Reference: "ok"},
		{ID: "2", Prompt: "boom", Reference: "boom"},
		{ID: "3", Prompt: "ok2", Reference: "ok2"},
	}
	runner := func(_ context.Context, ta Task) (string, error) {
		if ta.ID == "2" {
			return "", errors.New("simulated runner failure")
		}
		return ta.Prompt, nil
	}
	r, err := Run(context.Background(), tasks, runner, SubstringGrader{}, RunOpts{})
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if r.Errored != 1 {
		t.Errorf("Errored = %d, want 1", r.Errored)
	}
	if r.Results[1].Err == nil {
		t.Error("Results[1] should carry the runner error")
	}
}

func TestRunRejectsNilDeps(t *testing.T) {
	if _, err := Run(context.Background(), nil, nil, SubstringGrader{}, RunOpts{}); err == nil {
		t.Error("want error for nil runner")
	}
	if _, err := Run(context.Background(), nil, echoRunner, nil, RunOpts{}); err == nil {
		t.Error("want error for nil grader")
	}
}

func TestRunRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, []Task{{ID: "x", Prompt: "p", Reference: "p"}}, echoRunner, SubstringGrader{}, RunOpts{})
	if err == nil {
		t.Error("want context error")
	}
}
