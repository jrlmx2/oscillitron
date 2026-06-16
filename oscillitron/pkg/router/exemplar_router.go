package router

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// ExemplarRouter is the kNN-over-BM25 playbook-hint router. It reads the
// per-action exemplar store via the optional AcrossRetriever capability,
// votes the action labels of the k nearest neighbors, and returns the
// majority playbook — abstaining when the vote is weak or ambiguous. No
// model call, no weight update, no node type: a pure stateless read over
// the substrate.
type ExemplarRouter struct {
	// Store is the exemplar substrate. Must implement AcrossRetriever
	// (FileStore does); a Store that doesn't is treated as "always
	// abstain" rather than an error (the router is a best-effort
	// sidecar, never load-bearing).
	Store exemplar.Store
	// K neighbors to poll. <=0 defaults to 8.
	K int
	// MinConfidence: winning vote share below this → abstain.
	// 0 defaults to 0.5 (simple majority).
	MinConfidence float64
	// MinMargin: (top1−top2) share below this → abstain (tie guard).
	// 0 defaults to 0.15.
	MinMargin float64
}

func (r ExemplarRouter) k() int {
	if r.K <= 0 {
		return 8
	}
	return r.K
}
func (r ExemplarRouter) minConfidence() float64 {
	if r.MinConfidence <= 0 {
		return 0.5
	}
	return r.MinConfidence
}
func (r ExemplarRouter) minMargin() float64 {
	if r.MinMargin <= 0 {
		return 0.15
	}
	return r.MinMargin
}

// Hint implements Router.
func (r ExemplarRouter) Hint(ctx context.Context, in session.Payload) (Hint, error) {
	across, ok := r.Store.(exemplar.AcrossRetriever)
	if !ok {
		return Hint{}, nil // store can't do cross-action retrieval → abstain
	}
	neighbors, err := across.RetrieveAcross(ctx, in.Content, r.k())
	if err != nil {
		return Hint{}, err // surface real errors; caller treats as abstain
	}
	if len(neighbors) == 0 {
		return Hint{}, nil // cold/empty store → abstain (Evaluate runs cold)
	}

	// Majority vote over VALID neighbor playbook labels. Sim-weighting is
	// a deliberate non-goal in v0 (plain counts keep the kNN-router
	// baseline honest and the math inspectable).
	votes := map[session.Playbook]float64{}
	total := 0.0
	for _, n := range neighbors {
		pb := session.Playbook(n.Exemplar.Action)
		if !validPlaybook(pb) {
			continue
		}
		votes[pb]++
		total++
	}
	if total == 0 {
		return Hint{}, nil // all neighbors had invalid labels → abstain
	}

	top1, top2 := winnerRunnerUp(votes)
	h := Hint{
		Playbook:   top1.key,
		Confidence: top1.val / total,
		Margin:     (top1.val - top2.val) / total,
		K:          len(neighbors),
	}
	if h.Confidence < r.minConfidence() || h.Margin < r.minMargin() {
		return Hint{}, nil // weak or ambiguous → abstain (cheap-local default)
	}
	return h, nil
}

type kv struct {
	key session.Playbook
	val float64
}

// winnerRunnerUp returns the top-two vote entries (runner-up is the zero
// kv when only one playbook got votes). Deterministic tie-break by
// playbook name so equal-vote runs are reproducible.
func winnerRunnerUp(votes map[session.Playbook]float64) (top1, top2 kv) {
	for k, v := range votes {
		cur := kv{key: k, val: v}
		switch {
		case cur.val > top1.val || (cur.val == top1.val && cur.key < top1.key):
			top2 = top1
			top1 = cur
		case cur.val > top2.val || (cur.val == top2.val && cur.key < top2.key):
			top2 = cur
		}
	}
	return top1, top2
}

var _ Router = ExemplarRouter{}
