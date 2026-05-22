// CLAUDE GENERATED
// Package exemplar is the per-action substrate that closes
// Oscillitron's self-improvement loop without violating
// invocation isolation.
//
// Background (from parent CLAUDE.md, "Architecture" + "v2 plan"):
//
//   - WARM PATH: each AP invocation gets a fresh Hermes session.
//     Isolation is strict — no cross-AP working-memory contamination.
//   - COLD PATH: per-brain-function long-lived sessions read
//     completed APs (from the bench JSONL stream) and distill
//     them into exemplars/recipes/anti-patterns. (Cold-path
//     driver lives in pkg/curation; not in this PR.)
//   - SUBSTRATE: the curated artifacts live in files on disk,
//     per-action. The wrapping adapter (pkg/adapter/curated;
//     not in this PR either) retrieves top-K relevant exemplars
//     for each warm-path call and prepends them as few-shot
//     examples.
//
// This package is the substrate layer. Subsequent v2 PRs add BM25
// retrieval (currently exemplars are returned by score-descending
// recency as a v0 placeholder), the curated wrapping adapter, and
// the cold-path curation driver.
//
// Stdlib only. One JSON file per action under FileStore.Dir.
package exemplar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Exemplar is one preserved AP outcome — a (prompt, output) pair
// worth retrieving as a few-shot example for future APs of the same
// brain function. Each Exemplar belongs to exactly one Action.
type Exemplar struct {
	// Action is the brain-function key (e.g., "process", "plan",
	// "critique"). Maps to session.Playbook values when populated by
	// the curation driver.
	Action string `json:"action"`
	// Prompt is the input the AP saw.
	Prompt string `json:"prompt"`
	// Output is the model's response that the grader accepted as
	// high-quality.
	Output string `json:"output"`
	// Score is the grader's normalized score (0.0-1.0). Used for
	// retrieval ranking and as a quality floor when curating.
	Score float64 `json:"score"`
	// SourceCase identifies the bench case this exemplar came from.
	// Used for dedup — one exemplar per (Action, SourceCase) pair.
	SourceCase string `json:"source_case"`
	// Tokens is the approximate token count of (prompt + output);
	// used by GC's per-action budget cap. Populated by Add if zero.
	Tokens int `json:"tokens"`
	// AddedAt is when this exemplar entered the store.
	AddedAt time.Time `json:"added_at"`
	// LastRetrievedAt is when this exemplar was last surfaced by
	// Retrieve. Used by GC for LRU eviction. Zero value = never
	// retrieved (newest exemplars sort fresh).
	LastRetrievedAt time.Time `json:"last_retrieved_at,omitempty"`
}

// Store is the per-action exemplar substrate interface.
//
// Implementations must be safe for concurrent use — the bench's
// OnCase callback writes during a run while a sidecar might retrieve.
type Store interface {
	// Add records an exemplar. If one with the same (Action,
	// SourceCase) pair already exists, the later Add wins (overwrite
	// semantics — caller can re-curate cleanly).
	Add(ctx context.Context, e Exemplar) error

	// Retrieve returns up to k exemplars relevant to (action, prompt).
	// The v0 ranking is score-descending then recency-descending — a
	// placeholder for the BM25 retrieval that lands in v2 PR 2.
	// Updates LastRetrievedAt on returned exemplars.
	Retrieve(ctx context.Context, action, prompt string, k int) ([]Exemplar, error)

	// GC enforces budget caps. Returns the number of exemplars
	// dropped. Idempotent (returns 0 if already under budget).
	GC(ctx context.Context) (int, error)
}

// --- FileStore: JSON file per action under Dir ---

// FileStore stores exemplars as JSON, one file per Action under Dir.
// Concurrent-safe via an internal mutex serializing file I/O.
//
// File layout:
//
//	Dir/
//	  process.json     — array of Exemplars where Action="process"
//	  critique.json    — Action="critique"
//	  ...
//
// Operators GC by passing MaxTokensPerAction; without it the store
// grows unbounded.
type FileStore struct {
	// Dir is the root directory. Must exist (FileStore does not
	// create it — that's an operator setup concern).
	Dir string
	// MaxTokensPerAction caps the per-action token budget. GC
	// evicts least-recently-retrieved exemplars until under budget.
	// 0 means no cap (operator opts out of GC, e.g., for tests).
	MaxTokensPerAction int
	// TokensFn estimates the token count of an exemplar's
	// (prompt+output) text when Exemplar.Tokens is zero on Add.
	// Defaults to ApproxTokensByChars.
	TokensFn func(text string) int

	mu sync.Mutex
}

// ApproxTokensByChars is the default TokensFn — len(text)/4 + 1,
// the same approximation OpenAI and Anthropic publish for English
// text. Cheap, no tokenizer dependency, good enough for budget caps
// (which only need to be roughly right).
func ApproxTokensByChars(text string) int {
	if text == "" {
		return 0
	}
	return len(text)/4 + 1
}

// ErrInvalidExemplar is returned by Add when the exemplar is
// missing required fields.
var ErrInvalidExemplar = errors.New("exemplar: missing required field (action, prompt, or output)")

