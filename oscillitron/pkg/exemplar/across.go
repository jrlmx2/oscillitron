package exemplar

import (
	"context"
	"os"
	"sort"
	"strings"
)

// Neighbor is one cross-action BM25 hit: the matched exemplar plus its
// similarity score. Carries the full Exemplar so callers reach .Action
// (the playbook label the router votes on), .Prompt, and .Score.
type Neighbor struct {
	Exemplar Exemplar // .Action, .Prompt, .Output, .Score all reachable
	Sim      float64  // BM25 score of the query against this exemplar's Prompt
}

// AcrossRetriever is the optional cross-action retrieval capability.
// FileStore implements it; the Store interface deliberately does NOT
// embed it (adding a method to Store would force every implementer to
// grow it and break pkg/curation / pkg/adapter/curated, which consume
// Store). Consumers that need it type-assert: store.(AcrossRetriever).
type AcrossRetriever interface {
	// RetrieveAcross ranks exemplars across ALL action files by BM25
	// similarity of the query to each exemplar's Prompt, returning the
	// global top-k with their action labels. Same k1/b/tokenizer as
	// Retrieve. Read-only: unlike Retrieve, it does NOT update
	// LastRetrievedAt (the router is a sidecar read, not a warm-path
	// surfacing — bumping LRU on every routed AP would distort GC).
	RetrieveAcross(ctx context.Context, prompt string, k int) ([]Neighbor, error)
}

// RetrieveAcross implements AcrossRetriever on *FileStore. Touches no
// existing function: it iterates every action file, scores the query
// against each corpus with the same bm25.go machinery Retrieve uses,
// and merges the global top-k carrying each hit's Action label.
func (s *FileStore) RetrieveAcross(ctx context.Context, prompt string, k int) ([]Neighbor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if k <= 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		// A missing dir is an operator setup error; surface it.
		return nil, err
	}

	queryTokens := tokenize(prompt)
	var all []Neighbor
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		action := strings.TrimSuffix(entry.Name(), ".json")
		corpus, err := s.loadActionLocked(action)
		if err != nil {
			return nil, err
		}
		if len(corpus) == 0 {
			continue
		}
		// Reuse the exact bm25.go machinery — one index per action file,
		// same as Retrieve does for its single action.
		idx := buildBM25Index(corpus)
		for i := range corpus {
			sim := idx.score(queryTokens, i)
			if sim <= 0 {
				continue // no lexical overlap — not a neighbor
			}
			all = append(all, Neighbor{Exemplar: corpus[i], Sim: sim})
		}
	}
	if len(all) == 0 {
		return nil, nil
	}

	// Global top-k by Sim desc, then curation Score desc, then AddedAt
	// desc — the same tiebreaker order Retrieve uses.
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].Sim != all[b].Sim {
			return all[a].Sim > all[b].Sim
		}
		if all[a].Exemplar.Score != all[b].Exemplar.Score {
			return all[a].Exemplar.Score > all[b].Exemplar.Score
		}
		return all[a].Exemplar.AddedAt.After(all[b].Exemplar.AddedAt)
	})
	if k < len(all) {
		all = all[:k]
	}
	return all, nil
}

// Compile-time check: FileStore satisfies the optional capability.
var _ AcrossRetriever = (*FileStore)(nil)
