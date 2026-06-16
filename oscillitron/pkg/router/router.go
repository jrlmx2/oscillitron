// Package router provides an advisory, stateless playbook-hint router
// over the per-action exemplar substrate. It is the Thread A graft from
// scratch/dense-router-design.md §2.10 / §4d: a kNN-over-BM25 read that
// suggests (never dictates) which playbook the Evaluate step might pick.
//
// The old pkg/router (routing edges + rule tables) was deleted under the
// call-tree refactor; this is a clean-slate, differently-shaped package
// — no persistent routing topology, no node types, just a read.
package router

import (
	"context"

	"github.com/jrlmx2/oscillitron/pkg/session"
)

// Hint is the advisory playbook suggestion. The zero Hint (empty
// Playbook) means "no opinion" — the router abstained (empty corpus, no
// neighbors, or below the confidence/margin floor) and Evaluate proceeds
// cold.
type Hint struct {
	Playbook   session.Playbook // top-voted playbook among the k neighbors
	Confidence float64          // winning_votes / total_votes ∈ [0,1]
	Margin     float64          // (top1 − top2) vote share — ambiguity guard
	K          int              // neighbors actually found (corpus may be < k)
}

// IsEmpty reports whether the router abstained (no playbook opinion).
func (h Hint) IsEmpty() bool { return h.Playbook == "" }

// Router produces an advisory playbook hint from an AP's input.
//
// ADVISORY ONLY. A consumer MAY seed Evaluate with the hint (as steering
// text or, later, a bias field); it MUST NOT skip Evaluate. Skipping
// Evaluate on a hint re-introduces a declared brain-function by the back
// door — the precise thing the uniform-node + every-AP-evaluates locks
// forbid. Abstention (empty Hint) is the safe default.
type Router interface {
	Hint(ctx context.Context, in session.Payload) (Hint, error)
}

// validPlaybooks is the set the adapter's Evaluate can actually run. A
// neighbor whose Action string isn't one of these is NOT counted — a
// corrupt or legacy store must not be able to hint a playbook the
// adapter can't execute. (Exemplar.Action is a free string; the curation
// driver populates it with session.Playbook values, but the router can't
// assume the store is clean.)
var validPlaybooks = map[session.Playbook]bool{
	session.PlaybookPlan:           true,
	session.PlaybookProcess:        true,
	session.PlaybookCritique:       true,
	session.PlaybookVerifyGrounded: true,
	session.PlaybookCompose:        true,
}

func validPlaybook(p session.Playbook) bool { return validPlaybooks[p] }