// Add appends e to the file for e.Action. If an exemplar with the
// same (Action, SourceCase) already exists, it's overwritten in
// place (so callers can re-run curation idempotently). Populates
// AddedAt if zero, and Tokens via TokensFn if zero.
func (s *FileStore) Add(ctx context.Context, e Exemplar) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.Action == "" || e.Prompt == "" || e.Output == "" {
		return fmt.Errorf("%w: %+v", ErrInvalidExemplar, e)
	}
	if e.AddedAt.IsZero() {
		e.AddedAt = time.Now()
	}
	if e.Tokens == 0 {
		fn := s.TokensFn
		if fn == nil {
			fn = ApproxTokensByChars
		}
		e.Tokens = fn(e.Prompt + e.Output)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.loadActionLocked(e.Action)
	if err != nil {
		return err
	}
	// Dedup by SourceCase if set; else by (Prompt+Output) hash.
	replaced := false
	for i, prior := range existing {
		if e.SourceCase != "" && prior.SourceCase == e.SourceCase {
			existing[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, e)
	}
	return s.writeActionLocked(e.Action, existing)
}

// Retrieve returns up to k exemplars for the action, ranked
// score-descending then most-recent-first. prompt is currently
// unused by FileStore (v0 placeholder); v2 PR 2 wires BM25 over
// prompts here.
func (s *FileStore) Retrieve(ctx context.Context, action, prompt string, k int) ([]Exemplar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if action == "" {
		return nil, errors.New("exemplar: Retrieve requires non-empty action")
	}
	if k <= 0 {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.loadActionLocked(action)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}

	// Sort by Score desc, then AddedAt desc. Stable sort so equal
	// keys retain insertion order — operator's curation order survives.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].AddedAt.After(all[j].AddedAt)
	})

	if k > len(all) {
		k = len(all)
	}
	out := make([]Exemplar, k)
	copy(out, all[:k])

	// Update LastRetrievedAt on the returned exemplars in the
	// underlying file. Done in a single rewrite to amortize I/O.
	now := time.Now()
	for i := range out {
		out[i].LastRetrievedAt = now
	}
	// Apply the update to the full slice (find by SourceCase, or by
	// pointer-equality on prompt+output if no SourceCase).
	for _, ret := range out {
		for j, ex := range all {
			if matches(ret, ex) {
				all[j].LastRetrievedAt = now
				break
			}
		}
	}
	if err := s.writeActionLocked(action, all); err != nil {
		return nil, err
	}
	return out, nil
}

// GC enforces MaxTokensPerAction across every action file. Drops
// least-recently-retrieved (LastRetrievedAt asc, AddedAt asc as
// tiebreaker) until each action is under budget.
func (s *FileStore) GC(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.MaxTokensPerAction <= 0 {
		return 0, nil // no cap configured → nothing to GC
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0, fmt.Errorf("exemplar.GC: read dir %s: %w", s.Dir, err)
	}

	dropped := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		action := strings.TrimSuffix(name, ".json")
		all, err := s.loadActionLocked(action)
		if err != nil {
			return dropped, fmt.Errorf("exemplar.GC: load %s: %w", action, err)
		}
		total := 0
		for _, e := range all {
			total += e.Tokens
		}
		if total <= s.MaxTokensPerAction {
			continue
		}
		// Sort by eviction priority: oldest LastRetrievedAt first
		// (never-retrieved exemplars have zero time = oldest).
		// Tiebreak by oldest AddedAt.
		sort.SliceStable(all, func(i, j int) bool {
			if all[i].LastRetrievedAt.Equal(all[j].LastRetrievedAt) {
				return all[i].AddedAt.Before(all[j].AddedAt)
			}
			return all[i].LastRetrievedAt.Before(all[j].LastRetrievedAt)
		})
		// Drop from the front until under budget.
		for total > s.MaxTokensPerAction && len(all) > 0 {
			total -= all[0].Tokens
			all = all[1:]
			dropped++
		}
		if err := s.writeActionLocked(action, all); err != nil {
			return dropped, fmt.Errorf("exemplar.GC: write %s: %w", action, err)
		}
	}
	return dropped, nil
}

// --- internal file I/O helpers ---

// loadActionLocked reads the action's JSON file, returning an empty
// slice if the file doesn't exist. Caller must hold s.mu.
func (s *FileStore) loadActionLocked(action string) ([]Exemplar, error) {
	if action == "" {
		return nil, errors.New("exemplar: action cannot be empty")
	}
	path := filepath.Join(s.Dir, action+".json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("exemplar: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var out []Exemplar
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("exemplar: parse %s: %w", path, err)
	}
	return out, nil
}

// writeActionLocked writes the action's exemplars atomically (write
// to .tmp, rename) so a crash mid-write can't corrupt the file.
// Caller must hold s.mu.
func (s *FileStore) writeActionLocked(action string, exemplars []Exemplar) error {
	path := filepath.Join(s.Dir, action+".json")
	tmp := path + ".tmp"

	if len(exemplars) == 0 {
		// Empty list — remove the file rather than leave a "[]" stub.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("exemplar: remove %s: %w", path, err)
		}
		return nil
	}

	data, err := json.MarshalIndent(exemplars, "", "  ")
	if err != nil {
		return fmt.Errorf("exemplar: marshal: %w", err)
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("exemplar: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("exemplar: rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

// matches reports whether two exemplars refer to the same logical
// entry. Used during Retrieve to map returned-and-mutated exemplars
// back into the full slice for the LastRetrievedAt update.
func matches(a, b Exemplar) bool {
	if a.SourceCase != "" && a.SourceCase == b.SourceCase && a.Action == b.Action {
		return true
	}
	return a.Action == b.Action && a.Prompt == b.Prompt && a.Output == b.Output
}

// Compile-time check.
var _ Store = (*FileStore)(nil)
