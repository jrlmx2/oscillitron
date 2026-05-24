package stakes

import "testing"

func TestEffective(t *testing.T) {
	cases := []struct {
		in   Level
		want Level
	}{
		{Low, Low},
		{Medium, Medium},
		{High, High},
		{"", Medium},        // zero value
		{"garbage", Medium}, // unknown defaults to safe (medium)
	}
	for _, tc := range cases {
		if got := Effective(tc.in); got != tc.want {
			t.Errorf("Effective(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		in   Level
		want bool
	}{
		{Low, true},
		{Medium, true},
		{High, true},
		{"", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		if got := Valid(tc.in); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAttemptScale(t *testing.T) {
	cases := []struct {
		level Level
		base  int
		want  int
	}{
		// Low always caps at 1 regardless of base — the cheap-path
		// skips voting.
		{Low, 1, 1},
		{Low, 5, 1},
		{Low, 10, 1},

		// Medium passes base through.
		{Medium, 1, 1},
		{Medium, 5, 5},
		{Medium, 10, 10},

		// High doubles base.
		{High, 1, 2},
		{High, 5, 10},
		{High, 3, 6},

		// Zero value defaults to medium.
		{"", 5, 5},

		// Defensive: base of 0 or negative gets clamped to 1
		// before scaling.
		{Medium, 0, 1},
		{Medium, -3, 1},
		{High, 0, 2},
	}
	for _, tc := range cases {
		if got := AttemptScale(tc.level, tc.base); got != tc.want {
			t.Errorf("AttemptScale(%q, %d) = %d, want %d", tc.level, tc.base, got, tc.want)
		}
	}
}
