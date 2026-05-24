package grader

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/benchmark"
)

// fixedGrader returns a configured verdict regardless of input.
type fixedGrader struct {
	name string
	pass bool
	err  error
}

func (f fixedGrader) Name() string { return f.name }
func (f fixedGrader) Grade(_ context.Context, _ benchmark.Case, _ benchmark.Answer) (benchmark.Verdict, error) {
	if f.err != nil {
		return benchmark.Verdict{GraderName: f.name}, f.err
	}
	score := 0.0
	if f.pass {
		score = 1.0
	}
	return benchmark.Verdict{GraderName: f.name, Pass: f.pass, Score: score}, nil
}

func TestDual_PrimaryCanonical_SecondariesAttached(t *testing.T) {
	d := Dual{
		Primary: fixedGrader{name: "primary", pass: true},
		Secondary: []benchmark.Grader{
			fixedGrader{name: "sec-1", pass: true},
			fixedGrader{name: "sec-2", pass: false},
		},
	}
	v, err := d.Grade(context.Background(), benchmark.Case{}, benchmark.Answer{})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if !v.Pass {
		t.Error("Primary's Pass should be canonical")
	}
	if len(v.Secondary) != 2 {
		t.Fatalf("Secondary count = %d, want 2", len(v.Secondary))
	}
	if v.Secondary[0].GraderName != "sec-1" || !v.Secondary[0].Pass {
		t.Errorf("sec-1 wrong: %+v", v.Secondary[0])
	}
	if v.Secondary[1].GraderName != "sec-2" || v.Secondary[1].Pass {
		t.Errorf("sec-2 wrong: %+v", v.Secondary[1])
	}
}

func TestDual_NoSecondaries_PassesThroughPrimary(t *testing.T) {
	d := Dual{Primary: fixedGrader{name: "p", pass: true}}
	v, _ := d.Grade(context.Background(), benchmark.Case{}, benchmark.Answer{})
	if !v.Pass || len(v.Secondary) != 0 {
		t.Errorf("expected pass with no secondaries; got %+v", v)
	}
}

func TestDual_PrimaryError_Propagates(t *testing.T) {
	d := Dual{
		Primary:   fixedGrader{name: "p", err: errors.New("boom")},
		Secondary: []benchmark.Grader{fixedGrader{name: "s", pass: true}},
	}
	_, err := d.Grade(context.Background(), benchmark.Case{}, benchmark.Answer{})
	if err == nil {
		t.Fatal("expected primary error to propagate")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %q, want 'boom'", err.Error())
	}
}

func TestDual_SecondaryError_RecordedNotPropagated(t *testing.T) {
	d := Dual{
		Primary: fixedGrader{name: "p", pass: true},
		Secondary: []benchmark.Grader{
			fixedGrader{name: "s-fail", err: errors.New("judge down")},
			fixedGrader{name: "s-ok", pass: true},
		},
	}
	v, err := d.Grade(context.Background(), benchmark.Case{}, benchmark.Answer{})
	if err != nil {
		t.Fatalf("Grade should not propagate secondary errors: %v", err)
	}
	if len(v.Secondary) != 2 {
		t.Fatalf("Secondary count = %d", len(v.Secondary))
	}
	if !strings.Contains(v.Secondary[0].Notes, "judge down") {
		t.Errorf("expected error to be recorded in Notes: %q", v.Secondary[0].Notes)
	}
	if v.Secondary[0].GraderName != "s-fail" {
		t.Errorf("secondary 0 name = %q, want 's-fail'", v.Secondary[0].GraderName)
	}
}

func TestDual_RequiresPrimary(t *testing.T) {
	d := Dual{}
	_, err := d.Grade(context.Background(), benchmark.Case{}, benchmark.Answer{})
	if err == nil {
		t.Fatal("expected error with nil Primary")
	}
}

func TestAgreement(t *testing.T) {
	cases := []struct {
		name string
		v    benchmark.Verdict
		want float64
	}{
		{"no secondaries", benchmark.Verdict{Pass: true}, -1},
		{"all agree", benchmark.Verdict{
			Pass:      true,
			Secondary: []benchmark.Verdict{{Pass: true}, {Pass: true}},
		}, 1.0},
		{"all disagree", benchmark.Verdict{
			Pass:      true,
			Secondary: []benchmark.Verdict{{Pass: false}, {Pass: false}},
		}, 0.0},
		{"mixed", benchmark.Verdict{
			Pass:      true,
			Secondary: []benchmark.Verdict{{Pass: true}, {Pass: false}, {Pass: true}},
		}, 2.0 / 3.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Agreement(c.v)
			if got != c.want {
				t.Errorf("Agreement = %f, want %f", got, c.want)
			}
		})
	}
}
