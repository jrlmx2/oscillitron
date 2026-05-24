package classification

import "testing"

func TestPropagate(t *testing.T) {
	cases := []struct {
		a, b Level
		want Level
	}{
		{Public, Public, Public},
		{Public, Internal, Internal},
		{Internal, Public, Internal},
		{Internal, Confidential, Confidential},
		{Confidential, Regulated, Regulated},
		{Regulated, Public, Regulated},
		// Unknown fails closed (more restrictive).
		{Level("unknown"), Public, Level("unknown")},
	}
	for _, c := range cases {
		if got := Propagate(c.a, c.b); got != c.want {
			t.Errorf("Propagate(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestValid(t *testing.T) {
	for _, l := range []Level{Public, Internal, Confidential, Regulated} {
		if !Valid(l) {
			t.Errorf("Valid(%q) = false, want true", l)
		}
	}
	if Valid(Level("nope")) {
		t.Error("Valid(\"nope\") = true, want false")
	}
}
