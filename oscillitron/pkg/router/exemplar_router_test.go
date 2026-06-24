package router

import (
	"context"
	"math"
	"testing"

	"github.com/jrlmx2/oscillitron/pkg/exemplar"
	"github.com/jrlmx2/oscillitron/pkg/session"
)

// fakeAcross is an exemplar.Store that also implements AcrossRetriever,
// returning a canned neighbor list (action labels are what the router
// votes on).
type fakeAcross struct {
	exemplar.Store // embedded nil — Add/Retrieve/GC unused in these tests
	neighbors      []exemplar.Neighbor
	err            error
}

func (f fakeAcross) RetrieveAcross(_ context.Context, _ string, _ int) ([]exemplar.Neighbor, error) {
	return f.neighbors, f.err
}

func nbrs(actions ...string) []exemplar.Neighbor {
	out := make([]exemplar.Neighbor, len(actions))
	for i, a := range actions {
		out[i] = exemplar.Neighbor{Exemplar: exemplar.Exemplar{Action: a}, Sim: 1.0}
	}
	return out
}

// plainStore implements Store but NOT AcrossRetriever.
type plainStore struct{ exemplar.Store }

func TestHint_MajorityVoteAndConfidence(t *testing.T) {
	// 6 of 8 neighbors are process → process wins with confidence 0.75.
	st := fakeAcross{neighbors: nbrs("process", "process", "process", "process", "process", "process", "plan", "plan")}
	r := ExemplarRouter{Store: st, K: 8}
	h, err := r.Hint(context.Background(), session.Payload{Content: "anything"})
	if err != nil {
		t.Fatalf("Hint: %v", err)
	}
	if h.Playbook != session.PlaybookProcess {
		t.Errorf("Playbook = %q, want process", h.Playbook)
	}
	if math.Abs(h.Confidence-0.75) > 1e-9 {
		t.Errorf("Confidence = %v, want 0.75", h.Confidence)
	}
	if math.Abs(h.Margin-0.5) > 1e-9 { // (6-2)/8
		t.Errorf("Margin = %v, want 0.5", h.Margin)
	}
}

func TestHint_AbstainsBelowMarginAndConfidence(t *testing.T) {
	// 4/4 split → margin 0 < 0.15 → abstain.
	st := fakeAcross{neighbors: nbrs("process", "process", "process", "process", "plan", "plan", "plan", "plan")}
	if h, _ := (ExemplarRouter{Store: st, K: 8}).Hint(context.Background(), session.Payload{}); !h.IsEmpty() {
		t.Errorf("4/4 split: got %v, want abstain", h)
	}
	// 5/4 (confidence 0.556 ok) but tighten MinConfidence to force abstain.
	st2 := fakeAcross{neighbors: nbrs("process", "process", "process", "process", "process", "plan", "plan", "plan", "plan")}
	if h, _ := (ExemplarRouter{Store: st2, K: 9, MinConfidence: 0.9}).Hint(context.Background(), session.Payload{}); !h.IsEmpty() {
		t.Errorf("below MinConfidence: got %v, want abstain", h)
	}
}

func TestHint_IgnoresInvalidActionLabels(t *testing.T) {
	// Garbage labels don't count; remaining 3 process / 1 plan → process.
	st := fakeAcross{neighbors: nbrs("garbage", "garbage", "process", "process", "process", "plan")}
	h, _ := (ExemplarRouter{Store: st, K: 8}).Hint(context.Background(), session.Payload{})
	if h.Playbook != session.PlaybookProcess {
		t.Errorf("Playbook = %q, want process (invalid labels excluded)", h.Playbook)
	}
	// All-invalid → abstain.
	allBad := fakeAcross{neighbors: nbrs("garbage", "nonsense", "xyz")}
	if h, _ := (ExemplarRouter{Store: allBad, K: 8}).Hint(context.Background(), session.Payload{}); !h.IsEmpty() {
		t.Errorf("all-invalid: got %v, want abstain", h)
	}
}

func TestHint_NonAcrossStoreAbstains(t *testing.T) {
	r := ExemplarRouter{Store: plainStore{}, K: 8}
	h, err := r.Hint(context.Background(), session.Payload{Content: "x"})
	if err != nil {
		t.Errorf("err = %v, want nil (graceful abstain)", err)
	}
	if !h.IsEmpty() {
		t.Errorf("non-across store: got %v, want abstain", h)
	}
}

func TestHint_EmptyNeighborsAbstains(t *testing.T) {
	st := fakeAcross{neighbors: nil}
	if h, _ := (ExemplarRouter{Store: st, K: 8}).Hint(context.Background(), session.Payload{}); !h.IsEmpty() {
		t.Errorf("empty neighbors: got %v, want abstain", h)
	}
}
