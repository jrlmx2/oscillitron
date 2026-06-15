// Package semanticentropy computes discrete (black-box) semantic
// entropy over a set of sampled answers and maps it to a [0,1]
// confidence the cope dispatcher can consume. "Discrete" = it needs
// only the output strings and a clustering function — no logprobs,
// no model internals (Farquhar et al., Nature 2024; the frequency-
// based variant in arXiv:2510.09256). That matches Vote exactly:
// Vote has N text answers and no logit access.
//
// v0 ships the ExactMatch clusterer, which for MCQ / extracted-
// canonical workloads (GPQA) IS meaning-clustering — identical
// extracted letters are the same meaning. Free-form workloads need a
// semantic clusterer (NLI / embedding cosine); that lives in a
// pkg/semanticentropy/nli subpackage (v1), dependency-isolated like
// pkg/trace/otel. This package stays stdlib-only.
package semanticentropy

import "math"

// Clusterer groups answers into meaning-clusters and returns the
// cluster sizes. Order is irrelevant — entropy is symmetric over the
// cluster distribution. Empty/blank answers are the clusterer's
// responsibility to drop (a failed extraction is not a meaning).
type Clusterer interface {
	Cluster(answers []string) (sizes []int)
}

// ExactMatch is the v0 clusterer: byte-identical strings cluster
// together. For MCQ/extracted-canonical answers this is exact
// meaning-clustering, and it produces the same histogram Vote's
// vote distribution already builds.
type ExactMatch struct{}

// Cluster implements Clusterer. Blank answers ("") are dropped —
// a failed extraction is not a cluster (mirrors Vote's tally rule).
func (ExactMatch) Cluster(answers []string) []int {
	counts := map[string]int{}
	for _, a := range answers {
		if a == "" {
			continue
		}
		counts[a]++
	}
	sizes := make([]int, 0, len(counts))
	for _, n := range counts {
		sizes = append(sizes, n)
	}
	return sizes
}

// Entropy returns the discrete Shannon entropy over the cluster
// distribution, in nats (natural log):
//
//	H = −Σ_c (n_c/N)·ln(n_c/N)     where N = Σ_c n_c
//
// Edge cases:
//   - len(sizes) == 0  → 0   (no answers; no spread to measure)
//   - len(sizes) == 1  → 0   (full agreement = certain)
//   - any size ≤ 0     → that cluster is skipped (defensive; a
//     well-formed clusterer never emits non-positive sizes)
func Entropy(sizes []int) float64 {
	total := 0
	for _, n := range sizes {
		if n > 0 {
			total += n
		}
	}
	if total == 0 {
		return 0
	}
	var h float64
	for _, n := range sizes {
		if n <= 0 {
			continue
		}
		p := float64(n) / float64(total)
		h -= p * math.Log(p)
	}
	return h
}

// Confidence maps cluster sizes to a [0,1] confidence:
//
//	conf = 1 − H/ln(N)            (N = number of answers = Σ sizes)
//
// Normalizing by the maximum possible entropy ln(N) (the all-
// singletons / total-disagreement case) makes the value comparable
// across different N — important because Vote's N is stakes-scaled
// (stakes.AttemptScale: Low=1, Medium=N, High=2N).
//
// The n parameter is the caller's count of answers. It is treated as
// a HINT: if it disagrees with Σ sizes (e.g. the caller passed
// `successes` but the histogram dropped empty extractions), the
// function trusts Σ sizes. Pass n only so a caller that knows the
// true sample count can be explicit; the function never lets a wrong
// n corrupt the math.
//
// Edge cases (all return 0 = "no signal", which cope.Decide reads as
// mid-band → ShipWithCaveat, the safe default):
//   - N < 2  → 0   (can't measure spread from < 2 answers; ln(N)≤0)
//   - single cluster (H == 0, full agreement) → 1.0 (max confidence)
func Confidence(sizes []int, n int) float64 {
	total := 0
	for _, s := range sizes {
		if s > 0 {
			total += s
		}
	}
	// Trust the histogram over the hint.
	if total != n {
		n = total
	}
	if n < 2 {
		return 0
	}
	h := Entropy(sizes)
	maxH := math.Log(float64(n))
	if maxH <= 0 {
		return 0
	}
	conf := 1 - h/maxH
	// Clamp for float safety (h can be a hair above maxH from rounding).
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	return conf
}
