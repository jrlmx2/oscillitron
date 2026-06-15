package semanticentropy

import (
	"math"
	"testing"
)

func TestExactMatch_Cluster(t *testing.T) {
	var c ExactMatch

	// Two clusters {2,1} summing to 3.
	sizes := c.Cluster([]string{"A", "A", "B"})
	if got := sum(sizes); got != 3 {
		t.Fatalf("Cluster([A,A,B]) sizes sum = %d, want 3", got)
	}
	if len(sizes) != 2 {
		t.Fatalf("Cluster([A,A,B]) cluster count = %d, want 2", len(sizes))
	}
	if !sameMultiset(sizes, []int{2, 1}) {
		t.Fatalf("Cluster([A,A,B]) = %v, want multiset {2,1}", sizes)
	}

	// Blank answers are dropped (a failed extraction is not a meaning).
	sizes = c.Cluster([]string{"A", "", "A"})
	if !sameMultiset(sizes, []int{2}) {
		t.Fatalf("Cluster([A,\"\",A]) = %v, want {2}", sizes)
	}

	// Empty input → no clusters.
	if sizes = c.Cluster(nil); len(sizes) != 0 {
		t.Fatalf("Cluster(nil) = %v, want empty", sizes)
	}
}

func TestEntropy(t *testing.T) {
	const eps = 1e-9

	if got := Entropy(nil); got != 0 {
		t.Fatalf("Entropy(nil) = %v, want 0", got)
	}
	if got := Entropy([]int{5}); got != 0 {
		t.Fatalf("Entropy([5]) = %v, want 0 (single cluster)", got)
	}
	if got := Entropy([]int{1, 1}); math.Abs(got-math.Ln2) > eps {
		t.Fatalf("Entropy([1,1]) = %v, want ln2=%v", got, math.Ln2)
	}
	// −(0.75·ln0.75 + 0.25·ln0.25)
	want := -(0.75*math.Log(0.75) + 0.25*math.Log(0.25))
	if got := Entropy([]int{3, 1}); math.Abs(got-want) > eps {
		t.Fatalf("Entropy([3,1]) = %v, want %v", got, want)
	}
}

func TestConfidence(t *testing.T) {
	const eps = 1e-9

	// Unanimous → max confidence.
	if got := Confidence([]int{5}, 5); math.Abs(got-1.0) > eps {
		t.Fatalf("Confidence([5],5) = %v, want 1.0", got)
	}
	// Total disagreement (all singletons) → 0.
	if got := Confidence([]int{1, 1, 1, 1, 1}, 5); math.Abs(got) > eps {
		t.Fatalf("Confidence([1,1,1,1,1],5) = %v, want 0.0", got)
	}
	// No answers → 0.
	if got := Confidence(nil, 0); got != 0 {
		t.Fatalf("Confidence(nil,0) = %v, want 0", got)
	}
	// Genuine N<2 (one answer, one cluster) → 0 (can't measure spread).
	if got := Confidence([]int{1}, 1); got != 0 {
		t.Fatalf("Confidence([1],1) = %v, want 0 (N<2)", got)
	}
	// A single cluster of N≥2 votes is unanimous → 1.0. The histogram
	// total (Σ=3) is trusted over a wrong hint n=1 (correction #2), so
	// this is NOT an N<2 case — it is full agreement.
	if got := Confidence([]int{3}, 1); math.Abs(got-1.0) > eps {
		t.Fatalf("Confidence([3],1) = %v, want 1.0 (unanimous, Σ=3 trusted over n=1)", got)
	}
	// Mismatch guard (correction #2): a wrong n is ignored; Σsizes=4 used.
	want := 1 - Entropy([]int{3, 1})/math.Log(4)
	if got := Confidence([]int{3, 1}, 99); math.Abs(got-want) > eps {
		t.Fatalf("Confidence([3,1],99) = %v, want %v (Σsizes=4, not n=99)", got, want)
	}
	// Result is always within [0,1].
	if got := Confidence([]int{3, 1}, 4); got < 0 || got > 1 {
		t.Fatalf("Confidence([3,1],4) = %v, out of [0,1]", got)
	}
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func sameMultiset(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	ca, cb := map[int]int{}, map[int]int{}
	for _, x := range a {
		ca[x]++
	}
	for _, x := range b {
		cb[x]++
	}
	if len(ca) != len(cb) {
		return false
	}
	for k, v := range ca {
		if cb[k] != v {
			return false
		}
	}
	return true
}
